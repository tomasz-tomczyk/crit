package focus

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// CommentFocusOverride captures the user's --scope flag for `crit comment`.
type CommentFocusOverride string

const (
	ScopeOverrideUnset       CommentFocusOverride = ""
	ScopeOverrideLayer       CommentFocusOverride = "layer"
	ScopeOverrideFullStack   CommentFocusOverride = "full-stack"
	ScopeOverrideWorkingTree CommentFocusOverride = "working-tree"
)

// InheritedScope is the focus metadata stamped on comments authored via
// `crit comment`. All fields empty for working-tree mode. PRNumber and
// BaseSHA flow through to the comment's FocusKey so view-scoped visibility
// matches the daemon's view.
//
// Defined in session; aliased here for focus helpers.

// prURLRe matches GitHub PR URLs like https://github.com/owner/repo/pull/123.
// The trailing group accepts /, ?, # so suffixes like /files or ?diff=split work.
var prURLRe = regexp.MustCompile(`^https?://[^/]+/[^/]+/[^/]+/pull/(\d+)(?:[/?#].*)?$`)

// parsePRSpec resolves --pr <num|url> to a numeric PR number. Returns an error
// for non-numeric, non-positive, or unparsable inputs.
func parsePRSpec(spec string) (int, error) {
	if m := prURLRe.FindStringSubmatch(spec); m != nil {
		return strconv.Atoi(m[1])
	}
	n, err := strconv.Atoi(spec)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid --pr value %q (expected number or https://.../pull/N URL)", spec)
	}
	return n, nil
}

// parseRangeSpec splits "base..head" with strict validation. Three dots ("...")
// are explicitly rejected — symmetric-difference is not what users expect.
func parseRangeSpec(spec string) (base, head string, err error) {
	if strings.Contains(spec, "...") {
		return "", "", fmt.Errorf("--range expects two-dot syntax (base..head), got %q", spec)
	}
	parts := strings.SplitN(spec, "..", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid --range value %q (expected base..head)", spec)
	}
	return parts[0], parts[1], nil
}

// normalizeScopeSpec is the shared parser for the --scope CLI flag. It maps
// the raw string to a DiffScope and reports whether the value referred to
// the working-tree pseudo-scope (only valid for `crit comment`, not for
// starting a session). Callers gate on the bool depending on which surface
// they implement.
//
// "" defaults to layer. "full-stack" / "full_stack" both map to full_stack.
// "working-tree" / "working_tree" map to (DiffScopeLayer, true) — the
// DiffScope value is irrelevant because callers that accept working-tree
// inspect the bool first.
func normalizeScopeSpec(s string) (DiffScope, bool, error) {
	switch s {
	case "", "layer":
		return DiffScopeLayer, false, nil
	case "full-stack", "full_stack":
		return DiffScopeFullStack, false, nil
	case "working-tree", "working_tree":
		return DiffScopeLayer, true, nil
	default:
		return "", false, fmt.Errorf("invalid --scope value %q", s)
	}
}

// parseScopeSpec maps the --scope flag to a DiffScope for the session-start
// surface (`crit --pr <n> --scope=...`). Rejects "working-tree" because it's
// not a valid scope for a focus — sessions either run in working-tree mode
// (no --pr/--range) or in range mode with layer/full-stack.
func parseScopeSpec(s string) (DiffScope, error) {
	scope, isWorkingTree, err := normalizeScopeSpec(s)
	if err != nil {
		return "", fmt.Errorf("%w (expected layer or full-stack)", err)
	}
	if isWorkingTree {
		return "", fmt.Errorf("invalid --scope value %q (expected layer or full-stack)", s)
	}
	return scope, nil
}

// shortSHA returns a 7-char SHA prefix for display. Defensive against short input.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// resolveFocus turns CLI inputs into a *Focus, or nil for working-tree default.
// Mutually exclusive: errors if both --pr and --range are given.
//
// remoteFiles=true skips local-fetch / object presence checks because file
// content reads will go through the GitHub API instead of local git.
// ResolveFocus parses --pr / --range / --scope into a Focus value.
func ResolveFocus(prSpec, rangeSpec, scopeSpec string, remoteFiles bool, v vcs.VCS, repoRoot string) (*Focus, error) {
	if prSpec != "" && rangeSpec != "" {
		return nil, fmt.Errorf("--pr and --range are mutually exclusive")
	}
	scope, err := parseScopeSpec(scopeSpec)
	if err != nil {
		return nil, err
	}
	if scopeSpec != "" && rangeSpec != "" {
		fmt.Fprintln(os.Stderr, "Note: --scope is ignored with --range; pass an explicit base..head instead")
	}
	switch {
	case prSpec != "":
		return resolveFocusFromPR(prSpec, scope, remoteFiles, v, repoRoot)
	case rangeSpec != "":
		return resolveFocusFromRange(rangeSpec, remoteFiles, v, repoRoot)
	}
	return nil, nil
}

func resolveFocusFromPR(prSpec string, scope DiffScope, remoteFiles bool, v vcs.VCS, repoRoot string) (*Focus, error) {
	prNum, err := parsePRSpec(prSpec)
	if err != nil {
		return nil, err
	}
	if FetchPRByNumberHook == nil {
		return nil, fmt.Errorf("resolving PR #%d: PR fetch not wired", prNum)
	}
	info, err := FetchPRByNumberHook(prNum)
	if err != nil {
		return nil, fmt.Errorf("resolving PR #%d: %w", prNum, err)
	}
	forkURL := ""
	if info.IsCrossRepository {
		forkURL = info.HeadRepoURL
	}
	// In --remote mode, file content reads go through `gh api`, so we don't
	// need (and don't want) the local git-fetch side effects.
	if !remoteFiles {
		if err := vcs.EnsureSHAFetched(v, info.BaseRefOid, repoRoot, ""); err != nil {
			return nil, err
		}
		if err := vcs.EnsureSHAFetched(v, info.HeadRefOid, repoRoot, forkURL); err != nil {
			return nil, err
		}
	}

	defaultBranch := ""
	if v != nil {
		defaultBranch = v.DefaultBranch()
	}
	defaultSHA, _ := vcs.ResolveDefaultBranchSHA(v, repoRoot, defaultBranch)
	if scope == DiffScopeFullStack && defaultSHA == "" {
		return nil, fmt.Errorf("--scope=full-stack requires a resolvable default branch tip; got none for %q (detached HEAD or no remote?)", defaultBranch)
	}

	// GitHub renders a PR as base...head (three-dot, from the merge-base).
	// Using info.BaseRefOid directly would diff base..head (two-dot), folding
	// in any commits that landed on the base branch since this branch diverged
	// — surfacing files that aren't part of the PR. Pin the layer diff base to
	// the merge-base so the diff matches GitHub regardless of rebase state.
	baseSHA := info.BaseRefOid
	if v != nil {
		// Best-effort: in --remote mode the base/head objects may not be local
		// (EnsureSHAFetched was skipped above), so merge-base can fail. We then
		// fall back to BaseRefOid — a two-dot diff (the case #659 fixed locally).
		if mb, err := v.MergeBaseOf(info.BaseRefOid, info.HeadRefOid, repoRoot); err == nil && mb != "" {
			baseSHA = mb
		}
	}

	return &Focus{
		Kind:        FocusRange,
		PRNumber:    info.Number,
		PRURL:       info.URL,
		Label:       fmt.Sprintf("PR #%d: %s", info.Number, info.Title),
		BaseSHA:     baseSHA,
		HeadSHA:     info.HeadRefOid,
		DefaultSHA:  defaultSHA,
		ForkURL:     forkURL,
		BaseRefName: info.BaseRefName,
		HeadRefName: info.HeadRefName,
		DiffScope:   scope,
		IsStacked:   IsStackedPRHook != nil && IsStackedPRHook(info, v),
	}, nil
}

func resolveFocusFromRange(rangeSpec string, remoteFiles bool, v vcs.VCS, repoRoot string) (*Focus, error) {
	base, head, err := parseRangeSpec(rangeSpec)
	if err != nil {
		return nil, err
	}
	// In --remote mode, content reads come from the GitHub API; we can't
	// prove the SHAs exist locally because we don't intend to use them.
	if !remoteFiles && v != nil {
		if !vcs.HasObject(base, repoRoot) {
			return nil, fmt.Errorf("base SHA %s not present locally", base)
		}
		if !vcs.HasObject(head, repoRoot) {
			return nil, fmt.Errorf("head SHA %s not present locally", head)
		}
	}
	return &Focus{
		Kind:      FocusRange,
		BaseSHA:   base,
		HeadSHA:   head,
		Label:     fmt.Sprintf("%s..%s", shortSHA(base), shortSHA(head)),
		DiffScope: DiffScopeLayer,
		IsStacked: false,
	}, nil
}

// commentScopeOverrideFromFlag normalizes the raw --scope string for `crit comment`.
// Unlike parseScopeSpec (used at session start), this surface accepts
// "working-tree" — a comment can be explicitly stamped against the working
// tree even when a daemon is running in range mode. Empty input is unset.
func commentScopeOverrideFromFlag(s string) (CommentFocusOverride, error) {
	if s == "" {
		return ScopeOverrideUnset, nil
	}
	scope, isWorkingTree, err := normalizeScopeSpec(s)
	if err != nil {
		return "", fmt.Errorf("%w (expected layer | full-stack | working-tree)", err)
	}
	switch {
	case isWorkingTree:
		return ScopeOverrideWorkingTree, nil
	case scope == DiffScopeFullStack:
		return ScopeOverrideFullStack, nil
	default:
		return ScopeOverrideLayer, nil
	}
}

// probeDaemonFocusFn is the live function that contacts the daemon to fetch
// its current Focus. Indirected through a package var so tests can stub it.
var probeDaemonFocusFn = probeDaemonFocusReal

// probeDaemonFocus contacts the running daemon (if any) and returns its Focus.
// Returns nil on any failure — best-effort.
func probeDaemonFocus() *Focus {
	return probeDaemonFocusFn()
}

func probeDaemonFocusReal() *Focus {
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		return nil
	}
	sessions, _ := daemon.ListSessionsForCWD(cwd)
	if len(sessions) == 0 {
		return nil
	}
	// Query every daemon for its Focus. When multiple daemons run in the
	// same cwd (e.g. one reviewing a PR, one reviewing the working tree),
	// returning sessions[0] would silently stamp `crit comment` with the
	// wrong scope. Treat ambiguity as "no inheritable focus" so the caller
	// falls through to the on-disk ActiveDiffScope path — which is the
	// safer default than guessing.
	client := &http.Client{Timeout: 2 * time.Second}
	var rangeFoci []*Focus
	var workingFoci []*Focus
	for _, sess := range sessions {
		f := fetchSessionFocus(client, sess.Host, sess.Port)
		if f == nil {
			continue
		}
		if f.Kind == FocusRange {
			rangeFoci = append(rangeFoci, f)
		} else {
			workingFoci = append(workingFoci, f)
		}
	}
	// Range focus is the strictly-scoped one — prefer it when uniquely
	// resolvable. If two daemons both expose a Range focus, ambiguity
	// wins: return nil and let the caller resolve from disk / explicit flag.
	if len(rangeFoci) == 1 {
		return rangeFoci[0]
	}
	if len(rangeFoci) > 1 {
		return nil
	}
	if len(workingFoci) == 1 {
		return workingFoci[0]
	}
	return nil
}

// fetchSessionFocus queries one daemon's /api/session and returns its Focus
// (nil on any error). Factored out so probeDaemonFocusReal can iterate over
// every matching daemon without ballooning its complexity.
func fetchSessionFocus(client *http.Client, host string, port int) *Focus {
	connHost := host
	if connHost == "" {
		connHost = "127.0.0.1"
	}
	resp, err := client.Get(fmt.Sprintf("http://%s:%d/api/session", connHost, port))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var info struct {
		Focus *Focus `json:"focus"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil
	}
	return info.Focus
}

// loadCritJSONForOutputDir loads the on-disk CritJSON for the given output dir
// (or the resolved review path when outputDir is ""). A missing review file is
// the common case and returns ok=false silently. Parse errors and other I/O
// errors are logged to stderr so a corrupt review file is not papered over by
// silent fallback to "no scope inheritance".
func loadCritJSONForOutputDir(outputDir string) (CritJSON, bool) {
	critPath, err := review.ResolveReviewPath(outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot resolve review path: %v\n", err)
		return CritJSON{}, false
	}
	cj, err := review.LoadCritJSON(critPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return CritJSON{}, false
		}
		fmt.Fprintf(os.Stderr, "Warning: cannot read review file %q: %v\n", critPath, err)
		return CritJSON{}, false
	}
	return cj, true
}

// resolveCommentScope decides which scope tags `crit comment` should stamp,
// based on the --scope flag, a running daemon's Focus, and the on-disk
// ActiveDiffScope. Order of precedence per spec §C "crit comment scope inheritance".
func ResolveCommentScope(override CommentFocusOverride, outputDir string) (InheritedScope, error) {
	daemon := probeDaemonFocus()

	switch override {
	case ScopeOverrideWorkingTree:
		return InheritedScope{}, nil
	case ScopeOverrideFullStack:
		return resolveExplicitScope(daemon, outputDir, DiffScopeFullStack, "full_stack",
			"--scope=full-stack: no active full-stack focus to attach to (start `crit --pr <n> --scope=full-stack` first)")
	case ScopeOverrideLayer:
		return resolveExplicitScope(daemon, outputDir, DiffScopeLayer, "layer",
			"--scope=layer: no active layer focus to attach to (start `crit --pr <n>` first)")
	case ScopeOverrideUnset:
		return resolveAutoScope(daemon, outputDir), nil
	}
	return InheritedScope{}, fmt.Errorf("invalid --scope value %q", override)
}

// resolveExplicitScope handles the --scope=layer and --scope=full-stack cases:
// must match either the running daemon's diff scope or the on-disk ActiveDiffScope.
func resolveExplicitScope(daemon *Focus, outputDir string, want DiffScope, wantStr, errMsg string) (InheritedScope, error) {
	if daemon != nil && daemon.Kind == FocusRange && daemon.DiffScope == want {
		return InheritedScope{
			HeadSHA:   daemon.HeadSHA,
			BaseSHA:   daemon.BaseSHA,
			PRNumber:  daemon.PRNumber,
			DiffScope: wantStr,
		}, nil
	}
	if cj, ok := loadCritJSONForOutputDir(outputDir); ok && cj.ActiveDiffScope == wantStr {
		return InheritedScope{DiffScope: wantStr}, nil
	}
	return InheritedScope{}, fmt.Errorf("%s", errMsg)
}

// resolveAutoScope inherits from a running daemon's range focus (preferred) or
// falls back to the on-disk ActiveDiffScope (with a stderr note). Returns empty
// InheritedScope when neither is available.
func resolveAutoScope(daemon *Focus, outputDir string) InheritedScope {
	if daemon != nil && daemon.Kind == FocusRange {
		return InheritedScope{
			HeadSHA:   daemon.HeadSHA,
			BaseSHA:   daemon.BaseSHA,
			PRNumber:  daemon.PRNumber,
			DiffScope: string(daemon.DiffScope),
		}
	}
	if cj, ok := loadCritJSONForOutputDir(outputDir); ok && cj.ActiveDiffScope != "" {
		fmt.Fprintf(os.Stderr,
			"Note: stamping comment with diff_scope=%q from review file (no daemon running; head_sha unknown)\n",
			cj.ActiveDiffScope)
		return InheritedScope{DiffScope: cj.ActiveDiffScope}
	}
	return InheritedScope{}
}

// ResolvePullScope picks the (HeadSHA, DiffScope) pair stamped on imported
// GitHub PR comments. Pulled comments anchor to the PR's actual diff, so
// DiffScope is always "layer".
func ResolvePullScope(cj *CritJSON) InheritedScope {
	if focus := probeDaemonFocus(); focus != nil && focus.Kind == FocusRange {
		return InheritedScope{HeadSHA: focus.HeadSHA, DiffScope: "layer"}
	}
	if cj != nil && cj.ActiveDiffScope != "" {
		return InheritedScope{DiffScope: "layer"}
	}
	return InheritedScope{}
}

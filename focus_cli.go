package main

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
)

// commentFocusOverride captures the user's --scope flag for `crit comment`.
type commentFocusOverride string

const (
	scopeOverrideUnset       commentFocusOverride = ""
	scopeOverrideLayer       commentFocusOverride = "layer"
	scopeOverrideFullStack   commentFocusOverride = "full-stack"
	scopeOverrideWorkingTree commentFocusOverride = "working-tree"
)

// inheritedScope is the focus metadata stamped on comments authored via
// `crit comment`. All fields empty for working-tree mode. PRNumber and
// BaseSHA flow through to the comment's FocusKey so view-scoped visibility
// matches the daemon's view.
type inheritedScope struct {
	HeadSHA   string
	BaseSHA   string
	PRNumber  int
	DiffScope string // "layer" | "full_stack" | ""
}

// asFocus returns a synthetic Focus that produces the same stamping as
// inheritedScope when passed to stampWithFocus. Empty scope yields a
// working-tree focus that no-ops.
func (s inheritedScope) asFocus() Focus {
	if s.DiffScope == "" {
		return Focus{Kind: FocusWorkingTree}
	}
	return Focus{
		Kind:      FocusRange,
		HeadSHA:   s.HeadSHA,
		BaseSHA:   s.BaseSHA,
		PRNumber:  s.PRNumber,
		DiffScope: DiffScope(s.DiffScope),
	}
}

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
func resolveFocus(prSpec, rangeSpec, scopeSpec string, remoteFiles bool, vcs VCS, repoRoot string) (*Focus, error) {
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
		return resolveFocusFromPR(prSpec, scope, remoteFiles, vcs, repoRoot)
	case rangeSpec != "":
		return resolveFocusFromRange(rangeSpec, remoteFiles, vcs, repoRoot)
	}
	return nil, nil
}

func resolveFocusFromPR(prSpec string, scope DiffScope, remoteFiles bool, vcs VCS, repoRoot string) (*Focus, error) {
	prNum, err := parsePRSpec(prSpec)
	if err != nil {
		return nil, err
	}
	info, err := fetchPRByNumber(prNum)
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
		if err := ensureSHAFetched(vcs, info.BaseRefOid, repoRoot, ""); err != nil {
			return nil, err
		}
		if err := ensureSHAFetched(vcs, info.HeadRefOid, repoRoot, forkURL); err != nil {
			return nil, err
		}
	}

	defaultBranch := ""
	if vcs != nil {
		defaultBranch = vcs.DefaultBranch()
	}
	defaultSHA, _ := ResolveDefaultBranchSHA(vcs, repoRoot, defaultBranch)
	if scope == DiffScopeFullStack && defaultSHA == "" {
		return nil, fmt.Errorf("--scope=full-stack requires a resolvable default branch tip; got none for %q (detached HEAD or no remote?)", defaultBranch)
	}

	return &Focus{
		Kind:        FocusRange,
		PRNumber:    info.Number,
		PRURL:       info.URL,
		Label:       fmt.Sprintf("PR #%d: %s", info.Number, info.Title),
		BaseSHA:     info.BaseRefOid,
		HeadSHA:     info.HeadRefOid,
		DefaultSHA:  defaultSHA,
		ForkURL:     forkURL,
		BaseRefName: info.BaseRefName,
		HeadRefName: info.HeadRefName,
		DiffScope:   scope,
		IsStacked:   IsStackedPR(info, vcs),
	}, nil
}

func resolveFocusFromRange(rangeSpec string, remoteFiles bool, vcs VCS, repoRoot string) (*Focus, error) {
	base, head, err := parseRangeSpec(rangeSpec)
	if err != nil {
		return nil, err
	}
	// In --remote mode, content reads come from the GitHub API; we can't
	// prove the SHAs exist locally because we don't intend to use them.
	if !remoteFiles && vcs != nil {
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
func commentScopeOverrideFromFlag(s string) (commentFocusOverride, error) {
	if s == "" {
		return scopeOverrideUnset, nil
	}
	scope, isWorkingTree, err := normalizeScopeSpec(s)
	if err != nil {
		return "", fmt.Errorf("%w (expected layer | full-stack | working-tree)", err)
	}
	switch {
	case isWorkingTree:
		return scopeOverrideWorkingTree, nil
	case scope == DiffScopeFullStack:
		return scopeOverrideFullStack, nil
	default:
		return scopeOverrideLayer, nil
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
	cwd, err := resolvedCWD()
	if err != nil {
		return nil
	}
	sessions, _ := listSessionsForCWD(cwd)
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
		f := fetchSessionFocus(client, sess.Port)
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
func fetchSessionFocus(client *http.Client, port int) *Focus {
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/session", port))
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
	critPath, err := resolveReviewPath(outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot resolve review path: %v\n", err)
		return CritJSON{}, false
	}
	cj, err := loadCritJSON(critPath)
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
func resolveCommentScope(override commentFocusOverride, outputDir string) (inheritedScope, error) {
	daemon := probeDaemonFocus()

	switch override {
	case scopeOverrideWorkingTree:
		return inheritedScope{}, nil
	case scopeOverrideFullStack:
		return resolveExplicitScope(daemon, outputDir, DiffScopeFullStack, "full_stack",
			"--scope=full-stack: no active full-stack focus to attach to (start `crit --pr <n> --scope=full-stack` first)")
	case scopeOverrideLayer:
		return resolveExplicitScope(daemon, outputDir, DiffScopeLayer, "layer",
			"--scope=layer: no active layer focus to attach to (start `crit --pr <n>` first)")
	case scopeOverrideUnset:
		return resolveAutoScope(daemon, outputDir), nil
	}
	return inheritedScope{}, fmt.Errorf("invalid --scope value %q", override)
}

// resolveExplicitScope handles the --scope=layer and --scope=full-stack cases:
// must match either the running daemon's diff scope or the on-disk ActiveDiffScope.
func resolveExplicitScope(daemon *Focus, outputDir string, want DiffScope, wantStr, errMsg string) (inheritedScope, error) {
	if daemon != nil && daemon.Kind == FocusRange && daemon.DiffScope == want {
		return inheritedScope{
			HeadSHA:   daemon.HeadSHA,
			BaseSHA:   daemon.BaseSHA,
			PRNumber:  daemon.PRNumber,
			DiffScope: wantStr,
		}, nil
	}
	if cj, ok := loadCritJSONForOutputDir(outputDir); ok && cj.ActiveDiffScope == wantStr {
		return inheritedScope{DiffScope: wantStr}, nil
	}
	return inheritedScope{}, fmt.Errorf("%s", errMsg)
}

// resolveAutoScope inherits from a running daemon's range focus (preferred) or
// falls back to the on-disk ActiveDiffScope (with a stderr note). Returns empty
// inheritedScope when neither is available.
func resolveAutoScope(daemon *Focus, outputDir string) inheritedScope {
	if daemon != nil && daemon.Kind == FocusRange {
		return inheritedScope{
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
		return inheritedScope{DiffScope: cj.ActiveDiffScope}
	}
	return inheritedScope{}
}

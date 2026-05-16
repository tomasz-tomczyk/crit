package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const (
	jjRootCommitID = "0000000000000000000000000000000000000000"
	jjTrunkRevset  = "trunk()"
)

// JJVCS implements VCS for Jujutsu repositories. Crit treats JJ's working-copy
// commit as the review head and uses trunk()/bookmark resolution only to choose
// the base revision; staged/unstaged scopes are intentionally unavailable.
type JJVCS struct {
	defaultOnce   sync.Once
	defaultBranch string
	defaultBase   string
	overrideBase  string
	mu            sync.RWMutex
}

func (j *JJVCS) Name() string { return "jj" }

// RepoRoot returns the absolute path to the Jujutsu repository root.
func (j *JJVCS) RepoRoot() (string, error) {
	out, err := jjCommandInDir("", "root")
	if err != nil {
		return "", fmt.Errorf("jj root: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// CurrentBranch returns a stable session label: a bookmark at @ when present,
// otherwise JJ's short change id for the working-copy commit.
func (j *JJVCS) CurrentBranch() string {
	out, err := jjCommandInDir("", "log", "-r", "@", "--no-graph", "-T", "local_bookmarks ++ \"\\n\" ++ change_id.short() ++ \"\\n\"")
	if err != nil {
		return ""
	}
	lines := splitNonEmpty(out)
	if len(lines) == 0 {
		return ""
	}
	bookmarks := strings.Fields(lines[0])
	if len(bookmarks) > 0 {
		return bookmarks[0]
	}
	if len(lines) > 1 {
		return strings.TrimSpace(lines[1])
	}
	return ""
}

// DefaultBranch returns the display name for the default base. trunk() is the
// primary source; when JJ resolves trunk() to root in local-only repos, exact
// main/master/trunk bookmark lookup is used as a practical fallback.
func (j *JJVCS) DefaultBranch() string {
	j.mu.RLock()
	override := j.overrideBase
	j.mu.RUnlock()
	if override != "" {
		return override
	}
	j.defaultOnce.Do(func() {
		name, sha := resolveJJDefaultBaseInDir("")
		j.mu.Lock()
		j.defaultBranch = name
		j.defaultBase = sha
		j.mu.Unlock()
	})
	j.mu.RLock()
	defer j.mu.RUnlock()
	if j.overrideBase != "" {
		return j.overrideBase
	}
	return j.defaultBranch
}

// SetDefaultBranchOverride sets the base ref used for diffs. The value may be a
// bookmark, remote bookmark, commit id, or JJ revset accepted by jj log -r.
func (j *JJVCS) SetDefaultBranchOverride(branch string) {
	j.mu.Lock()
	j.overrideBase = branch
	j.mu.Unlock()
}

func (j *JJVCS) GetDefaultBranchOverride() string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.overrideBase
}

// DefaultBaseRef returns the commit id behind the default base. Returning a
// commit id avoids JJ revset alias shadowing for names like `main`.
func (j *JJVCS) DefaultBaseRef() string {
	j.mu.RLock()
	override := j.overrideBase
	j.mu.RUnlock()
	if override != "" {
		if override == jjTrunkRevset {
			_, sha := resolveJJDefaultBaseInDir("")
			if sha != "" {
				return sha
			}
		}
		if sha, err := resolveJJRevisionToCommitID("", override); err == nil {
			return sha
		}
		return override
	}
	_ = j.DefaultBranch()
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.defaultBase
}

// MergeBase returns the best common ancestor between @ and ref.
func (j *JJVCS) MergeBase(ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("base ref is empty")
	}
	base, err := resolveJJRevisionToCommitID("", ref)
	if err != nil {
		return "", err
	}
	return jjMergeBase("", "@", base)
}

func (j *JJVCS) ChangedFilesOnDefaultInDir(dir string) ([]FileChange, error) {
	out, err := jjCommandInDir(dir, "diff", "--summary", "-r", "@")
	if err != nil {
		return nil, err
	}
	return parseJJDiffSummary(out), nil
}

func (j *JJVCS) ChangedFilesFromBaseInDir(baseRef, dir string) ([]FileChange, error) {
	if strings.TrimSpace(baseRef) == "" {
		return j.ChangedFilesOnDefaultInDir(dir)
	}
	base, err := resolveJJRevisionToCommitID(dir, baseRef)
	if err != nil {
		return nil, err
	}
	out, err := jjCommandInDir(dir, "diff", "--summary", "--from", jjCommitRevset(base), "--to", "@")
	if err != nil {
		return nil, err
	}
	return parseJJDiffSummary(out), nil
}

// ChangedFilesScoped returns changed files for supported JJ scopes. JJ has no
// staging area, so staged and unstaged scopes are intentionally empty.
func (j *JJVCS) ChangedFilesScoped(scope, baseRef string) ([]FileChange, error) {
	if scope == "branch" {
		return j.ChangedFilesFromBaseInDir(baseRef, "")
	}
	return nil, nil
}

func (j *JJVCS) ChangedFilesForCommit(sha, dir string) ([]FileChange, error) {
	rev, err := resolveJJRevisionToCommitID(dir, sha)
	if err != nil {
		return nil, err
	}
	out, err := jjCommandInDir(dir, "diff", "--summary", "-r", jjCommitRevset(rev))
	if err != nil {
		return nil, err
	}
	return parseJJDiffSummary(out), nil
}

func (j *JJVCS) FileDiffUnified(path, baseRef, dir string) ([]DiffHunk, error) {
	return j.FileDiffUnifiedCtx(context.Background(), path, baseRef, dir)
}

func (j *JJVCS) FileDiffUnifiedCtx(ctx context.Context, path, baseRef, dir string) ([]DiffHunk, error) {
	args := []string{"diff", "--git"}
	if strings.TrimSpace(baseRef) == "" {
		args = append(args, "-r", "@")
	} else {
		base, err := resolveJJRevisionToCommitID(dir, baseRef)
		if err != nil {
			return nil, err
		}
		args = append(args, "--from", jjCommitRevset(base), "--to", "@")
	}
	args = append(args, "--", path)
	out, err := jjCommandInDirCtx(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	return ParseUnifiedDiff(out), nil
}

func (j *JJVCS) FileDiffScoped(path, scope, baseRef, dir string) ([]DiffHunk, error) {
	if scope == "branch" {
		return j.FileDiffUnified(path, baseRef, dir)
	}
	return nil, nil
}

func (j *JJVCS) FileDiffForCommit(path, sha, dir string) ([]DiffHunk, error) {
	rev, err := resolveJJRevisionToCommitID(dir, sha)
	if err != nil {
		return nil, err
	}
	out, err := jjCommandInDir(dir, "diff", "--git", "-r", jjCommitRevset(rev), "--", path)
	if err != nil {
		return nil, err
	}
	return ParseUnifiedDiff(out), nil
}

func (j *JJVCS) FileDiffUnifiedNewFile(path string) ([]DiffHunk, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return FileDiffUnifiedNewFile(string(data)), nil
}

func (j *JJVCS) CommitLog(baseRef, headRef, dir string) ([]CommitInfo, error) {
	if strings.TrimSpace(baseRef) == "" {
		return nil, nil
	}
	base, err := resolveJJRevisionToCommitID(dir, baseRef)
	if err != nil {
		return nil, err
	}
	headRev := "@"
	if strings.TrimSpace(headRef) != "" {
		head, err := resolveJJRevisionToCommitID(dir, headRef)
		if err != nil {
			return nil, err
		}
		headRev = jjCommitRevset(head)
	}
	tpl := "commit_id ++ \"\\n\" ++ commit_id.short() ++ \"\\n\" ++ description.first_line() ++ \"\\n\" ++ author.name() ++ \"\\n\" ++ author.timestamp().format(\"%Y-%m-%dT%H:%M:%S%:z\") ++ \"\\n---\\n\""
	revset := fmt.Sprintf("%s::%s ~ %s", jjCommitRevset(base), headRev, jjCommitRevset(base))
	out, err := jjCommandInDir(dir, "log", "-r", revset, "--no-graph", "-T", tpl)
	if err != nil {
		return nil, err
	}
	return parseSaplingCommitLog(out), nil
}

func (j *JJVCS) WorkingTreeFingerprint() string {
	out, err := jjCommandInDir("", "status")
	if err != nil {
		return ""
	}
	return out
}

func (j *JJVCS) UntrackedFiles(_ string) ([]FileChange, error) {
	return nil, nil
}

func (j *JJVCS) AllTrackedFiles(dir string) ([]string, error) {
	out, err := jjCommandInDir(dir, "file", "list")
	if err != nil {
		return nil, err
	}
	return splitNonEmpty(out), nil
}

func (j *JJVCS) RemoteBranches(dir string) ([]string, error) {
	out, err := jjCommandInDir(dir, "bookmark", "list", "--all-remotes", "-T", "name ++ \"|\" ++ remote ++ \"\\n\"")
	if err != nil {
		return nil, nil //nolint:nilerr // remote bookmarks may not be configured
	}
	seen := map[string]bool{}
	var branches []string
	for _, line := range splitNonEmpty(out) {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 || parts[1] == "" || parts[1] == "git" {
			continue
		}
		if !seen[parts[0]] {
			seen[parts[0]] = true
			branches = append(branches, parts[0])
		}
	}
	return branches, nil
}

func (j *JJVCS) DiffNumstat(baseRef, dir string) (map[string]NumstatEntry, error) {
	if strings.TrimSpace(baseRef) == "" {
		return nil, nil
	}
	base, err := resolveJJRevisionToCommitID(dir, baseRef)
	if err != nil {
		return nil, err
	}
	out, err := jjCommandInDir(dir, "diff", "--stat", "--from", jjCommitRevset(base), "--to", "@")
	if err != nil {
		return nil, err
	}
	return parseSaplingDiffStat(out), nil
}

func (j *JJVCS) UserName() string { return jjUserName() }

func (j *JJVCS) FileContentAtRef(path, ref, dir string) (string, error) {
	data, err := j.ReadFileAtSHA(ref, path, dir)
	if err != nil || data == nil {
		return "", err
	}
	return string(data), nil
}

func (j *JJVCS) ChangedFilesBetweenSHAs(baseSHA, headSHA, dir string) ([]FileChange, error) {
	base, err := resolveJJRevisionToCommitID(dir, baseSHA)
	if err != nil {
		return nil, err
	}
	head, err := resolveJJRevisionToCommitID(dir, headSHA)
	if err != nil {
		return nil, err
	}
	out, err := jjCommandInDir(dir, "diff", "--summary", "--from", jjCommitRevset(base), "--to", jjCommitRevset(head))
	if err != nil {
		return nil, err
	}
	return parseJJDiffSummary(out), nil
}

func (j *JJVCS) FileDiffBetweenSHAs(path, baseSHA, headSHA, dir string) ([]DiffHunk, error) {
	base, err := resolveJJRevisionToCommitID(dir, baseSHA)
	if err != nil {
		return nil, err
	}
	head, err := resolveJJRevisionToCommitID(dir, headSHA)
	if err != nil {
		return nil, err
	}
	out, err := jjCommandInDir(dir, "diff", "--git", "--from", jjCommitRevset(base), "--to", jjCommitRevset(head), "--", path)
	if err != nil {
		return nil, err
	}
	return ParseUnifiedDiff(out), nil
}

func (j *JJVCS) ReadFileAtSHA(sha, path, dir string) ([]byte, error) {
	rev, err := resolveJJRevisionToCommitID(dir, sha)
	if err != nil {
		return nil, err
	}
	out, err := jjCommandBytesInDir(dir, "file", "show", "-r", jjCommitRevset(rev), "--", path)
	if err != nil {
		return nil, nil //nolint:nilerr // a missing path at a valid commit is not an error for callers
	}
	return out, nil
}

func (j *JJVCS) HasObject(sha, dir string) bool {
	_, err := resolveJJRevisionToCommitID(dir, sha)
	return err == nil
}

func (j *JJVCS) FileStatusInRepo(path, baseRef, dir string) string {
	changes, err := j.ChangedFilesFromBaseInDir(baseRef, dir)
	if err != nil {
		return ""
	}
	for _, fc := range changes {
		if fc.Path == path {
			return fc.Status
		}
	}
	return ""
}

func (j *JJVCS) HasStagingArea() bool { return false }

func (j *JJVCS) SkipDirNames() []string { return []string{".jj", ".git"} }

func jjCommandInDir(dir string, args ...string) (string, error) {
	out, err := jjCommandBytesInDir(dir, args...)
	return string(out), err
}

func jjCommandInDirCtx(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := jjCommandBytesInDirCtx(ctx, dir, args...)
	return string(out), err
}

func jjCommandBytesInDir(dir string, args ...string) ([]byte, error) {
	return jjCommandBytesInDirCtx(context.Background(), dir, args...)
}

func jjCommandBytesInDirCtx(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"--no-pager", "--color", "never"}, args...)
	cmd := exec.CommandContext(ctx, "jj", cmdArgs...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return stdout.Bytes(), fmt.Errorf("jj %s: %w: %s", strings.Join(args, " "), err, msg)
		}
		return stdout.Bytes(), fmt.Errorf("jj %s: %w", strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

func resolveJJDefaultBaseInDir(dir string) (name, sha string) {
	trunk, err := jjCommitIDForRevset(dir, jjTrunkRevset)
	if err == nil && trunk != "" && !isJJRootCommitID(trunk) {
		if branch := jjKnownDefaultNameForCommit(dir, trunk); branch != "" {
			return branch, trunk
		}
		return jjTrunkRevset, trunk
	}
	for _, candidate := range []string{"main", "master", "trunk"} {
		if sha, err := resolveJJBookmarkToCommitID(dir, candidate); err == nil && sha != "" && !isJJRootCommitID(sha) {
			return candidate, sha
		}
	}
	return "", ""
}

func jjKnownDefaultNameForCommit(dir, sha string) string {
	for _, candidate := range []string{"main", "master", "trunk"} {
		if refSHA, err := resolveJJBookmarkToCommitID(dir, candidate); err == nil && refSHA == sha {
			return candidate
		}
	}
	return ""
}

func resolveJJBookmarkToCommitID(dir, name string) (string, error) {
	for _, revset := range []string{
		fmt.Sprintf("remote_bookmarks(exact:%q, exact:%q)", name, "origin"),
		fmt.Sprintf("remote_bookmarks(exact:%q, exact:%q)", name, "upstream"),
		fmt.Sprintf("bookmarks(exact:%q)", name),
		fmt.Sprintf("remote_bookmarks(exact:%q, exact:%q)", name, "git"),
		fmt.Sprintf("remote_bookmarks(exact:%q)", name),
	} {
		sha, err := jjCommitIDForRevset(dir, revset)
		if err == nil && sha != "" {
			return sha, nil
		}
	}
	return "", fmt.Errorf("jj bookmark %q not found", name)
}

func resolveJJRevisionToCommitID(dir, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("empty JJ revision")
	}
	if looksLikeHexPrefix(ref) {
		if sha, err := jjCommitIDForRevset(dir, jjCommitRevset(ref)); err == nil && sha != "" {
			return sha, nil
		}
	}
	if isSimpleJJBookmarkName(ref) {
		if sha, err := resolveJJBookmarkToCommitID(dir, ref); err == nil {
			return sha, nil
		}
	}
	if sha, err := jjCommitIDForRevset(dir, ref); err == nil && sha != "" {
		return sha, nil
	}
	if sha, err := jjCommitIDForRevset(dir, jjCommitRevset(ref)); err == nil && sha != "" {
		return sha, nil
	}
	return "", fmt.Errorf("could not resolve JJ revision %q", ref)
}

func jjCommitIDForRevset(dir, revset string) (string, error) {
	out, err := jjCommandInDir(dir, "log", "-r", revset, "--no-graph", "-T", "commit_id ++ \"\\n\"")
	if err != nil {
		return "", err
	}
	ids := splitNonEmpty(out)
	if len(ids) == 0 {
		return "", fmt.Errorf("JJ revset %q resolved to no commits", revset)
	}
	return strings.TrimSpace(ids[0]), nil
}

func jjMergeBase(dir, headRef, baseSHA string) (string, error) {
	head, err := resolveJJRevisionToCommitID(dir, headRef)
	if err != nil {
		return "", err
	}
	revset := fmt.Sprintf("heads(ancestors(%s) & ancestors(%s))", jjCommitRevset(head), jjCommitRevset(baseSHA))
	return jjCommitIDForRevset(dir, revset)
}

func jjTopicChainRevset(dir string, maxDepth int) string {
	limit := ""
	if maxDepth > 0 {
		limit = fmt.Sprintf(", %d", maxDepth)
	}
	baseName, baseSHA := resolveJJDefaultBaseInDir(dir)
	if baseName != "" && baseSHA != "" && !isJJRootCommitID(baseSHA) {
		return fmt.Sprintf("ancestors(@%s) ~ ancestors(%s)", limit, jjCommitRevset(baseSHA))
	}
	return fmt.Sprintf("ancestors(@%s) ~ root()", limit)
}

func jjCommitSubject(dir, sha string) string {
	rev, err := resolveJJRevisionToCommitID(dir, sha)
	if err != nil {
		return ""
	}
	out, err := jjCommandInDir(dir, "log", "-r", jjCommitRevset(rev), "--no-graph", "-T", "description.first_line()")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func jjCommitRevset(sha string) string {
	return fmt.Sprintf("commit_id(%q)", strings.TrimSpace(sha))
}

func isJJRootCommitID(sha string) bool {
	return strings.TrimSpace(sha) == jjRootCommitID
}

func looksLikeHexPrefix(s string) bool {
	if len(s) < 4 {
		return false
	}
	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}

func isSimpleJJBookmarkName(s string) bool {
	if s == "" || strings.Contains(s, "@") {
		return false
	}
	return !strings.ContainsAny(s, " ()|&~:")
}

// jjUserName returns the JJ-configured user name, or empty on error.
func jjUserName() string {
	out, err := jjCommandInDir("", "config", "get", "user.name")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func hasJJDirAt(repoRoot string) bool {
	info, err := os.Stat(filepath.Join(repoRoot, ".jj"))
	return err == nil && info.IsDir()
}

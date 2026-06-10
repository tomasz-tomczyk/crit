package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// TestNewSessionFromFiles_SymlinkedRepoRoot is a regression test for the macOS
// /var → /private/var symlink mismatch. git rev-parse --show-toplevel always
// returns the canonicalized (symlink-resolved) repo root, while the file paths
// a user passes on the command line may travel through a symlink (e.g. a CI
// checkout under /var that resolves to /private/var). Before canonicalAbsPath /
// canonicalRepoRoot canonicalized BOTH sides, filepath.Rel compared a symlinked
// absolute path against the resolved repo root and produced a "../../../" escape
// string. NewSessionFromFiles then fell back to storing the bare absolute path,
// which (a) is not repo-relative and (b) causes the same file to be listed twice
// when review.json keys (resolved) don't match the session path (symlinked) —
// the original plan.md duplication symptom in file-mode sessions.
//
// The earlier TestNewSessionFromFiles_UsesRepoRelativePaths only uses t.TempDir(),
// which on macOS happens to live under the /var symlink but on Linux CI does not,
// so it never exercises the divergence. This test creates an explicit sibling
// symlink and drives the session through it, so it fails on every platform that
// supports symlink creation if the canonicalization is reverted.
func TestNewSessionFromFiles_SymlinkedRepoRoot(t *testing.T) {
	// Real directory that git will canonicalize to.
	realDir := t.TempDir()

	// Sibling symlink pointing at the real directory. Skip where the platform
	// or sandbox forbids symlink creation (notably unprivileged Windows).
	linkDir := filepath.Join(filepath.Dir(realDir), "crit-symlink-"+filepath.Base(realDir))
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("cannot create symlink on this platform: %v", err)
	}
	t.Cleanup(func() { os.Remove(linkDir) })

	// Initialize a git repo in the REAL directory with two committed files.
	gitT(t, realDir, "init")
	gitT(t, realDir, "config", "user.email", "test@test.com")
	gitT(t, realDir, "config", "user.name", "Test")
	gitT(t, realDir, "checkout", "-b", "main")
	writeFile(t, filepath.Join(realDir, "plan.md"), "# Plan\n")
	writeFile(t, filepath.Join(realDir, "main.go"), "package main\n")
	gitT(t, realDir, "add", "-A")
	gitT(t, realDir, "commit", "-m", "init")

	// Confirm the symlink actually diverges from its target. On a filesystem
	// where the temp dir is already canonical (no /var symlink), there is
	// nothing to regress, so skip rather than pass vacuously.
	resolvedReal, err := filepath.EvalSymlinks(realDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(realDir): %v", err)
	}
	resolvedLink, err := filepath.EvalSymlinks(linkDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(linkDir): %v", err)
	}
	if resolvedLink != resolvedReal {
		t.Fatalf("symlink does not resolve to real dir: %q vs %q", resolvedLink, resolvedReal)
	}

	// Operate from inside the symlinked path. git rev-parse --show-toplevel
	// (used by resolveGitContext → RepoRoot) returns the resolved real path,
	// while the file arguments below are addressed through the symlink — this
	// is exactly the /var vs /private/var split the fix canonicalizes away.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(linkDir); err != nil {
		t.Fatalf("Chdir(linkDir): %v", err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	// Override the default branch the same way resolveServerConfig does, so the
	// session resolves a base without depending on the ambient environment.
	vcs.SetDefaultBranchOverride("main")
	t.Cleanup(func() { vcs.SetDefaultBranchOverride("") })

	// Pass absolute paths THROUGH the symlink. With the fix, canonicalAbsPath
	// resolves these to the real path before filepath.Rel against the (also
	// canonical) repo root, yielding clean repo-relative keys. Without the fix,
	// Rel produces a "../../" escape and the code falls back to the absolute
	// path — reproducing the regression.
	planThroughLink := filepath.Join(linkDir, "plan.md")
	mainThroughLink := filepath.Join(linkDir, "main.go")

	session, err := NewSessionFromFiles([]string{planThroughLink, mainThroughLink}, nil)
	if err != nil {
		t.Fatalf("NewSessionFromFiles: %v", err)
	}

	// Symptom 1: no duplicate entries. The dedup in expandAndDedupPaths keys on
	// the canonicalized path; if both sides aren't canonical, the symlinked and
	// resolved spellings of the same file can survive as two entries.
	seen := make(map[string]int)
	for _, f := range session.Files {
		seen[f.Path]++
	}
	for path, n := range seen {
		if n > 1 {
			t.Errorf("file %q listed %d times; want exactly 1 (duplicate-entry regression)", path, n)
		}
	}
	if len(session.Files) != 2 {
		t.Fatalf("files = %d, want 2 (plan.md, main.go)", len(session.Files))
	}

	// Symptom 2: stored paths are clean repo-relative keys, not the absolute
	// fallback. This is the assertion that flips to failing if the
	// canonicalization in canonicalAbsPath/canonicalRepoRoot is reverted.
	wantPaths := map[string]bool{"plan.md": false, "main.go": false}
	for _, f := range session.Files {
		if filepath.IsAbs(f.Path) {
			t.Errorf("file path %q is absolute; want repo-relative (symlink divergence leaked into the session)", f.Path)
		}
		if strings.HasPrefix(f.Path, "../") || strings.Contains(f.Path, "/../") {
			t.Errorf("file path %q contains parent hops; canonicalization did not normalize the symlink", f.Path)
		}
		if _, ok := wantPaths[f.Path]; ok {
			wantPaths[f.Path] = true
		}
	}
	for path, found := range wantPaths {
		if !found {
			t.Errorf("expected repo-relative file %q in session, not present", path)
		}
	}

	// The repo root itself must be the canonical (resolved) path so that
	// downstream review.json keys line up with on-disk lookups.
	if session.RepoRoot != resolvedReal {
		t.Errorf("RepoRoot = %q, want canonical %q", session.RepoRoot, resolvedReal)
	}
}

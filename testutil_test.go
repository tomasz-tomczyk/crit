package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// mustMkdirAll ensures the parent directory of path exists, then returns path.
// Used by tests that seed a file inside the v4 review folder layout without
// writing the boilerplate at every call site.
func mustMkdirAll(path string) string {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		panic(err)
	}
	return path
}

// Strip GIT_* from the test process env so production code paths that exec
// git (e.g. NewSessionFromGit) don't target the parent repo when tests run
// inside a git hook (pre-commit's `go test ./...` inherits GIT_DIR /
// GIT_INDEX_FILE / GIT_WORK_TREE from the commit that triggered it).
func init() {
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			continue
		}
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		os.Unsetenv(kv[:eq])
	}
}

// initTestRepo creates a temp directory with a git repo and returns the path.
// The repo has an initial commit on the "main" branch.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitT(t, dir, "init")
	gitT(t, dir, "config", "user.email", "test@test.com")
	gitT(t, dir, "config", "user.name", "Test")
	// Create initial commit
	writeFile(t, filepath.Join(dir, "README.md"), "# Test")
	gitT(t, dir, "add", "README.md")
	gitT(t, dir, "commit", "-m", "initial")
	// Ensure default branch is "main"
	gitT(t, dir, "branch", "-M", "main")
	return dir
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Strip GIT_* and HOME from the inherited env. When tests run inside a
	// git hook (e.g. pre-commit's `go test ./...`), git sets GIT_DIR /
	// GIT_INDEX_FILE / GIT_WORK_TREE for the hook subprocess; without this
	// filter, every git op below would target the parent repo instead of
	// the test's tempdir. HOME is filtered so our explicit override below
	// is unambiguous (exec.Cmd treats duplicate env entries as platform-
	// dependent).
	src := os.Environ()
	env := make([]string, 0, len(src)+4)
	for _, kv := range src {
		if strings.HasPrefix(kv, "GIT_") || strings.HasPrefix(kv, "HOME=") {
			continue
		}
		// On Windows git also picks up USERPROFILE / HOMEDRIVE / HOMEPATH;
		// strip them so our explicit HOME override is unambiguous.
		if runtime.GOOS == "windows" && (strings.HasPrefix(kv, "USERPROFILE=") ||
			strings.HasPrefix(kv, "HOMEDRIVE=") || strings.HasPrefix(kv, "HOMEPATH=")) {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
	if runtime.GOOS == "windows" {
		env = append(env, "USERPROFILE="+dir)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// setHome sets HOME to dir for the duration of the test. On Windows
// os.UserHomeDir() reads USERPROFILE (and HOMEDRIVE+HOMEPATH) instead of
// HOME, so we set those too — otherwise tests that expect their config
// writes to land in dir leak into the real user profile.
//
// Pass "" to simulate "no home" — this also clears the Windows fallbacks.
func setHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
		// HOMEDRIVE/HOMEPATH together form a fallback used by os.UserHomeDir.
		// Setting both empty strings is enough — os.UserHomeDir ignores them
		// when either is empty.
		t.Setenv("HOMEDRIVE", "")
		t.Setenv("HOMEPATH", "")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// flushWrites stops any pending debounced write timer on the session.
// Call this before WriteFiles() in tests to prevent the timer from
// firing concurrently with explicit writes.
func flushWrites(s *Session) {
	s.mu.Lock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.mu.Unlock()
}

// quiesceSession cancels any pending debounced write and waits for any
// in-flight write callback to finish. Use it from t.Cleanup in tests that
// call AddComment so t.TempDir's RemoveAll does not race with a delayed
// disk write — on Windows that race manifests as "directory is not empty".
func quiesceSession(t *testing.T, s *Session) {
	t.Helper()
	s.mu.Lock()
	if s.writeTimer != nil {
		s.writeTimer.Stop()
	}
	s.writeGen++
	s.mu.Unlock()
	// writeMu is held by an in-flight callback for the duration of the
	// write; acquiring then releasing it ensures any callback that already
	// passed the timer Stop above runs to completion before we return.
	s.writeMu.Lock()
	//nolint:staticcheck // SA2001: intentional drain of in-flight write callback
	s.writeMu.Unlock()
}

// TestGitEnvLeakStripped guards against the testutil_test.go GIT_* env leak
// that previously corrupted the parent worktree when `go test ./...` was
// invoked from a pre-commit hook. See the init() and runGit() comments above.
func TestGitEnvLeakStripped(t *testing.T) {
	for _, k := range []string{"GIT_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Fatalf("%s still set after init(): %q", k, v)
		}
	}
	// Even if a test sets GIT_DIR for its own purposes, runGit must not
	// honor it when operating on its tempdir.
	t.Setenv("GIT_DIR", "/should/be/ignored")
	dir := t.TempDir()
	gitT(t, dir, "init")
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf(".git not created in tempdir — GIT_DIR leaked into runGit: %v", err)
	}
}

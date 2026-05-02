package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
	env := make([]string, 0, len(src)+2)
	for _, kv := range src {
		if strings.HasPrefix(kv, "GIT_") || strings.HasPrefix(kv, "HOME=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
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

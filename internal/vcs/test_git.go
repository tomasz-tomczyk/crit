package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// InitTestRepo creates a temp git repo with an initial commit on main.
func InitTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitForTest(t, dir, "init")
	runGitForTest(t, dir, "config", "user.email", "test@test.com")
	runGitForTest(t, dir, "config", "user.name", "Test")
	writeFileForTest(t, filepath.Join(dir, "README.md"), "# Test")
	runGitForTest(t, dir, "add", "README.md")
	runGitForTest(t, dir, "commit", "-m", "initial")
	runGitForTest(t, dir, "branch", "-M", "main")
	return dir
}

// GitRun runs git in dir and returns trimmed stdout.
func GitRun(t *testing.T, dir string, args ...string) string {
	return runGitForTest(t, dir, args...)
}

// CommitAtForTest writes a file and creates a single commit at dir, returning HEAD SHA.
func CommitAtForTest(t *testing.T, dir, path, content, msg string) string {
	t.Helper()
	writeFileForTest(t, filepath.Join(dir, path), content)
	runGitForTest(t, dir, "add", "-A")
	runGitForTest(t, dir, "commit", "-m", msg)
	return runGitForTest(t, dir, "rev-parse", "HEAD")
}

// IsCommitish reports whether s is a safe ref token (hex SHA, 4–40 chars).
func IsCommitish(s string) bool {
	return isCommitish(s)
}

func writeFileForTest(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGitForTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	src := os.Environ()
	env := make([]string, 0, len(src)+4)
	for _, kv := range src {
		if strings.HasPrefix(kv, "GIT_") || strings.HasPrefix(kv, "HOME=") {
			continue
		}
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

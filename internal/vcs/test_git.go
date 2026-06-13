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

// ClearGitEnvForTest unsets git's repo-location environment variables for the
// duration of the test, restoring them on cleanup. Git hooks (e.g. pre-commit)
// export GIT_DIR / GIT_WORK_TREE, which take precedence over a command's working
// directory — so a production git-in-dir call would resolve the hook's repo
// instead of the temp repo under test. Tests that exercise such calls against a
// temp repo must call this to stay deterministic when run from inside a hook.
func ClearGitEnvForTest(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE",
		"GIT_OBJECT_DIRECTORY", "GIT_PREFIX", "GIT_CEILING_DIRECTORIES", "GIT_NAMESPACE",
	} {
		if v, ok := os.LookupEnv(k); ok {
			k, v := k, v
			if err := os.Unsetenv(k); err != nil {
				t.Fatalf("unset %s: %v", k, err)
			}
			t.Cleanup(func() { os.Setenv(k, v) })
		}
	}
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

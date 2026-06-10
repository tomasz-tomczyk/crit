package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var frontendFS = FrontendFS

func initTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCommit(t, dir, "init")
	gitCommit(t, dir, "config", "user.email", "test@test.com")
	gitCommit(t, dir, "config", "user.name", "Test")
	writeGitFile(t, filepath.Join(dir, "README.md"), "# Test")
	gitCommit(t, dir, "add", "README.md")
	gitCommit(t, dir, "commit", "-m", "initial")
	gitCommit(t, dir, "branch", "-M", "main")
	return dir
}

func gitCommit(t *testing.T, dir string, args ...string) string {
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

func writeGitFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

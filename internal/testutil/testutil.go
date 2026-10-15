package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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

// SetHome sets HOME (and Windows profile vars) for the duration of the test.
func SetHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("CODEX_HOME", "")
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", dir)
		t.Setenv("HOMEDRIVE", "")
		t.Setenv("HOMEPATH", "")
	}
}

// InitTestRepo creates a temp git repo with an initial commit on main.
func InitTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// `init -b` and writing the identity straight into .git/config replace
	// three extra git spawns per fixture (config x2, branch -M); process
	// startup dominates test time on Windows.
	Git(t, dir, "init", "-b", "main")
	cfg, err := os.OpenFile(filepath.Join(dir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.WriteString("[user]\n\temail = test@test.com\n\tname = Test\n")
	if cerr := cfg.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatal(err)
	}
	WriteFile(t, filepath.Join(dir, "README.md"), "# Test")
	Git(t, dir, "add", "README.md")
	Git(t, dir, "commit", "-m", "initial")
	return dir
}

// Git runs a git command in dir with a sanitized environment.
func Git(t *testing.T, dir string, args ...string) string {
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

// WriteFile writes content to path, creating parent directories as needed.
func WriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// MustMkdirAll ensures the parent directory of path exists, then returns path.
func MustMkdirAll(path string) string {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		panic(err)
	}
	return path
}

// RunWithTempHome runs a package's tests with a throwaway HOME, for use from
// TestMain. Review sessions, config and auth live under ~/.crit; a crit daemon
// running in the same checkout (or the developer's own config) must not change
// test results. Helper subprocesses inherit it.
//
// Go's own cache, module cache and env file default to locations under HOME,
// so they are pinned to their original paths first; otherwise a test that runs
// `go build` (e.g. to spawn the crit binary) rebuilds from scratch and
// re-downloads every module. The startup integration and update checks are
// disabled because they probe agent CLIs on the developer's PATH (seconds per
// spawned daemon) and call the network.
func RunWithTempHome(m *testing.M) int {
	PinGoEnvBeforeHomeChange()
	os.Setenv("CRIT_NO_INTEGRATION_CHECK", "1")
	os.Setenv("CRIT_NO_UPDATE_CHECK", "1")
	home, err := os.MkdirTemp("", "crit-test-home-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(home)
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	os.Setenv("CODEX_HOME", "")
	return m.Run()
}

// PinGoEnvBeforeHomeChange sets GOENV, GOCACHE and GOMODCACHE to the paths the
// go command would derive from the current HOME, unless the environment or the
// user's go env file (`go env -w`) already sets them. Call it before
// redirecting HOME so `go` subprocesses keep the warm caches.
func PinGoEnvBeforeHomeChange() {
	if os.Getenv("GOENV") == "" {
		if dir, err := os.UserConfigDir(); err == nil {
			os.Setenv("GOENV", filepath.Join(dir, "go", "env"))
		}
	}
	fileSet := map[string]bool{}
	// GOENV=off means the go command reads no env file.
	if goenv := os.Getenv("GOENV"); goenv != "off" {
		data, _ := os.ReadFile(goenv)
		for _, line := range strings.Split(string(data), "\n") {
			if k, _, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
				fileSet[k] = true
			}
		}
	}
	unset := func(k string) bool { return os.Getenv(k) == "" && !fileSet[k] }
	if unset("GOCACHE") {
		if dir, err := os.UserCacheDir(); err == nil {
			os.Setenv("GOCACHE", filepath.Join(dir, "go-build"))
		}
	}
	if unset("GOMODCACHE") && unset("GOPATH") {
		if home, err := os.UserHomeDir(); err == nil {
			os.Setenv("GOMODCACHE", filepath.Join(home, "go", "pkg", "mod"))
		}
	}
}

// PrependFailingForgeCLIs puts `gh` and `glab` stubs that always fail first on
// PATH and returns a cleanup func. Code under test that shells out to them
// (auth status, pr view/list) then fails fast and deterministically instead of
// depending on whether the developer is logged in, and without spawning the
// real CLIs or reaching the network. For use from TestMain; tests that need a
// specific gh behaviour still install their own stub via t.Setenv("PATH").
func PrependFailingForgeCLIs() func() {
	dir, err := os.MkdirTemp("", "crit-test-forge-stubs-")
	if err != nil {
		panic(err)
	}
	for _, name := range []string{"gh", "glab"} {
		path := filepath.Join(dir, name)
		content := "#!/bin/sh\necho \"" + name + ": stubbed out in tests\" >&2\nexit 1\n"
		if runtime.GOOS == "windows" {
			path += ".bat"
			content = "@echo " + name + ": stubbed out in tests 1>&2\r\n@exit /b 1\r\n"
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			panic(err)
		}
	}
	os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() { os.RemoveAll(dir) }
}

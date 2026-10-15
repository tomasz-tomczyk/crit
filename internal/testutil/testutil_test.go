package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(RunWithTempHome(m)) }

func TestRunWithTempHomeSetsIsolatedEnv(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.Base(home), "crit-test-home-") {
		t.Errorf("home = %q, want a crit-test-home- temp dir", home)
	}
	for _, key := range []string{"CRIT_NO_INTEGRATION_CHECK", "CRIT_NO_UPDATE_CHECK"} {
		if os.Getenv(key) == "" {
			t.Errorf("%s is not set", key)
		}
	}
}

// clearGoEnv blanks the variables PinGoEnvBeforeHomeChange reads and writes;
// t.Setenv restores them after the test.
func clearGoEnv(t *testing.T, goenv string) {
	t.Helper()
	t.Setenv("GOENV", goenv)
	t.Setenv("GOCACHE", "")
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOPATH", "")
}

func TestPinGoEnvBeforeHomeChange(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	wantModCache := filepath.Join(home, "go", "pkg", "mod")
	wantCache := filepath.Join(cacheDir, "go-build")

	t.Run("pins caches when nothing sets them", func(t *testing.T) {
		clearGoEnv(t, "off")
		PinGoEnvBeforeHomeChange()
		if got := os.Getenv("GOCACHE"); got != wantCache {
			t.Errorf("GOCACHE = %q, want %q", got, wantCache)
		}
		if got := os.Getenv("GOMODCACHE"); got != wantModCache {
			t.Errorf("GOMODCACHE = %q, want %q", got, wantModCache)
		}
	})

	t.Run("leaves values from the go env file alone", func(t *testing.T) {
		envFile := filepath.Join(t.TempDir(), "env")
		WriteFile(t, envFile, "GOCACHE=/from/env/file\nGOPATH=/from/env/file/gopath\n")
		clearGoEnv(t, envFile)
		PinGoEnvBeforeHomeChange()
		if got := os.Getenv("GOCACHE"); got != "" {
			t.Errorf("GOCACHE = %q, want unset (the env file sets it)", got)
		}
		if got := os.Getenv("GOMODCACHE"); got != "" {
			t.Errorf("GOMODCACHE = %q, want unset (the env file sets GOPATH)", got)
		}
	})

	t.Run("defaults GOENV to the user config dir", func(t *testing.T) {
		configDir, err := os.UserConfigDir()
		if err != nil {
			t.Skipf("no user config dir: %v", err)
		}
		clearGoEnv(t, "")
		PinGoEnvBeforeHomeChange()
		if got, want := os.Getenv("GOENV"), filepath.Join(configDir, "go", "env"); got != want {
			t.Errorf("GOENV = %q, want %q", got, want)
		}
	})
}

func TestPrependFailingForgeCLIs(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH")) // restore after the test
	cleanup := PrependFailingForgeCLIs()

	var stubDir string
	for _, name := range []string{"gh", "glab"} {
		out, err := exec.Command(name, "auth", "status").CombinedOutput()
		if err == nil {
			t.Errorf("%s auth status succeeded, want the stub to fail", name)
		}
		if want := name + ": stubbed out in tests"; !strings.Contains(string(out), want) {
			t.Errorf("%s output = %q, want it to contain %q", name, out, want)
		}
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		stubDir = filepath.Dir(path)
	}

	cleanup()
	if _, err := os.Stat(stubDir); !os.IsNotExist(err) {
		t.Errorf("stub dir %s still exists after cleanup (err=%v)", stubDir, err)
	}
}

func TestInitTestRepo(t *testing.T) {
	dir := InitTestRepo(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"branch", "--show-current"}, "main"},
		{[]string{"config", "user.email"}, "test@test.com"},
		{[]string{"config", "user.name"}, "Test"},
		{[]string{"log", "--format=%s"}, "initial"},
	} {
		if got := Git(t, dir, tc.args...); strings.TrimSpace(got) != tc.want {
			t.Errorf("git %s = %q, want %q", strings.Join(tc.args, " "), got, tc.want)
		}
	}
}

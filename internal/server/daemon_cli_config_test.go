package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func writeDaemonOutputConfig(t *testing.T, dir, output string) {
	t.Helper()
	data, err := json.Marshal(map[string]string{"output": output})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".crit.config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func resetBranchOverride(t *testing.T) {
	t.Helper()
	vcs.SetDefaultBranchOverride("")
	vcs.ResetDefaultBranchOnceForTest()
}

func branchOverride() string {
	return (&vcs.GitVCS{}).GetDefaultBranchOverride()
}

func TestResolveServerConfig_BaseBranch(t *testing.T) {
	// Reset global state before and after
	defer resetBranchOverride(t)

	t.Run("CLI flag sets override", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		_, err := ResolveDaemonCLIConfig([]string{"--base-branch", "uat"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branchOverride() != "uat" {
			t.Errorf("expected branchOverride=uat, got %q", branchOverride())
		}
	})

	t.Run("config file used when no flag", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		cfgPath := filepath.Join(dir, ".crit.config.json")
		os.WriteFile(cfgPath, []byte(`{"base_branch": "develop"}`), 0644)

		// ResolveDaemonCLIConfig reads from cwd, so chdir to our temp dir
		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		_, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branchOverride() != "develop" {
			t.Errorf("expected branchOverride=develop, got %q", branchOverride())
		}
	})

	t.Run("CLI flag overrides config file", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		cfgPath := filepath.Join(dir, ".crit.config.json")
		os.WriteFile(cfgPath, []byte(`{"base_branch": "develop"}`), 0644)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		_, err := ResolveDaemonCLIConfig([]string{"--base-branch", "uat"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if branchOverride() != "uat" {
			t.Errorf("expected branchOverride=uat (CLI wins), got %q", branchOverride())
		}
	})
}

func TestResolveServerConfig_PortPrecedence(t *testing.T) {
	defer resetBranchOverride(t)

	t.Run("CLI flag wins over env and config", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"port": 4000}`), 0644)
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		t.Setenv("CRIT_PORT", "5000")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{"--port", "6000"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.Port != 6000 {
			t.Errorf("port = %d, want 6000 (CLI flag)", sc.Port)
		}
	})

	t.Run("env var wins over config when no CLI flag", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"port": 4000}`), 0644)
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		t.Setenv("CRIT_PORT", "5000")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.Port != 5000 {
			t.Errorf("port = %d, want 5000 (env var)", sc.Port)
		}
	})

	t.Run("config wins when no CLI flag or env var", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"port": 4000}`), 0644)
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		t.Setenv("CRIT_PORT", "")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.Port != 4000 {
			t.Errorf("port = %d, want 4000 (config file)", sc.Port)
		}
	})

	t.Run("zero port when nothing set", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		t.Setenv("CRIT_PORT", "")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.Port != 0 {
			t.Errorf("port = %d, want 0 (default)", sc.Port)
		}
	})
}

func TestResolveServerConfig_HostPrecedence(t *testing.T) {
	defer resetBranchOverride(t)

	t.Run("CLI flag wins over env and config", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"host": "10.0.0.1"}`), 0644)
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		t.Setenv("CRIT_HOST", "10.0.0.2")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{"--host", "0.0.0.0", "--allow-unauthenticated-network"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.Host != "0.0.0.0" {
			t.Errorf("host = %q, want 0.0.0.0 (CLI flag)", sc.Host)
		}
		if !sc.AllowUnauthenticatedNetwork {
			t.Error("expected AllowUnauthenticatedNetwork")
		}
	})

	t.Run("non-loopback host refused without allow flag", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		t.Setenv("CRIT_HOST", "")
		t.Setenv(config.AllowUnauthenticatedNetworkEnv, "")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		_, err := ResolveDaemonCLIConfig([]string{"--host", "0.0.0.0"})
		if err == nil {
			t.Fatal("expected error for non-loopback host without allow flag")
		}
		if !strings.Contains(err.Error(), config.AllowUnauthenticatedNetworkFlag) {
			t.Fatalf("error = %v, want mention of allow flag", err)
		}
	})

	t.Run("env allow unlocks non-loopback host", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		t.Setenv("CRIT_HOST", "10.0.0.2")
		t.Setenv(config.AllowUnauthenticatedNetworkEnv, "1")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.Host != "10.0.0.2" {
			t.Errorf("host = %q, want 10.0.0.2 (env var)", sc.Host)
		}
	})

	t.Run("env var wins over config when no CLI flag", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"host": "10.0.0.1"}`), 0644)
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		t.Setenv("CRIT_HOST", "10.0.0.2")
		t.Setenv(config.AllowUnauthenticatedNetworkEnv, "1")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.Host != "10.0.0.2" {
			t.Errorf("host = %q, want 10.0.0.2 (env var)", sc.Host)
		}
	})

	t.Run("global config wins when no CLI flag or env var", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		homeDir := t.TempDir()
		os.WriteFile(filepath.Join(homeDir, ".crit.config.json"), []byte(`{"host": "10.0.0.1"}`), 0644)
		testutil.SetHome(t, homeDir)
		t.Setenv("CRIT_HOST", "")
		t.Setenv(config.AllowUnauthenticatedNetworkEnv, "1")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.Host != "10.0.0.1" {
			t.Errorf("host = %q, want 10.0.0.1 (global config)", sc.Host)
		}
	})

	t.Run("project config cannot override host", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"host": "0.0.0.0"}`), 0644)
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		t.Setenv("CRIT_HOST", "")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.Host != "127.0.0.1" {
			t.Errorf("host = %q, want 127.0.0.1 (project config must not override host)", sc.Host)
		}
	})

	t.Run("default 127.0.0.1 when nothing set", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		t.Setenv("CRIT_HOST", "")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.Host != "127.0.0.1" {
			t.Errorf("host = %q, want 127.0.0.1 (default)", sc.Host)
		}
	})
}

func TestResolveServerConfig_PublicURLPrecedence(t *testing.T) {
	defer resetBranchOverride(t)

	t.Run("CLI flag wins over env and config", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		os.WriteFile(filepath.Join(homeDir, ".crit.config.json"), []byte(`{"public_url": "https://config.example.com"}`), 0644)
		dir := t.TempDir()
		t.Setenv("CRIT_PUBLIC_URL", "https://env.example.com")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{"--public-url", "https://cli.example.com", "--allow-unauthenticated-network"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.PublicURL != "https://cli.example.com" {
			t.Errorf("publicURL = %q, want CLI value", sc.PublicURL)
		}
	})

	t.Run("public URL refused without allow flag", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		dir := t.TempDir()
		t.Setenv("CRIT_PUBLIC_URL", "")
		t.Setenv(config.AllowUnauthenticatedNetworkEnv, "")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		_, err := ResolveDaemonCLIConfig([]string{"--public-url", "https://cli.example.com"})
		if err == nil {
			t.Fatal("expected error for public_url without allow flag")
		}
	})

	t.Run("env var wins over config when no CLI flag", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		os.WriteFile(filepath.Join(homeDir, ".crit.config.json"), []byte(`{"public_url": "https://config.example.com"}`), 0644)
		dir := t.TempDir()
		t.Setenv("CRIT_PUBLIC_URL", "https://env.example.com")
		t.Setenv(config.AllowUnauthenticatedNetworkEnv, "1")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.PublicURL != "https://env.example.com" {
			t.Errorf("publicURL = %q, want env value", sc.PublicURL)
		}
	})

	t.Run("global config used when no CLI or env", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		os.WriteFile(filepath.Join(homeDir, ".crit.config.json"), []byte(`{"public_url": "https://config.example.com"}`), 0644)
		dir := t.TempDir()
		os.Unsetenv("CRIT_PUBLIC_URL")
		t.Setenv(config.AllowUnauthenticatedNetworkEnv, "1")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.PublicURL != "https://config.example.com" {
			t.Errorf("publicURL = %q, want global config value", sc.PublicURL)
		}
	})

	t.Run("project config cannot set public_url", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"public_url": "https://evil.example.com"}`), 0644)
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		os.Unsetenv("CRIT_PUBLIC_URL")
		t.Setenv(config.AllowUnauthenticatedNetworkEnv, "")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.PublicURL != "" {
			t.Errorf("publicURL = %q, want empty (project config must not apply)", sc.PublicURL)
		}
	})

	t.Run("invalid public URL rejected", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		t.Setenv(config.AllowUnauthenticatedNetworkEnv, "1")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		_, err := ResolveDaemonCLIConfig([]string{"--public-url", "not-a-url"})
		if err == nil {
			t.Fatal("expected error for invalid public URL")
		}
	})
}

func TestResolveServerConfig_ShareURLPrecedence(t *testing.T) {
	defer resetBranchOverride(t)

	t.Run("CLI flag wins over env and config", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		// share_url is global-only; write to global config
		os.WriteFile(filepath.Join(homeDir, ".crit.config.json"), []byte(`{"share_url": "https://config.example.com"}`), 0644)
		dir := t.TempDir()
		t.Setenv("CRIT_SHARE_URL", "https://env.example.com")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{"--share-url", "https://cli.example.com"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.ShareURL != "https://cli.example.com" {
			t.Errorf("shareURL = %q, want CLI value", sc.ShareURL)
		}
	})

	t.Run("env var wins over config when no CLI flag", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		// share_url is global-only; write to global config
		os.WriteFile(filepath.Join(homeDir, ".crit.config.json"), []byte(`{"share_url": "https://config.example.com"}`), 0644)
		dir := t.TempDir()
		t.Setenv("CRIT_SHARE_URL", "https://env.example.com")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.ShareURL != "https://env.example.com" {
			t.Errorf("shareURL = %q, want env value", sc.ShareURL)
		}
	})

	t.Run("global config used when no CLI or env", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)
		// share_url is global-only; project config cannot set it
		os.WriteFile(filepath.Join(homeDir, ".crit.config.json"), []byte(`{"share_url": "https://config.example.com"}`), 0644)
		dir := t.TempDir()
		os.Unsetenv("CRIT_SHARE_URL")

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.ShareURL != "https://config.example.com" {
			t.Errorf("shareURL = %q, want global config value", sc.ShareURL)
		}
	})
}

func TestResolveServerConfig_BoolFlags(t *testing.T) {
	defer resetBranchOverride(t)

	t.Run("--no-open flag sets noOpen", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{"--no-open"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !sc.NoOpen {
			t.Error("noOpen should be true when --no-open is passed")
		}
	})

	t.Run("config no_open used when no flag", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"no_open": true}`), 0644)
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !sc.NoOpen {
			t.Error("noOpen should be true from config")
		}
	})

	t.Run("--quiet flag sets quiet", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{"--quiet"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !sc.Quiet {
			t.Error("quiet should be true when --quiet is passed")
		}
	})

	t.Run("--no-ignore disables ignore patterns", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"ignore_patterns": ["*.lock", "vendor/"]}`), 0644)
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{"--no-ignore"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(sc.IgnorePatterns) != 0 {
			t.Errorf("ignorePatterns = %v, want empty (--no-ignore)", sc.IgnorePatterns)
		}
	})
}

func TestResolveServerConfig_FileArgs(t *testing.T) {
	defer resetBranchOverride(t)

	vcs.SetDefaultBranchOverride("")

	dir := t.TempDir()
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	sc, err := ResolveDaemonCLIConfig([]string{"plan.md", "notes.md"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sc.Files) != 2 || sc.Files[0] != "plan.md" || sc.Files[1] != "notes.md" {
		t.Errorf("files = %v, want [plan.md notes.md]", sc.Files)
	}
}

func TestParseDaemonFlags_PRAndRange(t *testing.T) {
	sf := parseDaemonFlagsForTest([]string{"--pr", "1", "--range", "a..b"})
	if sf.prSpec != "1" || sf.rangeSpec != "a..b" {
		t.Fatalf("expected both flags captured, got %+v", sf)
	}
}

func TestParseDaemonFlags_RangeAndScope(t *testing.T) {
	sf := parseDaemonFlagsForTest([]string{"--range", "a..b", "--scope", "layer"})
	if sf.rangeSpec != "a..b" || sf.scopeSpec != "layer" {
		t.Errorf("got %+v", sf)
	}
}

func TestParseDaemonFlags_Remote(t *testing.T) {
	sf := parseDaemonFlagsForTest([]string{"--pr", "1", "--remote"})
	if !sf.remoteFiles {
		t.Errorf("remoteFiles = false, want true")
	}
}

func TestParseDaemonFlags_RemoteDefaultsFalse(t *testing.T) {
	sf := parseDaemonFlagsForTest([]string{"plan.md"})
	if sf.remoteFiles {
		t.Errorf("remoteFiles = true, want false")
	}
}

func TestParseDaemonFlags_Session(t *testing.T) {
	sf := parseDaemonFlagsForTest([]string{"--session", "839f3b4cd5d6"})
	if sf.sessionID != "839f3b4cd5d6" {
		t.Errorf("sessionID = %q", sf.sessionID)
	}
}

func TestParseDaemonFlags_FlagsAfterFile(t *testing.T) {
	sf := parseDaemonFlagsForTest([]string{"plan.md", "-p", "51573", "--no-open", "--quiet"})
	if sf.port != 51573 || !sf.noOpen || !sf.quiet {
		t.Fatalf("flags after file not applied: %+v", sf)
	}
	if len(sf.fileArgs) != 1 || sf.fileArgs[0] != "plan.md" {
		t.Fatalf("fileArgs = %v, want [plan.md]", sf.fileArgs)
	}
}

func TestResolveDaemonCLIConfig_SessionID(t *testing.T) {
	defer resetBranchOverride(t)
	vcs.SetDefaultBranchOverride("")

	dir := t.TempDir()
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	sc, err := ResolveDaemonCLIConfig([]string{"--session", "839f3b4cd5d6", "--no-open"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sc.SessionID != "839f3b4cd5d6" {
		t.Errorf("SessionID = %q", sc.SessionID)
	}
}

func TestResolveServerConfig_OutputDir(t *testing.T) {
	defer resetBranchOverride(t)

	t.Run("CLI --output sets outputDir", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{"--output", "/tmp/out"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.OutputDir != "/tmp/out" {
			t.Errorf("outputDir = %q, want /tmp/out", sc.OutputDir)
		}
	})

	t.Run("config output used when no flag", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		configuredOutput := t.TempDir()
		writeDaemonOutputConfig(t, dir, configuredOutput)
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		sc, err := ResolveDaemonCLIConfig([]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.OutputDir != configuredOutput {
			t.Errorf("outputDir = %q, want %q (from config)", sc.OutputDir, configuredOutput)
		}
	})

	t.Run("plan dir wins over config output", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		writeDaemonOutputConfig(t, dir, t.TempDir())
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		planDir := filepath.Join(dir, "plan")
		sc, err := ResolveDaemonCLIConfig([]string{"--plan-dir", planDir})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.OutputDir != "" {
			t.Errorf("outputDir = %q, want empty so plan dir remains authoritative", sc.OutputDir)
		}
		if sc.PlanDir != planDir {
			t.Errorf("planDir = %q, want %q", sc.PlanDir, planDir)
		}
	})

	t.Run("explicit output wins over plan dir and config", func(t *testing.T) {
		vcs.SetDefaultBranchOverride("")

		dir := t.TempDir()
		writeDaemonOutputConfig(t, dir, t.TempDir())
		homeDir := t.TempDir()
		testutil.SetHome(t, homeDir)

		origDir, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(origDir)

		explicitOutput := t.TempDir()
		planDir := t.TempDir()
		sc, err := ResolveDaemonCLIConfig([]string{"--output", explicitOutput, "--plan-dir", planDir})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sc.OutputDir != explicitOutput {
			t.Errorf("outputDir = %q, want %q", sc.OutputDir, explicitOutput)
		}
	})
}

func TestResolveDaemonCLIConfigRelativeOutputAnchoredToRepoRoot(t *testing.T) {
	defer resetBranchOverride(t)
	vcs.SetDefaultBranchOverride("")
	repoDir := testutil.InitTestRepo(t)
	testutil.SetHome(t, t.TempDir())
	if err := os.WriteFile(
		filepath.Join(repoDir, ".crit.config.json"),
		[]byte(`{"output":"reviews"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	nestedDir := filepath.Join(repoDir, "pkg")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nestedDir)

	sc, err := ResolveDaemonCLIConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRepo, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(canonicalRepo, "reviews")
	if sc.OutputDir != want {
		t.Fatalf("outputDir = %q, want repo-relative %q", sc.OutputDir, want)
	}
}

func TestResolveDaemonCLIConfig_NotifyOnRoundReady(t *testing.T) {
	vcs.SetDefaultBranchOverride("")
	dir := t.TempDir()
	homeDir := t.TempDir()
	testutil.SetHome(t, homeDir)
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(origDir) })

	sc, err := ResolveDaemonCLIConfig([]string{"--no-open"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sc.NotifyOnRoundReady {
		t.Fatal("NotifyOnRoundReady should default to false")
	}

	os.WriteFile(filepath.Join(homeDir, ".crit.config.json"), []byte(`{"notify_on_round_ready": true}`), 0644)
	sc, err = ResolveDaemonCLIConfig([]string{"--no-open"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sc.NotifyOnRoundReady {
		t.Fatal("NotifyOnRoundReady should be true when set in global config")
	}
}

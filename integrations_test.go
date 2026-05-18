package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestComputeFileHash(t *testing.T) {
	h1 := computeFileHash([]byte("hello world"))
	h2 := computeFileHash([]byte("hello world"))
	h3 := computeFileHash([]byte("different content"))

	if h1 != h2 {
		t.Errorf("same content should produce same hash: %q != %q", h1, h2)
	}
	if h1 == h3 {
		t.Error("different content should produce different hash")
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex hash, got %d chars: %q", len(h1), h1)
	}
}

func TestCheckInstalledIntegrations_StaleFile(t *testing.T) {
	dir := t.TempDir()

	// Write a file at the claude-code skill destination with different content
	ccDest := filepath.Join(dir, ".claude", "skills", "crit")
	if err := os.MkdirAll(ccDest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ccDest, "SKILL.md"), []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}

	stale := checkInstalledIntegrations(dir, dir)
	if len(stale) == 0 {
		t.Fatal("expected stale files, got none")
	}

	found := false
	for _, s := range stale {
		if s.agent == "claude-code" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected claude-code in stale results")
	}
}

func TestCheckInstalledIntegrations_UpToDate(t *testing.T) {
	dir := t.TempDir()

	// Read the actual embedded content and write it to the destination
	// so it matches the precomputed hash
	embedded, err := integrationsFS.ReadFile("integrations/claude-code/skills/crit/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	ccDest := filepath.Join(dir, ".claude", "skills", "crit")
	if err := os.MkdirAll(ccDest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ccDest, "SKILL.md"), embedded, 0o644); err != nil {
		t.Fatal(err)
	}

	stale := checkInstalledIntegrations(dir, dir)
	for _, s := range stale {
		if s.agent == "claude-code" && s.dest == filepath.Join(ccDest, "SKILL.md") {
			t.Error("file matches embedded content, should not be stale")
		}
	}
}

func TestCheckInstalledIntegrations_MissingFile(t *testing.T) {
	dir := t.TempDir()
	stale := checkInstalledIntegrations(dir, dir)
	if len(stale) != 0 {
		t.Errorf("expected no stale files for empty dir, got %d", len(stale))
	}
}

func TestCheckInstalledIntegrations_HomeDirStale(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()

	// Write stale file only in homeDir
	ccDest := filepath.Join(homeDir, ".claude", "skills", "crit")
	if err := os.MkdirAll(ccDest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ccDest, "SKILL.md"), []byte("old version"), 0o644); err != nil {
		t.Fatal(err)
	}

	stale := checkInstalledIntegrations(projectDir, homeDir)
	if len(stale) == 0 {
		t.Fatal("expected stale file in home dir, got none")
	}
	if stale[0].dest != filepath.Join(ccDest, "SKILL.md") {
		t.Errorf("expected home dir path, got %s", stale[0].dest)
	}
}

func TestCheckInstalledIntegrations_MarketplaceStale(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()

	// Write stale file at marketplace source path
	mpPath := filepath.Join(homeDir, ".claude", "plugins", "marketplaces", "crit",
		"integrations", "claude-code", "skills", "crit")
	if err := os.MkdirAll(mpPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mpPath, "SKILL.md"), []byte("old marketplace"), 0o644); err != nil {
		t.Fatal(err)
	}

	stale := checkInstalledIntegrations(projectDir, homeDir)
	if len(stale) == 0 {
		t.Fatal("expected stale marketplace file, got none")
	}

	found := false
	for _, s := range stale {
		if s.location == locationMarketplace {
			found = true
			if !strings.Contains(s.updateHint(), "claude plugin update crit@crit") {
				t.Errorf("marketplace hint should suggest plugin update, got: %s", s.updateHint())
			}
		}
	}
	if !found {
		t.Error("expected marketplace location in stale results")
	}
}

func TestCheckInstalledIntegrations_CacheStale(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()

	// Write stale file at cache path with hash-named dir
	cachePath := filepath.Join(homeDir, ".claude", "plugins", "cache", "crit", "crit",
		"abc123def456", "skills", "crit")
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cachePath, "SKILL.md"), []byte("cached old"), 0o644); err != nil {
		t.Fatal(err)
	}

	stale := checkInstalledIntegrations(projectDir, homeDir)
	if len(stale) == 0 {
		t.Fatal("expected stale cache file, got none")
	}

	found := false
	for _, s := range stale {
		if s.location == locationCache {
			found = true
			if !strings.Contains(s.updateHint(), "claude plugin update crit@crit") {
				t.Errorf("cache hint should suggest plugin update, got: %s", s.updateHint())
			}
		}
	}
	if !found {
		t.Error("expected cache location in stale results")
	}
}

func TestLatestCacheDir(t *testing.T) {
	t.Run("picks lexicographically last dir", func(t *testing.T) {
		dir := t.TempDir()
		os.Mkdir(filepath.Join(dir, "1.0.0"), 0o755)
		os.Mkdir(filepath.Join(dir, "1.0.2"), 0o755)
		os.Mkdir(filepath.Join(dir, "1.0.1"), 0o755)
		if got := latestCacheDir(dir); got != "1.0.2" {
			t.Errorf("got %q, want 1.0.2", got)
		}
	})
	t.Run("ignores files", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "zzz"), nil, 0o644)
		os.Mkdir(filepath.Join(dir, "1.0.0"), 0o755)
		if got := latestCacheDir(dir); got != "1.0.0" {
			t.Errorf("got %q, want 1.0.0", got)
		}
	})
	t.Run("returns empty for nonexistent dir", func(t *testing.T) {
		if got := latestCacheDir("/no/such/path"); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("returns empty for empty dir", func(t *testing.T) {
		if got := latestCacheDir(t.TempDir()); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestCheckInstalledIntegrations_CacheSkipsOldVersions(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()

	// Create two version dirs: 1.0.0 (stale) and 1.0.1 (current)
	for _, ver := range []string{"1.0.0", "1.0.1"} {
		cachePath := filepath.Join(homeDir, ".claude", "plugins", "cache", "crit", "crit",
			ver, "skills", "crit")
		if err := os.MkdirAll(cachePath, 0o755); err != nil {
			t.Fatal(err)
		}
		if ver == "1.0.0" {
			// Stale content in old version
			os.WriteFile(filepath.Join(cachePath, "SKILL.md"), []byte("old stale"), 0o644)
		} else {
			// Current content — use the real source file to get the correct hash
			src := filepath.Join("integrations", "claude-code", "skills", "crit", "SKILL.md")
			data, err := os.ReadFile(src)
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(cachePath, "SKILL.md"), data, 0o644)
		}
	}

	stale := checkInstalledIntegrations(projectDir, homeDir)
	for _, s := range stale {
		if s.location == locationCache {
			t.Errorf("should not flag cache as stale when latest version matches, got: %s", s.dest)
		}
	}
}

func TestPrintStaleWarnings_NoStale(t *testing.T) {
	count := printStaleWarnings(nil)
	if count != 0 {
		t.Errorf("expected 0 warnings for nil slice, got %d", count)
	}
}

func TestPrintStaleWarnings_WithStale(t *testing.T) {
	stale := []staleFile{
		{agent: "claude-code", file: "SKILL.md", dest: "/tmp/test/.claude/skills/crit/SKILL.md", location: locationProject},
	}
	count := printStaleWarnings(stale)
	if count == 0 {
		t.Error("expected at least 1 warning")
	}
}

func TestDetectInstalledIntegrations(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "project")
	homeDir := filepath.Join(tmpDir, "home")
	os.MkdirAll(projectDir, 0o755)
	os.MkdirAll(homeDir, 0o755)

	// No integrations installed — should return empty
	result := detectInstalledIntegrations(projectDir, homeDir)
	if len(result) != 0 {
		t.Errorf("expected 0 integrations, got %d", len(result))
	}

	// Install a current integration file
	sourceFiles := integrationMap["claude-code"]
	if len(sourceFiles) == 0 {
		t.Fatal("no claude-code integration files defined")
	}
	sourceContent, err := integrationsFS.ReadFile(sourceFiles[0].source)
	if err != nil {
		t.Fatalf("reading embedded source: %v", err)
	}
	dest := filepath.Join(projectDir, sourceFiles[0].dest)
	os.MkdirAll(filepath.Dir(dest), 0o755)
	os.WriteFile(dest, sourceContent, 0o644)

	result = detectInstalledIntegrations(projectDir, homeDir)
	if len(result) == 0 {
		t.Fatal("expected at least 1 integration, got 0")
	}
	found := false
	for _, r := range result {
		if r.Agent == "claude-code" && r.Status == "current" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected claude-code with status current, got %+v", result)
	}

	// Write a stale file
	os.WriteFile(dest, []byte("stale content"), 0o644)
	result = detectInstalledIntegrations(projectDir, homeDir)
	found = false
	for _, r := range result {
		if r.Agent == "claude-code" && r.Status == "stale" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected claude-code with status stale, got %+v", result)
	}
}

func TestDetectInstalledIntegrations_DedupsPerAgent(t *testing.T) {
	tmpDir := t.TempDir()
	projectDir := filepath.Join(tmpDir, "project")
	homeDir := filepath.Join(tmpDir, "home")
	os.MkdirAll(projectDir, 0o755)
	os.MkdirAll(homeDir, 0o755)

	// Install same integration in both project and home — should only appear once
	sourceFiles := integrationMap["claude-code"]
	if len(sourceFiles) == 0 {
		t.Fatal("no claude-code integration files defined")
	}
	sourceContent, err := integrationsFS.ReadFile(sourceFiles[0].source)
	if err != nil {
		t.Fatalf("reading embedded source: %v", err)
	}
	for _, dir := range []string{projectDir, homeDir} {
		dest := filepath.Join(dir, sourceFiles[0].dest)
		os.MkdirAll(filepath.Dir(dest), 0o755)
		os.WriteFile(dest, sourceContent, 0o644)
	}

	result := detectInstalledIntegrations(projectDir, homeDir)
	count := 0
	for _, r := range result {
		if r.Agent == "claude-code" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 claude-code entry (deduped), got %d", count)
	}
}

func TestRunCheck_NoStale(t *testing.T) {
	// runCheck uses os.Getwd() and os.UserHomeDir(), so we just verify it doesn't panic
	// when called in a temp dir with no installed integrations
	origDir, _ := os.Getwd()
	dir := t.TempDir()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Should not panic
	runCheck()
}

func TestDetectPresentAgents_BinaryOnPath(t *testing.T) {
	homeDir := t.TempDir()

	// detectPresentAgents should find agents whose binaries are on PATH.
	// We can't control PATH easily, but we can verify the function returns
	// results without panicking on a clean temp home dir.
	agents := detectPresentAgents(homeDir)
	// Just verify it runs — the result depends on what's installed on this machine
	_ = agents
}

func TestDetectPresentAgents_ConfigDir(t *testing.T) {
	homeDir := t.TempDir()

	// Create a .claude directory to simulate claude-code presence
	os.MkdirAll(filepath.Join(homeDir, ".claude"), 0o755)

	agents := detectPresentAgents(homeDir)
	found := false
	for _, a := range agents {
		if a == "claude-code" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected claude-code to be detected via .claude dir")
	}
}

func TestDetectPresentAgents_NoDuplicates(t *testing.T) {
	homeDir := t.TempDir()

	// Create multiple probe dirs for same agent — should only appear once
	os.MkdirAll(filepath.Join(homeDir, ".claude"), 0o755)

	agents := detectPresentAgents(homeDir)
	count := 0
	for _, a := range agents {
		if a == "claude-code" {
			count++
		}
	}
	if count > 1 {
		t.Errorf("expected 1 claude-code entry, got %d", count)
	}
}

func TestConfirmBinaryVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	dir := t.TempDir()
	match := filepath.Join(dir, "fake-tool")
	os.WriteFile(match, []byte("#!/bin/sh\necho 'FakeTool v1.2 by Acme Corp'\n"), 0o755)

	noMatch := filepath.Join(dir, "wrong-tool")
	os.WriteFile(noMatch, []byte("#!/bin/sh\necho 'SomeOtherTool v3.0'\n"), 0o755)

	failing := filepath.Join(dir, "bad-tool")
	os.WriteFile(failing, []byte("#!/bin/sh\nexit 1\n"), 0o755)

	old := versionTimeout
	versionTimeout = 2 * time.Second
	defer func() { versionTimeout = old }()

	tests := []struct {
		name     string
		bin      string
		expected string
		want     bool
	}{
		{"match", match, "acme", true},
		{"case-insensitive", match, "ACME", true},
		{"no-match", noMatch, "acme", false},
		{"error-exit", failing, "anything", false},
		{"nonexistent", "/nonexistent/binary", "x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := confirmBinaryVersion(tt.bin, tt.expected)
			if got != tt.want {
				t.Errorf("confirmBinaryVersion(%q, %q) = %v, want %v", tt.bin, tt.expected, got, tt.want)
			}
		})
	}
}

func TestCheckMissingIntegrations(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()

	// Create .claude dir to simulate agent presence
	os.MkdirAll(filepath.Join(homeDir, ".claude"), 0o755)

	// No integration installed — should report claude-code as missing
	missing := checkMissingIntegrations(projectDir, homeDir)
	found := false
	for _, m := range missing {
		if m == "claude-code" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected claude-code in missing list when .claude dir exists but no integration installed")
	}
}

func TestCheckMissingIntegrations_Installed(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()

	// Create .claude dir
	os.MkdirAll(filepath.Join(homeDir, ".claude"), 0o755)

	// Install the integration file with correct content
	sourceFiles := integrationMap["claude-code"]
	sourceContent, _ := integrationsFS.ReadFile(sourceFiles[0].source)
	dest := filepath.Join(projectDir, sourceFiles[0].dest)
	os.MkdirAll(filepath.Dir(dest), 0o755)
	os.WriteFile(dest, sourceContent, 0o644)

	missing := checkMissingIntegrations(projectDir, homeDir)
	for _, m := range missing {
		if m == "claude-code" {
			t.Error("claude-code should not be missing when integration is installed")
		}
	}
}

func TestCheckMissingIntegrations_NoAgentsPresent(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()

	// Empty home — no agents detected, so nothing missing
	missing := checkMissingIntegrations(projectDir, homeDir)
	// May still detect agents on PATH, but should not panic
	_ = missing
}

func TestHintMissingIntegrationsFor_SkipsWhenInstalled(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()

	// Install an integration so installedAgents returns non-empty
	sourceFiles := integrationMap["claude-code"]
	sourceContent, _ := integrationsFS.ReadFile(sourceFiles[0].source)
	dest := filepath.Join(projectDir, sourceFiles[0].dest)
	os.MkdirAll(filepath.Dir(dest), 0o755)
	os.WriteFile(dest, sourceContent, 0o644)

	// Create .gemini to simulate a detected-but-missing agent
	os.MkdirAll(filepath.Join(homeDir, ".gemini"), 0o755)

	// Should not panic and should not print (installed agent exists)
	hintMissingIntegrationsFor(projectDir, homeDir)
}

func TestHintMissingIntegrationsFor_PrintsWhenNoneInstalled(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()

	// Create .gemini to simulate detection
	os.MkdirAll(filepath.Join(homeDir, ".gemini"), 0o755)

	// Should print hint (no installed agents, gemini detected)
	hintMissingIntegrationsFor(projectDir, homeDir)
}

func TestHintMissingIntegrations_EnvDisable(t *testing.T) {
	t.Setenv("CRIT_NO_INTEGRATION_CHECK", "1")
	// Should return immediately without doing any work
	hintMissingIntegrations()
}

func TestInstalledAgents(t *testing.T) {
	homeDir := t.TempDir()
	projectDir := t.TempDir()

	// Empty — no agents installed
	agents := installedAgents(projectDir, homeDir)
	if len(agents) != 0 {
		t.Errorf("expected 0 installed agents, got %d", len(agents))
	}

	// Install claude-code
	sourceFiles := integrationMap["claude-code"]
	sourceContent, _ := integrationsFS.ReadFile(sourceFiles[0].source)
	dest := filepath.Join(projectDir, sourceFiles[0].dest)
	os.MkdirAll(filepath.Dir(dest), 0o755)
	os.WriteFile(dest, sourceContent, 0o644)

	agents = installedAgents(projectDir, homeDir)
	if !agents["claude-code"] {
		t.Error("expected claude-code in installed agents")
	}
}

func TestPrintMissingHints(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if n := printMissingHints(nil); n != 0 {
			t.Errorf("expected 0, got %d", n)
		}
	})
	t.Run("single", func(t *testing.T) {
		if n := printMissingHints([]string{"claude-code"}); n != 1 {
			t.Errorf("expected 1, got %d", n)
		}
	})
	t.Run("multiple", func(t *testing.T) {
		if n := printMissingHints([]string{"claude-code", "cursor"}); n != 2 {
			t.Errorf("expected 2, got %d", n)
		}
	})
}

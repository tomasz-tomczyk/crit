package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	// Write a file at the claude-code command destination with different content
	ccDest := filepath.Join(dir, ".claude", "commands")
	if err := os.MkdirAll(ccDest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ccDest, "crit.md"), []byte("old content"), 0o644); err != nil {
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
	embedded, err := integrationsFS.ReadFile("integrations/claude-code/commands/crit.md")
	if err != nil {
		t.Fatal(err)
	}
	ccDest := filepath.Join(dir, ".claude", "commands")
	if err := os.MkdirAll(ccDest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ccDest, "crit.md"), embedded, 0o644); err != nil {
		t.Fatal(err)
	}

	stale := checkInstalledIntegrations(dir, dir)
	for _, s := range stale {
		if s.agent == "claude-code" && s.dest == filepath.Join(ccDest, "crit.md") {
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
	ccDest := filepath.Join(homeDir, ".claude", "commands")
	if err := os.MkdirAll(ccDest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ccDest, "crit.md"), []byte("old version"), 0o644); err != nil {
		t.Fatal(err)
	}

	stale := checkInstalledIntegrations(projectDir, homeDir)
	if len(stale) == 0 {
		t.Fatal("expected stale file in home dir, got none")
	}
	if stale[0].dest != filepath.Join(ccDest, "crit.md") {
		t.Errorf("expected home dir path, got %s", stale[0].dest)
	}
}

func TestCheckInstalledIntegrations_MarketplaceStale(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()

	// Write stale file at marketplace source path
	mpPath := filepath.Join(homeDir, ".claude", "plugins", "marketplaces", "crit",
		"integrations", "claude-code", "commands")
	if err := os.MkdirAll(mpPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mpPath, "crit.md"), []byte("old marketplace"), 0o644); err != nil {
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
		"abc123def456", "commands")
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cachePath, "crit.md"), []byte("cached old"), 0o644); err != nil {
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
			ver, "commands")
		if err := os.MkdirAll(cachePath, 0o755); err != nil {
			t.Fatal(err)
		}
		if ver == "1.0.0" {
			// Stale content in old version
			os.WriteFile(filepath.Join(cachePath, "crit.md"), []byte("old stale"), 0o644)
		} else {
			// Current content — use the real source file to get the correct hash
			src := filepath.Join("integrations", "claude-code", "commands", "crit.md")
			data, err := os.ReadFile(src)
			if err != nil {
				t.Fatal(err)
			}
			os.WriteFile(filepath.Join(cachePath, "crit.md"), data, 0o644)
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
		{agent: "claude-code", file: "crit.md", dest: "/tmp/test/.claude/commands/crit.md", location: locationProject},
	}
	count := printStaleWarnings(stale)
	if count == 0 {
		t.Error("expected at least 1 warning")
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

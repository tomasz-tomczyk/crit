package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLoadConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".crit.config.json")
	os.WriteFile(configPath, []byte(`{
  "port": 3456,
  "no_open": true,
  "share_url": "https://example.com",
  "quiet": true,
  "ignore_patterns": ["*.lock", "vendor/"]
}`), 0644)

	cfg, presence, err := loadConfigFile(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 3456 {
		t.Errorf("port = %d, want 3456", cfg.Port)
	}
	if !cfg.NoOpen {
		t.Error("no_open should be true")
	}
	if cfg.ShareURL != "https://example.com" {
		t.Errorf("share_url = %q", cfg.ShareURL)
	}
	if !cfg.Quiet {
		t.Error("quiet should be true")
	}
	if len(cfg.IgnorePatterns) != 2 {
		t.Errorf("ignore_patterns = %v", cfg.IgnorePatterns)
	}
	if !presence.ShareURL {
		t.Error("presence.ShareURL should be true")
	}
	if !presence.IgnorePatterns {
		t.Error("presence.IgnorePatterns should be true")
	}
}

func TestLoadConfigFileMissing(t *testing.T) {
	cfg, presence, err := loadConfigFile("/nonexistent/.crit.config.json")
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg.Port != 0 {
		t.Errorf("expected zero config")
	}
	if presence.ShareURL || presence.IgnorePatterns {
		t.Error("missing file should have no presence flags")
	}
}

func TestLoadConfigFileInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".crit.config.json")
	os.WriteFile(configPath, []byte(`{invalid json`), 0644)

	_, _, err := loadConfigFile(configPath)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadConfigFilePresenceEmptyValues(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".crit.config.json")
	os.WriteFile(configPath, []byte(`{"share_url": "", "ignore_patterns": []}`), 0644)

	cfg, presence, err := loadConfigFile(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ShareURL != "" {
		t.Errorf("share_url = %q, want empty", cfg.ShareURL)
	}
	if len(cfg.IgnorePatterns) != 0 {
		t.Errorf("ignore_patterns = %v, want empty", cfg.IgnorePatterns)
	}
	if !presence.ShareURL {
		t.Error("presence.ShareURL should be true even for empty string")
	}
	if !presence.IgnorePatterns {
		t.Error("presence.IgnorePatterns should be true even for empty array")
	}
}

func TestMergeConfigs(t *testing.T) {
	global := Config{Port: 3000, ShareURL: "https://global.example.com"}
	global.IgnorePatterns = []string{"*.lock"}

	project := Config{Port: 8080}
	project.IgnorePatterns = []string{"*.pb.go"}

	merged := mergeConfigs(global, project, configPresence{})
	if merged.Port != 8080 {
		t.Errorf("port = %d, want 8080 (project override)", merged.Port)
	}
	if merged.ShareURL != "https://global.example.com" {
		t.Errorf("share_url lost")
	}
	if len(merged.IgnorePatterns) != 2 {
		t.Errorf("patterns = %v, want union", merged.IgnorePatterns)
	}
}

func TestBaseBranchConfig(t *testing.T) {
	t.Run("loadConfigFile parses base_branch", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, ".crit.config.json")
		os.WriteFile(configPath, []byte(`{"base_branch": "uat"}`), 0644)

		cfg, _, err := loadConfigFile(configPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.BaseBranch != "uat" {
			t.Errorf("base_branch = %q, want %q", cfg.BaseBranch, "uat")
		}
	})

	t.Run("mergeConfigs: project base_branch overrides global", func(t *testing.T) {
		global := Config{BaseBranch: "main"}
		project := Config{BaseBranch: "uat"}
		merged := mergeConfigs(global, project, configPresence{})
		if merged.BaseBranch != "uat" {
			t.Errorf("base_branch = %q, want %q", merged.BaseBranch, "uat")
		}
	})

	t.Run("mergeConfigs: global base_branch preserved when project unset", func(t *testing.T) {
		global := Config{BaseBranch: "develop"}
		project := Config{}
		merged := mergeConfigs(global, project, configPresence{})
		if merged.BaseBranch != "develop" {
			t.Errorf("base_branch = %q, want %q", merged.BaseBranch, "develop")
		}
	})

	t.Run("LoadConfig: project base_branch wins over global", func(t *testing.T) {
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		os.WriteFile(
			filepath.Join(homeDir, ".crit.config.json"),
			[]byte(`{"base_branch": "main"}`),
			0644,
		)
		projectDir := t.TempDir()
		os.WriteFile(
			filepath.Join(projectDir, ".crit.config.json"),
			[]byte(`{"base_branch": "uat"}`),
			0644,
		)

		cfg := LoadConfig(projectDir)
		if cfg.BaseBranch != "uat" {
			t.Errorf("base_branch = %q, want %q", cfg.BaseBranch, "uat")
		}
	})
}

func TestMergeConfigsZeroValues(t *testing.T) {
	global := Config{Port: 3000, NoOpen: true, Quiet: true}
	project := Config{} // all zero — should not override

	merged := mergeConfigs(global, project, configPresence{})
	if merged.Port != 3000 {
		t.Errorf("port should stay 3000")
	}
	if !merged.NoOpen {
		t.Error("no_open should stay true")
	}
	if !merged.Quiet {
		t.Error("quiet should stay true")
	}
}

func TestMergeConfigsBoolOverride(t *testing.T) {
	// Project explicitly sets no_open: false to override global no_open: true
	global := Config{NoOpen: true, Quiet: true}
	project := Config{NoOpen: false, Quiet: false}
	presence := configPresence{NoOpen: true, Quiet: true}

	merged := mergeConfigs(global, project, presence)
	if merged.NoOpen {
		t.Error("project no_open: false should override global no_open: true")
	}
	if merged.Quiet {
		t.Error("project quiet: false should override global quiet: true")
	}
}

func TestLoadConfig(t *testing.T) {
	// Set up global config
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	globalPath := filepath.Join(homeDir, ".crit.config.json")
	os.WriteFile(globalPath, []byte(`{"port": 3000, "share_url": "https://global.example.com", "ignore_patterns": ["*.lock"]}`), 0644)

	// Set up project dir with config
	projectDir := t.TempDir()
	os.WriteFile(filepath.Join(projectDir, ".crit.config.json"), []byte(`{"port": 8080, "ignore_patterns": ["*.pb.go"]}`), 0644)

	cfg := LoadConfig(projectDir)

	if cfg.Port != 8080 {
		t.Errorf("port = %d, want 8080 (project override)", cfg.Port)
	}
	if cfg.ShareURL != "https://global.example.com" {
		t.Errorf("share_url = %q, want global value", cfg.ShareURL)
	}
	if len(cfg.IgnorePatterns) != 2 {
		t.Errorf("ignore_patterns = %v, want 2 entries", cfg.IgnorePatterns)
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		// Suffix match (pattern starts with *)
		{"*.lock", "go.sum.lock", true},
		{"*.lock", "vendor/go.lock", true},
		{"*.lock", "lockfile", false},
		{"*.pb.go", "api/v1/service.pb.go", true},
		{"*.pb.go", "main.go", false},

		// Directory prefix (pattern ends with /)
		{"vendor/", "vendor/foo.go", true},
		{"vendor/", "vendor/bar/baz.go", true},
		{"vendor/", "myvendor/foo.go", false},
		{"generated/", "generated/types.go", true},
		{"node_modules/", "node_modules/lodash/index.js", true},
		{".crit/", ".crit/sessions/abc123.json", true},
		{".crit/", ".crit/config", true},
		{".crit/", ".crit.json", false},

		// Exact filename match (no / in pattern, no leading *)
		{"package-lock.json", "package-lock.json", true},
		{"package-lock.json", "sub/package-lock.json", true},
		{".env", ".env", true},
		{".env", "src/.env", true},

		// Filename glob (no /, has wildcard)
		{"*.min.js", "assets/app.min.js", true},
		{"*.min.js", "app.min.js", true},
		{"*.min.js", "app.js", false},

		// Path with / (match against full path)
		{"generated/*.pb.go", "generated/service.pb.go", true},
		{"generated/*.pb.go", "other/service.pb.go", false},
	}
	for _, tt := range tests {
		got := matchPattern(tt.pattern, tt.path)
		if got != tt.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
		}
	}
}

func TestFilterIgnored(t *testing.T) {
	files := []FileChange{
		{Path: "main.go", Status: "modified"},
		{Path: "go.sum.lock", Status: "modified"},
		{Path: "vendor/lib/foo.go", Status: "added"},
		{Path: "api/service.pb.go", Status: "modified"},
		{Path: "README.md", Status: "modified"},
	}
	patterns := []string{"*.lock", "vendor/", "*.pb.go"}
	filtered := filterIgnored(files, patterns)
	if len(filtered) != 2 {
		t.Fatalf("got %d files, want 2: %v", len(filtered), filtered)
	}
	if filtered[0].Path != "main.go" {
		t.Errorf("filtered[0] = %q", filtered[0].Path)
	}
	if filtered[1].Path != "README.md" {
		t.Errorf("filtered[1] = %q", filtered[1].Path)
	}
}

func TestFilterIgnoredEmpty(t *testing.T) {
	files := []FileChange{{Path: "main.go", Status: "modified"}}
	filtered := filterIgnored(files, nil)
	if len(filtered) != 1 {
		t.Errorf("expected no filtering with nil patterns")
	}
}

func TestFilterPathsIgnored(t *testing.T) {
	paths := []string{
		"src/main.go",
		"src/generated/types.go",
		"package-lock.json",
	}
	patterns := []string{"generated/", "package-lock.json"}
	filtered := filterPathsIgnored(paths, patterns)
	if len(filtered) != 1 || filtered[0] != "src/main.go" {
		t.Errorf("got %v", filtered)
	}
}

func TestConfigString(t *testing.T) {
	cfg := Config{Port: 3456, IgnorePatterns: []string{"*.lock"}}
	s := cfg.String()
	if s == "{}" {
		t.Error("expected non-empty JSON output")
	}
}

func TestLoadConfigRuntimeDefaults(t *testing.T) {
	// No config files at all — runtime defaults should apply
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	projectDir := t.TempDir()

	cfg := LoadConfig(projectDir)
	if cfg.ShareURL != "https://crit.md" {
		t.Errorf("ShareURL = %q, want runtime default https://crit.md", cfg.ShareURL)
	}
	wantPatterns := []string{".crit/"}
	if len(cfg.IgnorePatterns) != len(wantPatterns) || cfg.IgnorePatterns[0] != wantPatterns[0] {
		t.Errorf("IgnorePatterns = %v, want %v", cfg.IgnorePatterns, wantPatterns)
	}
}

func TestLoadConfigRuntimeDefaultsOverriddenByEmptyValues(t *testing.T) {
	// Config explicitly sets share_url to "" and ignore_patterns to [] — no defaults
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	projectDir := t.TempDir()
	os.WriteFile(filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"share_url": "", "ignore_patterns": []}`), 0644)

	cfg := LoadConfig(projectDir)
	if cfg.ShareURL != "" {
		t.Errorf("ShareURL = %q, want empty (explicitly overridden)", cfg.ShareURL)
	}
	if len(cfg.IgnorePatterns) != 0 {
		t.Errorf("IgnorePatterns = %v, want empty (explicitly overridden)", cfg.IgnorePatterns)
	}
}

func TestLoadConfigRuntimeDefaultsOverriddenByGlobal(t *testing.T) {
	// Global config sets share_url — no runtime default applied
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	os.WriteFile(filepath.Join(homeDir, ".crit.config.json"),
		[]byte(`{"share_url": "https://custom.example.com"}`), 0644)
	projectDir := t.TempDir()

	cfg := LoadConfig(projectDir)
	if cfg.ShareURL != "https://custom.example.com" {
		t.Errorf("ShareURL = %q, want custom global value", cfg.ShareURL)
	}
	// ignore_patterns not set in any config — default applies
	wantPatterns := []string{".crit/"}
	if len(cfg.IgnorePatterns) != len(wantPatterns) || cfg.IgnorePatterns[0] != wantPatterns[0] {
		t.Errorf("IgnorePatterns = %v, want %v", cfg.IgnorePatterns, wantPatterns)
	}
}

func TestLoadConfigAuthorFallsBackToGit(t *testing.T) {
	// Isolated HOME so no global config interferes
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Set up a git repo with user.name configured
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "config", "user.email", "test@test.com")
	runGit(t, repoDir, "config", "user.name", "Ada Lovelace")

	// LoadConfig calls git without -C, so we must be inside the repo
	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	cfg := LoadConfig(repoDir)
	if cfg.Author != "Ada Lovelace" {
		t.Errorf("Author = %q, want %q", cfg.Author, "Ada Lovelace")
	}
}

func TestLoadConfigAuthorFromConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	projectDir := t.TempDir()
	os.WriteFile(filepath.Join(projectDir, ".crit.config.json"), []byte(`{"author": "Grace Hopper"}`), 0644)

	cfg := LoadConfig(projectDir)
	if cfg.Author != "Grace Hopper" {
		t.Errorf("Author = %q, want %q", cfg.Author, "Grace Hopper")
	}
}

func TestLoadConfigSameFileNoDuplicatePatterns(t *testing.T) {
	// When CWD is the home dir, global and project config resolve to the same file.
	// Patterns should not be duplicated. (GitHub issue #92)
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	os.WriteFile(filepath.Join(homeDir, ".crit.config.json"),
		[]byte(`{"ignore_patterns": ["*.lock", "*.min.js", "*.min.css", ".crit.json"]}`), 0644)

	cfg := LoadConfig(homeDir)

	// Should have exactly 4 patterns, not 8
	if len(cfg.IgnorePatterns) != 4 {
		t.Errorf("IgnorePatterns = %v (len %d), want 4 unique entries (not duplicated)", cfg.IgnorePatterns, len(cfg.IgnorePatterns))
	}
}

func TestNewSessionFromGitWithIgnore(t *testing.T) {
	dir := initTestRepo(t)

	// Reset defaultBranch cache so it detects the temp repo's branch
	defaultBranchOnce = sync.Once{}

	// Create a feature branch with several files
	runGit(t, dir, "checkout", "-b", "feature")
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n")
	writeFile(t, filepath.Join(dir, "service.pb.go"), "package main\n// generated\n")
	writeFile(t, filepath.Join(dir, "vendor", "lib.go"), "package vendor\n")
	writeFile(t, filepath.Join(dir, "README.md"), "# Updated\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "add files")

	// cd into the repo
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	patterns := []string{"*.pb.go", "vendor/"}
	session, err := NewSessionFromGit(patterns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only have main.go and README.md (not service.pb.go or vendor/lib.go)
	paths := make(map[string]bool)
	for _, f := range session.Files {
		paths[f.Path] = true
	}
	if paths["service.pb.go"] {
		t.Error("service.pb.go should be ignored")
	}
	if paths["vendor/lib.go"] {
		t.Error("vendor/lib.go should be ignored")
	}
	if !paths["main.go"] {
		t.Error("main.go should be present")
	}
	if !paths["README.md"] {
		t.Error("README.md should be present")
	}

	// Verify patterns are stored on the session
	if len(session.IgnorePatterns) != 2 {
		t.Errorf("session.IgnorePatterns = %v, want 2 entries", session.IgnorePatterns)
	}
}

func TestNewSessionFromFilesWithIgnore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n")
	writeFile(t, filepath.Join(dir, "generated", "types.go"), "package gen\n")
	writeFile(t, filepath.Join(dir, "app.min.js"), "// minified\n")
	writeFile(t, filepath.Join(dir, "readme.txt"), "hello\n")

	patterns := []string{"generated/"}
	session, err := NewSessionFromFiles([]string{dir}, patterns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, f := range session.Files {
		rel, _ := filepath.Rel(dir, f.AbsPath)
		if strings.HasPrefix(rel, "generated/") || strings.HasPrefix(rel, "generated\\") {
			t.Errorf("file %s should have been ignored by generated/ pattern", f.Path)
		}
	}
}

func TestMergeConfigs_AgentCmd(t *testing.T) {
	global := Config{AgentCmd: "claude -p"}
	project := Config{}
	merged := mergeConfigs(global, project, configPresence{})
	if merged.AgentCmd != "claude -p" {
		t.Fatalf("expected global agent_cmd to carry through, got %q", merged.AgentCmd)
	}

	// project-level agent_cmd must be ignored — only global config may set it
	project2 := Config{AgentCmd: "opencode ask"}
	merged2 := mergeConfigs(global, project2, configPresence{})
	if merged2.AgentCmd != "claude -p" {
		t.Fatalf("expected project agent_cmd to be ignored, got %q", merged2.AgentCmd)
	}
}

func TestNoIntegrationCheckConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".crit.config.json")
	if err := os.WriteFile(configPath, []byte(`{"no_integration_check": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadConfigFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.NoIntegrationCheck {
		t.Error("expected NoIntegrationCheck to be true")
	}
}

func TestNoIntegrationCheckMerge(t *testing.T) {
	global := Config{NoIntegrationCheck: false}
	project := Config{NoIntegrationCheck: true}
	presence := configPresence{NoIntegrationCheck: true}
	merged := mergeConfigs(global, project, presence)
	if !merged.NoIntegrationCheck {
		t.Error("project NoIntegrationCheck=true should override global")
	}
}

func TestNoUpdateCheckConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".crit.config.json")
	if err := os.WriteFile(configPath, []byte(`{"no_update_check": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := loadConfigFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.NoUpdateCheck {
		t.Error("expected NoUpdateCheck to be true")
	}
}

func TestNoUpdateCheckMerge(t *testing.T) {
	global := Config{NoUpdateCheck: false}
	project := Config{NoUpdateCheck: true}
	presence := configPresence{NoUpdateCheck: true}
	merged := mergeConfigs(global, project, presence)
	if !merged.NoUpdateCheck {
		t.Error("project NoUpdateCheck=true should override global")
	}
}

func TestSaveGlobalConfig_RoundTrip(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Write a key
	err := saveGlobalConfig(func(m map[string]json.RawMessage) error {
		data, _ := json.Marshal("https://example.com")
		m["share_url"] = data
		return nil
	})
	if err != nil {
		t.Fatalf("saveGlobalConfig: %v", err)
	}

	// Read it back via loadConfigFile
	cfg, presence, err := loadConfigFile(filepath.Join(homeDir, ".crit.config.json"))
	if err != nil {
		t.Fatalf("loadConfigFile: %v", err)
	}
	if cfg.ShareURL != "https://example.com" {
		t.Errorf("ShareURL = %q, want https://example.com", cfg.ShareURL)
	}
	if !presence.ShareURL {
		t.Error("expected ShareURL presence to be true")
	}
}

func TestSaveGlobalConfig_PreservesExistingKeys(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Write initial config manually
	configPath := filepath.Join(homeDir, ".crit.config.json")
	os.WriteFile(configPath, []byte(`{"port": 3456, "quiet": true}`), 0644)

	// Update a different key
	err := saveGlobalConfig(func(m map[string]json.RawMessage) error {
		data, _ := json.Marshal("https://custom.example.com")
		m["share_url"] = data
		return nil
	})
	if err != nil {
		t.Fatalf("saveGlobalConfig: %v", err)
	}

	// Read back — both old and new keys should be present
	cfg, _, err := loadConfigFile(configPath)
	if err != nil {
		t.Fatalf("loadConfigFile: %v", err)
	}
	if cfg.Port != 3456 {
		t.Errorf("Port = %d, want 3456 (should be preserved)", cfg.Port)
	}
	if !cfg.Quiet {
		t.Error("Quiet should be preserved as true")
	}
	if cfg.ShareURL != "https://custom.example.com" {
		t.Errorf("ShareURL = %q, want https://custom.example.com", cfg.ShareURL)
	}
}

func TestMergeConfigs_AgentCmdProjectIgnored(t *testing.T) {
	// Even if project config has agent_cmd, it must be ignored for security
	global := Config{}
	project := Config{AgentCmd: "malicious-command"}
	merged := mergeConfigs(global, project, configPresence{})
	if merged.AgentCmd != "" {
		t.Errorf("project agent_cmd should be ignored, got %q", merged.AgentCmd)
	}
}

func TestMergeConfigs_IgnorePatternsUnion(t *testing.T) {
	global := Config{IgnorePatterns: []string{"*.lock", "vendor/"}}
	project := Config{IgnorePatterns: []string{"*.pb.go"}}
	merged := mergeConfigs(global, project, configPresence{})

	// mergeConfigs appends project patterns to global (straight union)
	if len(merged.IgnorePatterns) != 3 {
		t.Errorf("expected 3 patterns, got %d: %v", len(merged.IgnorePatterns), merged.IgnorePatterns)
	}
	seen := make(map[string]bool)
	for _, p := range merged.IgnorePatterns {
		seen[p] = true
	}
	if !seen["*.lock"] {
		t.Error("expected *.lock in merged patterns")
	}
	if !seen["vendor/"] {
		t.Error("expected vendor/ in merged patterns")
	}
	if !seen["*.pb.go"] {
		t.Error("expected *.pb.go in merged patterns")
	}
}

func TestLoadConfig_OutputField(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	projectDir := t.TempDir()
	os.WriteFile(filepath.Join(projectDir, ".crit.config.json"),
		[]byte(`{"output": "/tmp/output"}`), 0644)

	cfg := LoadConfig(projectDir)
	if cfg.Output != "/tmp/output" {
		t.Errorf("Output = %q, want /tmp/output", cfg.Output)
	}
}

package main

import (
	"os"
	"os/exec"
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

	cfg, err := loadConfigFile(configPath)
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
}

func TestLoadConfigFileMissing(t *testing.T) {
	cfg, err := loadConfigFile("/nonexistent/.crit.config.json")
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg.Port != 0 {
		t.Errorf("expected zero config")
	}
}

func TestLoadConfigFileInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".crit.config.json")
	os.WriteFile(configPath, []byte(`{invalid json`), 0644)

	_, err := loadConfigFile(configPath)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMergeConfigs(t *testing.T) {
	global := Config{Port: 3000, ShareURL: "https://global.example.com"}
	global.IgnorePatterns = []string{"*.lock"}

	project := Config{Port: 8080}
	project.IgnorePatterns = []string{"*.pb.go"}

	merged := mergeConfigs(global, project)
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

func TestMergeConfigsZeroValues(t *testing.T) {
	global := Config{Port: 3000, NoOpen: true, Quiet: true}
	project := Config{} // all zero — should not override

	merged := mergeConfigs(global, project)
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

// Integration test helpers (reuse pattern from git_test.go)
func initTestRepoForConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitForConfig(t, dir, "init")
	runGitForConfig(t, dir, "config", "user.email", "test@test.com")
	runGitForConfig(t, dir, "config", "user.name", "Test")
	writeFileForConfig(t, filepath.Join(dir, "README.md"), "# Test")
	runGitForConfig(t, dir, "add", "README.md")
	runGitForConfig(t, dir, "commit", "-m", "initial")
	runGitForConfig(t, dir, "branch", "-M", "main")
	return dir
}

func runGitForConfig(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFileForConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestNewSessionFromGitWithIgnore(t *testing.T) {
	dir := initTestRepoForConfig(t)

	// Reset defaultBranch cache so it detects the temp repo's branch
	defaultBranchOnce = sync.Once{}

	// Create a feature branch with several files
	runGitForConfig(t, dir, "checkout", "-b", "feature")
	writeFileForConfig(t, filepath.Join(dir, "main.go"), "package main\n")
	writeFileForConfig(t, filepath.Join(dir, "service.pb.go"), "package main\n// generated\n")
	writeFileForConfig(t, filepath.Join(dir, "vendor", "lib.go"), "package vendor\n")
	writeFileForConfig(t, filepath.Join(dir, "README.md"), "# Updated\n")
	runGitForConfig(t, dir, "add", ".")
	runGitForConfig(t, dir, "commit", "-m", "add files")

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
	writeFileForConfig(t, filepath.Join(dir, "main.go"), "package main\n")
	writeFileForConfig(t, filepath.Join(dir, "generated", "types.go"), "package gen\n")
	writeFileForConfig(t, filepath.Join(dir, "app.min.js"), "// minified\n")
	writeFileForConfig(t, filepath.Join(dir, "readme.txt"), "hello\n")

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

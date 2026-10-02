package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOpencodeVersion(t *testing.T) {
	cases := []struct {
		output  string
		major   int
		version string
	}{
		{"opencode v2.0.22", 2, "2.0.22"},
		{"1.18.34", 1, "1.18.34"},
		{"OpenCode dev build", 1, ""},
		{"opencode v3.0.0", 1, ""},
	}
	for _, tc := range cases {
		t.Run(tc.output, func(t *testing.T) {
			major, version := parseOpencodeVersion(tc.output)
			if major != tc.major || version != tc.version {
				t.Fatalf("parseOpencodeVersion(%q) = (%d, %q), want (%d, %q)", tc.output, major, version, tc.major, tc.version)
			}
		})
	}
}

func TestOpencodeVersionInfoFallsBackWhenBinaryIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	major, version := opencodeVersionInfo()
	if major != 1 || version != "" {
		t.Fatalf("opencodeVersionInfo() = (%d, %q), want (1, empty)", major, version)
	}
}

func TestOpencodeIntegrationFilesSelectsMajorVersion(t *testing.T) {
	for _, major := range []int{1, 2, 3} {
		files := opencodeIntegrationFiles(major)
		for _, f := range files {
			if f.opencodeVersion != 0 && f.opencodeVersion != major {
				t.Errorf("OpenCode v%d selected version-specific source %s for v%d", major, f.source, f.opencodeVersion)
			}
		}
		if len(files) == 0 {
			t.Fatalf("OpenCode v%d selected no integration files", major)
		}
	}
	if got := integrationFilesForAgent("aider"); len(got) != len(integrationMap["aider"]) {
		t.Fatalf("non-OpenCode routing returned %d files, want %d", len(got), len(integrationMap["aider"]))
	}
}

func TestOpencodePluginPackagePath(t *testing.T) {
	home := t.TempDir()
	if got, want := opencodePluginPackagePath(false, home), filepath.Join(".opencode", "package.json"); got != want {
		t.Errorf("project package path = %q, want %q", got, want)
	}
	if got, want := opencodePluginPackagePath(true, home), filepath.Join(home, ".config", "opencode", "package.json"); got != want {
		t.Errorf("global package path = %q, want %q", got, want)
	}
}

func TestOpencodePluginDependencyInstalled(t *testing.T) {
	configDir := t.TempDir()
	if opencodePluginDependencyInstalled(configDir, "2.0.22") {
		t.Fatal("missing SDK package reported installed")
	}
	packageDir := filepath.Join(configDir, "node_modules", "@opencode", "plugin")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(packageDir, "package.json")
	if err := os.WriteFile(packagePath, []byte(`{"version":"2.0.21"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if opencodePluginDependencyInstalled(configDir, "2.0.22") {
		t.Fatal("mismatched SDK version reported installed")
	}
	if err := os.WriteFile(packagePath, []byte(`{"version":"2.0.22"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !opencodePluginDependencyInstalled(configDir, "2.0.22") {
		t.Fatal("matching SDK version not detected")
	}
}

func TestInstallOpencodePluginDependency(t *testing.T) {
	t.Run("missing version errors", func(t *testing.T) {
		if err := installOpencodePluginDependency(filepath.Join(t.TempDir(), "package.json"), "", false); err == nil {
			t.Fatal("expected missing-version error")
		}
	})

	t.Run("invalid JSON errors", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "package.json")
		if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := installOpencodePluginDependency(path, "2.0.22", false); err == nil {
			t.Fatal("expected invalid-JSON error")
		}
	})

	t.Run("non-object dependencies errors", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "package.json")
		if err := os.WriteFile(path, []byte(`{"dependencies":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := installOpencodePluginDependency(path, "2.0.22", false); err == nil {
			t.Fatal("expected invalid-dependencies error")
		}
	})

	t.Run("matching declared and installed SDK is idempotent", func(t *testing.T) {
		dir := t.TempDir()
		writeOpencodeSDKFixture(t, dir, "2.0.22", `{"version":"2.0.22"}`)
		path := filepath.Join(dir, "package.json")
		if err := installOpencodePluginDependency(path, "2.0.22", false); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"@opencode/plugin":"2.0.22"`) {
			t.Fatalf("SDK dependency missing from manifest: %s", data)
		}
	})

	t.Run("mismatch without force preserves manifest", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "package.json")
		initial := `{"dependencies":{"@opencode/plugin":"2.0.21","other":"1"}}`
		if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := installOpencodePluginDependency(path, "2.0.22", false); err == nil {
			t.Fatal("expected SDK version mismatch to require --force")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != initial {
			t.Fatalf("manifest changed without force: %s", data)
		}
	})

	t.Run("missing SDK invokes package manager and reports failure", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "package.json")
		t.Setenv("PATH", t.TempDir())
		err := installOpencodePluginDependency(path, "2.0.22", false)
		if err == nil || !strings.Contains(err.Error(), "require bun or npm") {
			t.Fatalf("expected missing-package-manager error, got %v", err)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var manifest struct {
			Dependencies map[string]string `json:"dependencies"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		if got := manifest.Dependencies["@opencode/plugin"]; got != "2.0.22" {
			t.Fatalf("installed dependency = %q, want 2.0.22", got)
		}
	})
}

func writeOpencodeSDKFixture(t *testing.T, dir, declared, installed string) {
	t.Helper()
	packagePath := filepath.Join(dir, "package.json")
	manifest := `{"dependencies":{"@opencode/plugin":"` + declared + `"}}`
	if err := os.WriteFile(packagePath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	packageDir := filepath.Join(dir, "node_modules", "@opencode", "plugin")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "package.json"), []byte(installed), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckOpencodePluginDependencyForVersion(t *testing.T) {
	projectDir, homeDir := t.TempDir(), t.TempDir()
	if got := checkOpencodePluginDependencyForVersion(projectDir, homeDir, 1, "1.18.34"); len(got) != 0 {
		t.Fatalf("v1 check returned stale dependencies: %+v", got)
	}
	if got := checkOpencodePluginDependencyForVersion(projectDir, homeDir, 2, ""); len(got) != 0 {
		t.Fatalf("unknown v2 version returned stale dependencies: %+v", got)
	}

	pluginPath := filepath.Join(projectDir, ".opencode", "plugins", "crit", "index.ts")
	if err := os.MkdirAll(filepath.Dir(pluginPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pluginPath, []byte("export {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := checkOpencodePluginDependencyForVersion(projectDir, homeDir, 2, "2.0.22"); len(got) != 1 || got[0].location != locationProject {
		t.Fatalf("missing SDK should report one project stale file, got %+v", got)
	}

	writeOpencodeSDKFixture(t, filepath.Join(projectDir, ".opencode"), "2.0.22", `{"version":"2.0.22"}`)
	if got := checkOpencodePluginDependencyForVersion(projectDir, homeDir, 2, "2.0.22"); len(got) != 0 {
		t.Fatalf("matching project SDK reported stale: %+v", got)
	}
}

func TestCleanupOpencodePluginVersionFiles(t *testing.T) {
	home := t.TempDir()
	for _, major := range []int{1, 2} {
		for _, f := range integrationMap["opencode"] {
			if f.opencodeVersion != major {
				continue
			}
			path := destFor(f, true, home, "opencode")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("plugin"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := cleanupOpencodePluginVersionFiles(true, home, 2, true); err != nil {
		t.Fatal(err)
	}
	for _, f := range integrationMap["opencode"] {
		if f.opencodeVersion == 0 {
			continue
		}
		path := destFor(f, true, home, "opencode")
		_, err := os.Stat(path)
		if f.opencodeVersion == 1 && !os.IsNotExist(err) {
			t.Errorf("inactive v1 file still exists: %s", path)
		}
		if f.opencodeVersion == 2 && err != nil {
			t.Errorf("active v2 file removed: %s: %v", path, err)
		}
	}
}

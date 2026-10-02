package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

func TestOpencodeVersionInfoCachesUntilBinaryChanges(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture uses POSIX executable scripts")
	}
	bin := t.TempDir()
	runs := filepath.Join(t.TempDir(), "runs")
	stub := filepath.Join(bin, "opencode")
	writeStub := func(version string) {
		t.Helper()
		body := "#!/bin/sh\necho x >> \"" + runs + "\"\necho \"" + version + "\"\n"
		if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	countRuns := func() int {
		t.Helper()
		data, err := os.ReadFile(runs)
		if errors.Is(err, os.ErrNotExist) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		return strings.Count(string(data), "x")
	}
	t.Setenv("PATH", bin)

	writeStub("2.0.22")
	for range 3 {
		if major, version := opencodeVersionInfo(); major != 2 || version != "2.0.22" {
			t.Fatalf("opencodeVersionInfo() = (%d, %q), want (2, 2.0.22)", major, version)
		}
	}
	if got := countRuns(); got != 1 {
		t.Fatalf("opencode --version ran %d times, want 1", got)
	}

	// Simulate an upgrade: new content (different size) and a new mtime.
	writeStub("2.10.100")
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(stub, later, later); err != nil {
		t.Fatal(err)
	}
	if major, version := opencodeVersionInfo(); major != 2 || version != "2.10.100" {
		t.Fatalf("after upgrade opencodeVersionInfo() = (%d, %q), want (2, 2.10.100)", major, version)
	}
	if got := countRuns(); got != 2 {
		t.Fatalf("opencode --version ran %d times after upgrade, want 2", got)
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

func TestRemoveStaleOpencodePluginFiles(t *testing.T) {
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
	stale, err := findStaleOpencodePluginFiles(true, home, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := removeStaleOpencodePluginFiles(stale); err != nil {
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

func TestFindStaleOpencodePluginFilesPreservesModifiedFilesAtomically(t *testing.T) {
	home := t.TempDir()
	var paths []string
	for _, f := range integrationMap["opencode"] {
		if f.opencodeVersion != 1 {
			continue
		}
		path := destFor(f, true, home, "opencode")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		contents, err := integrationsFS.ReadFile(f.source)
		if err != nil {
			t.Fatal(err)
		}
		if len(paths) == 1 {
			contents = []byte("user customization")
		}
		if err := os.WriteFile(path, contents, 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	if len(paths) != 2 {
		t.Fatalf("expected two V1 plugin files, got %d", len(paths))
	}
	if _, err := findStaleOpencodePluginFiles(true, home, 2, false); err == nil {
		t.Fatal("expected modified plugin file to block migration")
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("preflight removed plugin file before reporting error: %s: %v", path, err)
		}
	}
}

// setupOpencodeV2Install prepares a fake OpenCode v2 environment: a PATH with
// stub opencode and bun binaries, a HOME, and a project working directory.
// bun records each run in the returned marker file. seed returns the content
// to write for each V1 plugin file (keyed by integration source).
func setupOpencodeV2Install(t *testing.T, seed func(source string) []byte) (home, bunMarker string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fixture uses POSIX executable scripts")
	}
	root := t.TempDir()
	project := filepath.Join(root, "project")
	bin := filepath.Join(root, "bin")
	home = filepath.Join(root, "home")
	for _, dir := range []string{project, bin, home} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bunMarker = filepath.Join(root, "bun-ran")
	writeExecutable := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeExecutable("opencode", `echo "2.0.22"`)
	writeExecutable("bun", `: > "`+bunMarker+`"`)
	t.Setenv("PATH", bin)
	t.Setenv("HOME", home)
	t.Chdir(project)

	for _, f := range integrationMap["opencode"] {
		if f.opencodeVersion != 1 {
			continue
		}
		path := destFor(f, false, home, "opencode")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, seed(f.source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home, bunMarker
}

func embeddedOpencodeSeed(t *testing.T) func(string) []byte {
	return func(source string) []byte {
		t.Helper()
		contents, err := integrationsFS.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		return contents
	}
}

func assertOpencodeV2Migrated(t *testing.T, home string) {
	t.Helper()
	for _, f := range integrationMap["opencode"] {
		path := destFor(f, false, home, "opencode")
		_, err := os.Stat(path)
		if f.opencodeVersion == 1 && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("old V1 plugin file remains or is inaccessible: %s, err=%v", path, err)
		}
		if f.opencodeVersion == 2 && err != nil {
			t.Errorf("V2 plugin file missing: %s: %v", path, err)
		}
	}
	manifest, err := os.ReadFile(opencodePluginPackagePath(false, home))
	if err != nil || !strings.Contains(string(manifest), `"@opencode/plugin": "2.0.22"`) {
		t.Fatalf("V2 plugin SDK manifest not installed: %s, err=%v", manifest, err)
	}
}

func TestInstallOpencodeV2MigratesV1Files(t *testing.T) {
	home, _ := setupOpencodeV2Install(t, embeddedOpencodeSeed(t))
	config := opencodeConfigPath(false, home)
	if err := os.WriteFile(config, []byte(`{"plugin":["./.opencode/plugins/crit.ts"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installIntegration("opencode", false); err != nil {
		t.Fatal(err)
	}
	assertOpencodeV2Migrated(t, home)
	configData, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(configData), "crit.ts") {
		t.Fatalf("obsolete V1 plugin registration remains: %s", configData)
	}
}

// V1 files written by an older release differ from the current embedded copy
// but are still unmodified Crit files, so migration must not require --force.
func TestInstallOpencodeV2MigratesV1FilesFromOlderRelease(t *testing.T) {
	fixtures := map[string]string{
		"integrations/opencode/plugin/crit.ts":                 "testdata/opencode-v0.21.0/crit.ts",
		"integrations/opencode/plugin/lib/crit-wait-notify.js": "testdata/opencode-v0.21.0/lib/crit-wait-notify.js",
	}
	// Read fixtures before setup changes the working directory.
	contents := map[string][]byte{}
	for source, fixture := range fixtures {
		data, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatal(err)
		}
		embedded, err := integrationsFS.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) == string(embedded) {
			t.Fatalf("fixture %s matches the current embedded file; the test would not cover older releases", fixture)
		}
		contents[source] = data
	}
	home, _ := setupOpencodeV2Install(t, func(source string) []byte {
		data, ok := contents[source]
		if !ok {
			t.Fatalf("no v0.21.0 fixture for %s", source)
		}
		return data
	})
	if err := installIntegration("opencode", false); err != nil {
		t.Fatal(err)
	}
	assertOpencodeV2Migrated(t, home)
}

func TestInstallOpencodeV2ModifiedV1FileBlocksDependencyInstall(t *testing.T) {
	seed := embeddedOpencodeSeed(t)
	home, bunMarker := setupOpencodeV2Install(t, func(source string) []byte {
		if source == "integrations/opencode/plugin/crit.ts" {
			return []byte("user customization")
		}
		return seed(source)
	})
	err := installIntegration("opencode", false)
	if err == nil || !strings.Contains(err.Error(), "modified OpenCode v1 Crit plugin") {
		t.Fatalf("expected modified-plugin error, got %v", err)
	}
	if _, err := os.Stat(opencodePluginPackagePath(false, home)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("package.json written despite refusal: err=%v", err)
	}
	if _, err := os.Stat(bunMarker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("package manager ran despite refusal: err=%v", err)
	}
	for _, f := range integrationMap["opencode"] {
		if f.opencodeVersion != 1 {
			continue
		}
		if _, err := os.Stat(destFor(f, false, home, "opencode")); err != nil {
			t.Errorf("V1 file removed despite refusal: %v", err)
		}
	}
}

func TestRemoveOpencodePluginEntry(t *testing.T) {
	t.Run("removes only the Crit entry and preserves other plugins", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "opencode.json")
		initial := `{"plugin":["./.opencode/plugins/crit.ts","other-plugin"]}`
		if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
			t.Fatal(err)
		}
		removeOpencodePluginEntry(path, "./.opencode/plugins/crit.ts")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var config struct {
			Plugin []string `json:"plugin"`
		}
		if err := json.Unmarshal(data, &config); err != nil {
			t.Fatal(err)
		}
		if len(config.Plugin) != 1 || config.Plugin[0] != "other-plugin" {
			t.Fatalf("unexpected remaining plugins: %+v", config.Plugin)
		}
	})

	t.Run("leaves JSONC for manual edit", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "opencode.jsonc")
		initial := []byte("{ // keep comment\n  \"plugin\": [\"./.opencode/plugins/crit.ts\"]\n}\n")
		if err := os.WriteFile(path, initial, 0o644); err != nil {
			t.Fatal(err)
		}
		removeOpencodePluginEntry(path, "./.opencode/plugins/crit.ts")
		data, err := os.ReadFile(path)
		if err != nil || string(data) != string(initial) {
			t.Fatalf("JSONC changed: %s, err=%v", data, err)
		}
	})

	t.Run("leaves configs with unrelated keys untouched", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "opencode.json")
		initial := `{"theme":"dark","plugin":["./.opencode/plugins/crit.ts"]}`
		if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
			t.Fatal(err)
		}
		removeOpencodePluginEntry(path, "./.opencode/plugins/crit.ts")
		data, err := os.ReadFile(path)
		if err != nil || string(data) != initial {
			t.Fatalf("config changed: %s, err=%v", data, err)
		}
	})

	t.Run("ignores missing config and absent entry", func(t *testing.T) {
		dir := t.TempDir()
		removeOpencodePluginEntry(filepath.Join(dir, "missing.json"), "./.opencode/plugins/crit.ts")
		path := filepath.Join(dir, "opencode.json")
		initial := `{"plugin":["other-plugin"]}`
		if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
			t.Fatal(err)
		}
		removeOpencodePluginEntry(path, "./.opencode/plugins/crit.ts")
		data, err := os.ReadFile(path)
		if err != nil || string(data) != initial {
			t.Fatalf("config changed: %s, err=%v", data, err)
		}
	})

	t.Run("leaves invalid and unsupported configs untouched", func(t *testing.T) {
		for name, initial := range map[string]string{
			"invalid-json":  `{broken`,
			"plugin-string": `{"plugin":"./.opencode/plugins/crit.ts"}`,
		} {
			t.Run(name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "opencode.json")
				if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
					t.Fatal(err)
				}
				removeOpencodePluginEntry(path, "./.opencode/plugins/crit.ts")
				data, err := os.ReadFile(path)
				if err != nil || string(data) != initial {
					t.Fatalf("config changed: %s, err=%v", data, err)
				}
			})
		}
	})
}

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallOpencodePluginEntry(t *testing.T) {
	projectEntry := opencodePluginEntry(false)
	cases := []struct {
		name         string
		initial      string // existing file content; "" means file absent
		expectPlugin []interface{}
		expectErr    bool
		expectSkip   bool // file should be left untouched (e.g. JSONC with comments)
	}{
		{
			name:         "missing file",
			initial:      "",
			expectPlugin: []interface{}{projectEntry},
		},
		{
			name:         "empty object",
			initial:      `{}`,
			expectPlugin: []interface{}{projectEntry},
		},
		{
			name:         "existing plugins, no crit entry",
			initial:      `{"plugin":["some-other-plugin"]}`,
			expectPlugin: []interface{}{"some-other-plugin", projectEntry},
		},
		{
			name:         "already registered as string (idempotent)",
			initial:      `{"plugin":["./.opencode/plugins/crit.ts"]}`,
			expectPlugin: []interface{}{projectEntry},
		},
		{
			name:         "already registered as tuple",
			initial:      `{"plugin":[["./.opencode/plugins/crit.ts",{}]]}`,
			expectPlugin: []interface{}{[]interface{}{projectEntry, map[string]interface{}{}}},
		},
		{
			name:      "malformed json errors",
			initial:   `{"plugin": [`,
			expectErr: true,
		},
		{
			name:       "jsonc with comments left alone",
			initial:    "// my opencode config\n{\"plugin\": []}",
			expectSkip: true,
		},
		{
			name:       "config with unrelated keys is left untouched",
			initial:    `{"provider":{"anthropic":{"models":{"claude-3":{}}}},"model":"claude-3"}`,
			expectSkip: true,
		},
		{
			name:       "config with unrelated keys plus existing plugin is left untouched",
			initial:    `{"theme":"dark","plugin":["other-plugin"]}`,
			expectSkip: true,
		},
		{
			name:       "plugin key is a string (not an array) — bail",
			initial:    `{"plugin":"some-plugin.ts"}`,
			expectSkip: true,
		},
		{
			name:       "plugin key is an object — bail",
			initial:    `{"plugin":{"name":"foo"}}`,
			expectSkip: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "opencode.jsonc")
			if tc.initial != "" {
				if err := os.WriteFile(path, []byte(tc.initial), 0o644); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}

			err := installOpencodePluginEntry(path, projectEntry, false)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("install: %v", err)
			}

			if tc.expectSkip {
				got, _ := os.ReadFile(path)
				if string(got) != tc.initial {
					t.Fatalf("file modified despite skip; got %q want %q", got, tc.initial)
				}
				return
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read result: %v", err)
			}
			var root map[string]interface{}
			if err := json.Unmarshal(data, &root); err != nil {
				t.Fatalf("result not valid JSON: %v\n%s", err, data)
			}
			plugins, _ := root["plugin"].([]interface{})
			if len(plugins) != len(tc.expectPlugin) {
				t.Fatalf("plugin array len = %d want %d (got %v)", len(plugins), len(tc.expectPlugin), plugins)
			}
			for i := range plugins {
				gotJSON, _ := json.Marshal(plugins[i])
				wantJSON, _ := json.Marshal(tc.expectPlugin[i])
				if string(gotJSON) != string(wantJSON) {
					t.Errorf("plugin[%d] = %s want %s", i, gotJSON, wantJSON)
				}
			}
		})
	}
}

func TestLooksLikeJSONC(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"plain JSON", `{"plugin":[]}`, false},
		{"line comment", "// hi\n{\"plugin\":[]}", true},
		{"block comment", "/* hi */\n{\"plugin\":[]}", true},
		{"slashes inside string are not comments", `{"plugin":[],"note":"see https://x.com"}`, false},
		{"escaped quote inside string", `{"a":"he said \"//\""}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeJSONC([]byte(tc.in)); got != tc.want {
				t.Errorf("looksLikeJSONC(%q) = %v want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestOpencodePluginEntryPath(t *testing.T) {
	if got := opencodePluginEntry(false); got != "./.opencode/plugins/crit.ts" {
		t.Errorf("project entry = %q", got)
	}
	if got := opencodePluginEntry(true); got != "./plugins/crit.ts" {
		t.Errorf("global entry = %q", got)
	}
}

func TestOpencodeConfigPathPrefersJSON(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(jsonPath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := opencodeConfigPath(true, home)
	if got != jsonPath {
		t.Fatalf("got %s want %s (existing .json should win)", got, jsonPath)
	}
}

func TestOpencodeConfigPathDefaultsToJSONC(t *testing.T) {
	home := t.TempDir()
	got := opencodeConfigPath(true, home)
	if !strings.HasSuffix(got, "opencode.jsonc") {
		t.Fatalf("expected .jsonc default, got %s", got)
	}
}

func TestInstallOpencodeIncludesPluginFile(t *testing.T) {
	// Sanity: the integration map advertises the new plugin source, and the
	// embed FS resolves it.
	files, ok := integrationMap["opencode"]
	if !ok {
		t.Fatal("opencode integration missing from map")
	}
	var foundSource string
	for _, f := range files {
		if f.source == "integrations/opencode/plugin/crit.ts" {
			foundSource = f.source
			if f.dest != ".opencode/plugins/crit.ts" {
				t.Errorf("project dest = %q want .opencode/plugins/crit.ts", f.dest)
			}
			if f.globalDest != ".config/opencode/plugins/crit.ts" {
				t.Errorf("global dest = %q want .config/opencode/plugins/crit.ts", f.globalDest)
			}
		}
	}
	if foundSource == "" {
		t.Fatal("plugin file not registered in opencode integration map")
	}
	data, err := integrationsFS.ReadFile(foundSource)
	if err != nil {
		t.Fatalf("read embedded %s: %v", foundSource, err)
	}
	if !strings.Contains(string(data), "experimental.chat.system.transform") {
		t.Error("embedded plugin does not reference the system-prompt hook")
	}
}

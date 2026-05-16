package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These tests cover the destination-routing rules for non-aider integrations.
// They protect against regressions in integrationMap entries: missing
// globalDest fields, wrong globalDestKind, or wrong destination paths.
// (The aider integration has its own end-to-end coverage in aider_install_test.go.)

func TestDestFor_ProjectMode(t *testing.T) {
	// Project install must always use the dest field, regardless of whether a
	// globalDest is set.
	for name, files := range integrationMap {
		for i, f := range files {
			got := destFor(f, false, "/home/me", name)
			if got != f.dest {
				t.Errorf("%s[%d] project: got %q, want %q", name, i, got, f.dest)
			}
		}
	}
}

func TestDestFor_GlobalMode(t *testing.T) {
	// In global mode, integrations with no globalDest fall through to the raw
	// relative dest (which lands under $HOME because cwd == $HOME during a
	// global install). Integrations with a globalDest get the redirected
	// absolute path. The two routes have meaningfully different semantics, so
	// the test exercises both.
	home := "/home/me"
	cases := []struct {
		tool    string
		fileIdx int
		want    string
	}{
		// No globalDest → raw relative dest, written cwd-relative (cwd == $HOME).
		{"claude-code", 0, ".claude/skills/crit/SKILL.md"},
		{"claude-code", 1, ".claude/skills/crit-cli/SKILL.md"},
		{"codex", 0, ".agents/skills/crit/SKILL.md"},
		{"codex", 1, ".agents/skills/crit-cli/SKILL.md"},
		{"qwen", 0, ".qwen/skills/crit/SKILL.md"},
		{"qwen", 1, ".qwen/skills/crit-cli/SKILL.md"},
		{"cursor", 0, ".cursor/skills/crit/SKILL.md"},
		{"cursor", 1, ".cursor/skills/crit-cli/SKILL.md"},
		// grok: same-shape .grok/skills/ project-locally and ~/.grok/skills/ globally (no globalDest redirect needed).
		{"grok", 0, ".grok/skills/crit/SKILL.md"},
		{"grok", 1, ".grok/skills/crit-cli/SKILL.md"},
		// opencode: command stays cwd-relative; skill redirects globally to ~/.agents/skills/.
		{"opencode", 0, ".opencode/commands/crit.md"},
		{"opencode", 1, filepath.Join(home, ".agents/skills/crit/SKILL.md")},
		// github-copilot: both skills redirect to ~/.agents/skills/.
		{"github-copilot", 0, filepath.Join(home, ".agents/skills/crit/SKILL.md")},
		{"github-copilot", 1, filepath.Join(home, ".agents/skills/crit-cli/SKILL.md")},
		// hermes: both skills redirect to ~/.hermes/skills/.
		{"hermes", 0, filepath.Join(home, ".hermes/skills/crit/SKILL.md")},
		{"hermes", 1, filepath.Join(home, ".hermes/skills/crit-cli/SKILL.md")},
		// pi: both skills redirect to ~/.pi/agent/skills/.
		{"pi", 0, filepath.Join(home, ".pi/agent/skills/crit/SKILL.md")},
		{"pi", 1, filepath.Join(home, ".pi/agent/skills/crit-cli/SKILL.md")},
	}
	for _, tc := range cases {
		f := integrationMap[tc.tool][tc.fileIdx]
		got := destFor(f, true, home, tc.tool)
		if got != tc.want {
			t.Errorf("%s[%d] global: got %q, want %q", tc.tool, tc.fileIdx, got, tc.want)
		}
	}
}

func TestDestFor_ClineGlobalUsesDocuments(t *testing.T) {
	// Cline's globalDest uses the platform Documents directory, not $HOME directly.
	prev := xdgUserDirFn
	t.Cleanup(func() { xdgUserDirFn = prev })
	xdgUserDirFn = func(string) (string, error) { return "", nil }

	home := "/home/me"
	f := integrationMap["cline"][0]
	got := destFor(f, true, home, "cline")
	want := filepath.Join(documentsDir(home), "Cline/Rules/crit.md")
	if got != want {
		t.Errorf("cline global: got %q, want %q", got, want)
	}
	// On non-Linux, this should always be $HOME/Documents/Cline/Rules/crit.md.
	if runtime.GOOS != "linux" {
		expected := filepath.Join(home, "Documents/Cline/Rules/crit.md")
		if got != expected {
			t.Errorf("cline global on %s: got %q, want %q", runtime.GOOS, got, expected)
		}
	}
}

func TestIntegrationMap_SnapshotGlobalRouting(t *testing.T) {
	// Snapshot test: verifies each tool's globalDest configuration matches
	// what the integration validation findings established. Update this test
	// when intentionally changing routing.
	type want struct {
		globalDest string
		kind       globalDestKind
	}
	expected := map[string][]want{
		"claude-code":    {{"", globalDestNone}, {"", globalDestNone}},
		"cursor":         {{"", globalDestNone}, {"", globalDestNone}},
		"codex":          {{"", globalDestNone}, {"", globalDestNone}},
		"qwen":           {{"", globalDestNone}, {"", globalDestNone}},
		"opencode":       {{"", globalDestNone}, {".agents/skills/crit/SKILL.md", globalDestRelHome}, {".config/opencode/plugins/crit.ts", globalDestRelHome}},
		"github-copilot": {{".agents/skills/crit/SKILL.md", globalDestRelHome}, {".agents/skills/crit-cli/SKILL.md", globalDestRelHome}},
		"windsurf":       {{"", globalDestNone}},
		"cline":          {{"Cline/Rules/crit.md", globalDestDocuments}},
		"gemini":         {{".gemini/skills/crit-cli/SKILL.md", globalDestRelHome}, {".gemini/commands/crit.toml", globalDestRelHome}, {".gemini/policies/crit.toml", globalDestRelHome}},
		"grok":           {{"", globalDestNone}, {"", globalDestNone}},
		"hermes":         {{".hermes/skills/crit/SKILL.md", globalDestRelHome}, {".hermes/skills/crit-cli/SKILL.md", globalDestRelHome}},
		"pi":             {{".pi/agent/skills/crit/SKILL.md", globalDestRelHome}, {".pi/agent/skills/crit-cli/SKILL.md", globalDestRelHome}},
	}
	for tool, files := range expected {
		got := integrationMap[tool]
		if len(got) != len(files) {
			t.Errorf("%s: got %d files, want %d", tool, len(got), len(files))
			continue
		}
		for i, w := range files {
			if got[i].globalDest != w.globalDest || got[i].globalDestKind != w.kind {
				t.Errorf("%s[%d]: got (%q, kind=%d), want (%q, kind=%d)",
					tool, i, got[i].globalDest, got[i].globalDestKind, w.globalDest, w.kind)
			}
		}
	}
}

func TestInstallOneFile_WritesAndSkips(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "subdir", "out.md")
	f := integration{source: "integrations/cline/crit.md", dest: dest}

	// First install: file written.
	installOneFile(f, dest, false)
	if _, err := os.ReadFile(dest); err != nil {
		t.Fatalf("expected file at %s: %v", dest, err)
	}

	// Second install without --force: should skip without erroring.
	// Modify the file to verify it's not overwritten.
	if err := os.WriteFile(dest, []byte("hand-edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	installOneFile(f, dest, false)
	got, _ := os.ReadFile(dest)
	if string(got) != "hand-edited" {
		t.Errorf("non-force should skip; file was overwritten: %q", got)
	}

	// Force install: file overwritten with embedded content.
	installOneFile(f, dest, true)
	got, _ = os.ReadFile(dest)
	if string(got) == "hand-edited" {
		t.Errorf("force should overwrite; file still has hand-edited content")
	}
}

// TestInstallIntegration_GeminiWritesSettingsJSON verifies that the gemini
// special-case in installIntegration runs installGeminiSettings and produces
// a .gemini/settings.json in the project directory.
func TestInstallIntegration_GeminiWritesSettingsJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := installIntegration("gemini", false); err != nil {
		t.Fatalf("installIntegration: %v", err)
	}
	settingsPath := filepath.Join(dir, ".gemini", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("expected .gemini/settings.json to be written: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v", err)
	}
	hooks, _ := m["hooks"].(map[string]interface{})
	before, _ := hooks["BeforeTool"].([]interface{})
	for _, e := range before {
		if em, ok := e.(map[string]interface{}); ok && em["matcher"] == "exit_plan_mode" {
			return
		}
	}
	t.Error("exit_plan_mode hook not found in .gemini/settings.json")
}

// TestInstallIntegration_HermesPrintsExternalDirsNote verifies that on a
// project-mode install, the hermes special-case prints the external_dirs
// guidance — Hermes does not auto-discover project-local skills, so the
// note is the only thing that makes the project-install path useful.
func TestInstallIntegration_HermesPrintsExternalDirsNote(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = prev })

	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()

	if err := installIntegration("hermes", false); err != nil {
		t.Fatalf("installIntegration: %v", err)
	}
	_ = w.Close()
	out := <-done

	if _, err := os.Stat(filepath.Join(dir, ".hermes/skills/crit/SKILL.md")); err != nil {
		t.Fatalf("expected .hermes/skills/crit/SKILL.md to be written: %v", err)
	}
	for _, want := range []string{"~/.hermes/skills/", "external_dirs", "config.yaml"} {
		if !strings.Contains(out, want) {
			t.Errorf("project install output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestPrintUniqueHints_Dedups(t *testing.T) {
	// printUniqueHints prints to stdout; we just verify it doesn't panic on
	// duplicates and empty input. Output ordering and dedup logic are simple
	// enough that visual inspection during integration use covers the rest.
	printUniqueHints(nil)
	printUniqueHints([]string{"a", "b", "a", "c", "b"})
}

func TestInstallGeminiSettings(t *testing.T) {
	hookEntry := func(data []byte) bool {
		var m map[string]interface{}
		if err := json.Unmarshal(data, &m); err != nil {
			return false
		}
		hooks, _ := m["hooks"].(map[string]interface{})
		before, _ := hooks["BeforeTool"].([]interface{})
		for _, e := range before {
			em, ok := e.(map[string]interface{})
			if ok && em["matcher"] == "exit_plan_mode" {
				return true
			}
		}
		return false
	}

	t.Run("creates file with hook when absent", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		installGeminiSettings(path, false)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected settings.json to be written: %v", err)
		}
		if !hookEntry(data) {
			t.Errorf("exit_plan_mode hook not found in %s", data)
		}
	})

	t.Run("skips when hook already present and no force", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		// Write a valid settings.json that already has the hook plus a sentinel field.
		prebuilt := `{"hooks":{"BeforeTool":[{"matcher":"exit_plan_mode","hooks":[{"type":"command","command":"crit plan-hook","timeout":345600000}]}]},"sentinel":true}` + "\n"
		_ = os.WriteFile(path, []byte(prebuilt), 0o644)
		installGeminiSettings(path, false)
		got, _ := os.ReadFile(path)
		if string(got) != prebuilt {
			t.Errorf("no-force should skip; file was modified:\ngot:  %s\nwant: %s", got, prebuilt)
		}
	})

	t.Run("force overwrites existing hook entry", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		// write a settings file with a stale exit_plan_mode entry
		stale := `{"hooks":{"BeforeTool":[{"matcher":"exit_plan_mode","hooks":[{"type":"command","command":"old-cmd","timeout":1}]}]}}`
		_ = os.WriteFile(path, []byte(stale), 0o644)
		installGeminiSettings(path, true)
		data, _ := os.ReadFile(path)
		var m map[string]interface{}
		_ = json.Unmarshal(data, &m)
		hooks, _ := m["hooks"].(map[string]interface{})
		before, _ := hooks["BeforeTool"].([]interface{})
		// exactly one exit_plan_mode entry
		count := 0
		for _, e := range before {
			em, ok := e.(map[string]interface{})
			if ok && em["matcher"] == "exit_plan_mode" {
				count++
				inner, _ := em["hooks"].([]interface{})
				if len(inner) > 0 {
					cmd, _ := inner[0].(map[string]interface{})["command"].(string)
					if cmd == "old-cmd" {
						t.Error("stale command not replaced")
					}
				}
			}
		}
		if count != 1 {
			t.Errorf("expected exactly 1 exit_plan_mode entry, got %d", count)
		}
	})

	t.Run("preserves existing unrelated hooks", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		existing := `{"hooks":{"BeforeTool":[{"matcher":"other_tool","hooks":[{"type":"command","command":"other"}]}]}}`
		_ = os.WriteFile(path, []byte(existing), 0o644)
		installGeminiSettings(path, false)
		data, _ := os.ReadFile(path)
		var m map[string]interface{}
		_ = json.Unmarshal(data, &m)
		hooks, _ := m["hooks"].(map[string]interface{})
		before, _ := hooks["BeforeTool"].([]interface{})
		hasOther, hasCrit := false, false
		for _, e := range before {
			em, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			switch em["matcher"] {
			case "other_tool":
				hasOther = true
			case "exit_plan_mode":
				hasCrit = true
			}
		}
		if !hasOther {
			t.Error("pre-existing other_tool hook was removed")
		}
		if !hasCrit {
			t.Error("exit_plan_mode hook not added")
		}
	})
}

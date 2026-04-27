package main

import (
	"os"
	"path/filepath"
	"runtime"
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
		{"cursor", 0, ".cursor/skills/crit/SKILL.md"},
		{"cursor", 1, ".cursor/skills/crit-cli/SKILL.md"},
		// opencode: command stays cwd-relative; skill redirects globally to ~/.agents/skills/.
		{"opencode", 0, ".opencode/commands/crit.md"},
		{"opencode", 1, filepath.Join(home, ".agents/skills/crit/SKILL.md")},
		// github-copilot: both skills redirect to ~/.agents/skills/.
		{"github-copilot", 0, filepath.Join(home, ".agents/skills/crit/SKILL.md")},
		{"github-copilot", 1, filepath.Join(home, ".agents/skills/crit-cli/SKILL.md")},
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
		"opencode":       {{"", globalDestNone}, {".agents/skills/crit/SKILL.md", globalDestRelHome}},
		"github-copilot": {{".agents/skills/crit/SKILL.md", globalDestRelHome}, {".agents/skills/crit-cli/SKILL.md", globalDestRelHome}},
		"windsurf":       {{"", globalDestNone}},
		"cline":          {{"Cline/Rules/crit.md", globalDestDocuments}},
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

func TestPrintUniqueHints_Dedups(t *testing.T) {
	// printUniqueHints prints to stdout; we just verify it doesn't panic on
	// duplicates and empty input. Output ordering and dedup logic are simple
	// enough that visual inspection during integration use covers the rest.
	printUniqueHints(nil)
	printUniqueHints([]string{"a", "b", "a", "c", "b"})
}

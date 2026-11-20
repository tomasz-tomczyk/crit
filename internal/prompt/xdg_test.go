package prompt_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/prompt"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func writePrompt(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Global prompt files are discovered under $XDG_CONFIG_HOME/crit/prompts on a
// fresh install; the source label shows the absolute path because it is
// outside the home directory.
func TestRenderHook_GlobalFileUnderXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	testutil.SetFreshHome(t, home)
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	dir := filepath.Join(cfgHome, "crit", "prompts")
	writePrompt(t, dir, "on_story_generate.md", "XDG FILE")

	res, err := prompt.RenderHook(nil, nil, t.TempDir(), home, true, prompt.HookStoryGenerate, storyData())
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "XDG FILE" {
		t.Fatalf("text = %q", res.Text)
	}
	want := "global:" + filepath.ToSlash(filepath.Join(dir, "on_story_generate.md"))
	if res.Meta.TemplateSource != want {
		t.Fatalf("source = %q, want %q", res.Meta.TemplateSource, want)
	}
}

// The XDG location wins over the legacy one once the user has moved prompts
// there, and the label stays relative to home for the default location.
func TestRenderHook_GlobalXDGFileWinsOverLegacy(t *testing.T) {
	home := t.TempDir()
	testutil.SetFreshHome(t, home)
	writePrompt(t, filepath.Join(home, ".crit", "prompts"), "on_story_generate.md", "LEGACY FILE")
	writePrompt(t, filepath.Join(home, ".config", "crit", "prompts"), "on_story_generate.md", "XDG FILE")

	res, err := prompt.RenderHook(nil, nil, t.TempDir(), home, true, prompt.HookStoryGenerate, storyData())
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "XDG FILE" {
		t.Fatalf("text = %q", res.Text)
	}
	if res.Meta.TemplateSource != "global:.config/crit/prompts/on_story_generate.md" {
		t.Fatalf("source = %q", res.Meta.TemplateSource)
	}
}

// An existing install keeps reading ~/.crit/prompts even with XDG_CONFIG_HOME
// set, as long as nothing was moved to the XDG location.
func TestRenderHook_LegacyGlobalFileStillUsedWithXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	testutil.SetFreshHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writePrompt(t, filepath.Join(home, ".crit", "prompts"), "on_story_generate.md", "LEGACY FILE")

	res, err := prompt.RenderHook(nil, nil, t.TempDir(), home, true, prompt.HookStoryGenerate, storyData())
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "LEGACY FILE" {
		t.Fatalf("text = %q", res.Text)
	}
	if res.Meta.TemplateSource != "global:.crit/prompts/on_story_generate.md" {
		t.Fatalf("source = %q", res.Meta.TemplateSource)
	}
}

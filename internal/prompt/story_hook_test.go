package prompt_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/prompt"
)

func storyData() map[string]any {
	return prompt.StoryContext{
		PrepPath:        "/tmp/prep.txt",
		StorySchemaJSON: `{"type":"object"}`,
		CommitMessages:  "abc123 add feature",
		DiffScopeKind:   "workingTree",
		SessionKey:      "abcd1234",
		ReviewPath:      "/tmp/review.json",
	}.TemplateData()
}

// Level 1: project config `prompts` entry.
func TestRenderHook_StoryGenerate_ProjectConfig(t *testing.T) {
	dir := t.TempDir()
	project := map[string]string{
		prompt.HookStoryGenerate: "inline:PROJECT CONFIG {{.prep_path}}",
	}
	res, err := prompt.RenderHook(nil, project, dir, "", true, prompt.HookStoryGenerate, storyData())
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "PROJECT CONFIG /tmp/prep.txt" {
		t.Fatalf("text = %q", res.Text)
	}
	if res.Meta == nil || res.Meta.Hook != prompt.HookStoryGenerate {
		t.Fatalf("meta = %+v", res.Meta)
	}
}

// Level 2: global config `prompts` entry (wins when project has none, or when
// project prompts are untrusted).
func TestRenderHook_StoryGenerate_GlobalConfig(t *testing.T) {
	dir := t.TempDir()
	global := map[string]string{
		prompt.HookStoryGenerate: "inline:GLOBAL CONFIG",
	}
	res, err := prompt.RenderHook(global, nil, dir, "", true, prompt.HookStoryGenerate, storyData())
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "GLOBAL CONFIG" {
		t.Fatalf("text = %q", res.Text)
	}
	if res.Meta == nil || res.Meta.TemplateSource != "global:inline" {
		t.Fatalf("meta = %+v", res.Meta)
	}
}

// Level 3: project conventional file under .crit/prompts/on_story_generate.md.
func TestRenderHook_StoryGenerate_ProjectFile(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, ".crit", "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "on_story_generate.md"), []byte("PROJECT FILE {{.diff_scope_kind}}"), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := prompt.RenderHook(nil, nil, dir, "", true, prompt.HookStoryGenerate, storyData())
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "PROJECT FILE workingTree" {
		t.Fatalf("text = %q", res.Text)
	}
	if res.Meta.TemplateSource != "project:.crit/prompts/on_story_generate.md" {
		t.Fatalf("source = %q", res.Meta.TemplateSource)
	}
}

// Level 4: global conventional file under ~/.crit/prompts/on_story_generate.md.
func TestRenderHook_StoryGenerate_GlobalFile(t *testing.T) {
	projectDir := t.TempDir()
	homeDir := t.TempDir()
	promptsDir := filepath.Join(homeDir, ".crit", "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "on_story_generate.md"), []byte("GLOBAL FILE"), 0644); err != nil {
		t.Fatal(err)
	}
	res, err := prompt.RenderHook(nil, nil, projectDir, homeDir, true, prompt.HookStoryGenerate, storyData())
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "GLOBAL FILE" {
		t.Fatalf("text = %q", res.Text)
	}
	if res.Meta.TemplateSource != "global:.crit/prompts/on_story_generate.md" {
		t.Fatalf("source = %q", res.Meta.TemplateSource)
	}
}

// Level 5: stock fallback when nothing above matches.
func TestRenderHook_StoryGenerate_Stock(t *testing.T) {
	res, err := prompt.RenderHook(nil, nil, "", "", false, prompt.HookStoryGenerate, storyData())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "/tmp/prep.txt") {
		t.Fatalf("expected prep path interpolated into stock template: %q", res.Text)
	}
	if !strings.Contains(res.Text, "Explainer, not reviewer") {
		t.Fatalf("expected stock guide body: %q", res.Text)
	}
	if res.Meta == nil || !strings.HasPrefix(res.Meta.TemplateSource, "stock:") {
		t.Fatalf("expected stock meta, got %+v", res.Meta)
	}
}

// Project-level override requires trust — useProject=false (untrusted) must
// fall through to global/file/stock, mirroring the existing finish-hook trust
// gating (render.go's useProject param, driven by EvaluateTrust).
func TestRenderHook_StoryGenerate_UntrustedProjectFallsThrough(t *testing.T) {
	dir := t.TempDir()
	project := map[string]string{
		prompt.HookStoryGenerate: "inline:SHOULD NOT BE USED",
	}
	global := map[string]string{
		prompt.HookStoryGenerate: "inline:GLOBAL WINS",
	}
	res, err := prompt.RenderHook(global, project, dir, "", false, prompt.HookStoryGenerate, storyData())
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "GLOBAL WINS" {
		t.Fatalf("text = %q (project override should be ignored when untrusted)", res.Text)
	}
}

// Project-level on_story_generate.md must feed the trust content hash so
// changing it re-blocks trust, exactly like on_finish_*.md files today.
func TestContentHash_IncludesStoryGenerateFile(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, ".crit", "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(promptsDir, "on_story_generate.md")
	if err := os.WriteFile(path, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	h1 := prompt.ContentHash(nil, dir)
	if err := os.WriteFile(path, []byte("v2"), 0644); err != nil {
		t.Fatal(err)
	}
	h2 := prompt.ContentHash(nil, dir)
	if h1 == h2 {
		t.Fatal("expected content hash to change when on_story_generate.md changes")
	}
}

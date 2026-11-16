package prompt_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/prompt"
)

func TestResolveFinishTemplate_ProjectLoadErrorFallsBackToGlobal(t *testing.T) {
	resolved, err := prompt.ResolveFinishTemplate(
		map[string]string{prompt.HookFinishApproved: "inline:global"},
		map[string]string{prompt.HookFinishApproved: "file:missing.md"},
		t.TempDir(),
		t.TempDir(),
		prompt.HookFinishApproved,
		"files",
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == nil {
		t.Fatal("expected global fallback after project prompt load failure")
	}
	if resolved.Text != "global" || resolved.Layer != prompt.LayerGlobal {
		t.Fatalf("resolved = %+v, want global inline prompt", resolved)
	}
}

func TestRenderFinish_MalformedTemplateFallsBackToStock(t *testing.T) {
	result := prompt.RenderFinish(
		nil,
		map[string]string{prompt.HookFinishApproved: "inline:{{"},
		t.TempDir(),
		"",
		true,
		prompt.Context{Approved: true, Mode: "files"},
	)

	if !strings.HasPrefix(result.Prompt, "Note: the custom prompt on_finish_approved (project:inline) failed to render") {
		t.Fatalf("Prompt = %q, want a failure note first", result.Prompt)
	}
	if result.Meta == nil || result.Meta.TemplateSource != "stock:on_finish_approved.md" {
		t.Fatalf("Meta = %+v, want stock source", result.Meta)
	}
}

func TestRenderFinish_UnresolvedRenderErrorKeepsComments(t *testing.T) {
	// Parses fine but fails at execution time: len of an int.
	global := map[string]string{
		prompt.HookFinishUnresolved: "inline:{{len .unresolved_count}}",
	}
	ctx := prompt.Context{
		Mode:                   "files",
		UnresolvedCount:        2,
		CommentsUnresolvedJSON: `[{"id":"c_1","body":"fix the bug"}]`,
		InternalSessionMode:    "files",
		NextRoundCmd:           "crit --session abcd",
	}
	result := prompt.RenderFinish(global, nil, "", t.TempDir(), false, ctx)

	if !strings.HasPrefix(result.Prompt, "Note: the custom prompt on_finish_unresolved (global:inline) failed to render") {
		t.Fatalf("Prompt = %q, want a failure note first", result.Prompt)
	}
	for _, want := range []string{"2 unresolved comments", "fix the bug", "crit --session abcd"} {
		if !strings.Contains(result.Prompt, want) {
			t.Errorf("Prompt missing %q: %q", want, result.Prompt)
		}
	}
	if result.Meta == nil || result.Meta.Hook != "on_finish_unresolved:files" || result.Meta.TemplateSource != "stock:on_finish_unresolved.md" {
		t.Fatalf("Meta = %+v, want stock on_finish_unresolved:files", result.Meta)
	}
}

func TestRenderFinish_StoryRenderErrorFallsBackToStockStory(t *testing.T) {
	project := map[string]string{
		"on_finish_unresolved:story": "inline:{{.unresolved_count",
	}
	ctx := prompt.Context{
		Mode:                   "story",
		UnresolvedCount:        1,
		CommentsUnresolvedJSON: `[{"id":"c_1","body":"fix"}]`,
		InternalSessionMode:    "git",
	}
	result := prompt.RenderFinish(nil, project, t.TempDir(), "", true, ctx)

	if !strings.Contains(result.Prompt, "story-mode review finished with 1 unresolved comment") {
		t.Fatalf("Prompt = %q, want stock story prompt", result.Prompt)
	}
	if result.Meta == nil || result.Meta.Hook != "on_finish_unresolved:story" || result.Meta.TemplateSource != "stock:on_finish_unresolved.story.md" {
		t.Fatalf("Meta = %+v, want stock story source", result.Meta)
	}
}

func TestRenderHook_UnknownHookReturnsError(t *testing.T) {
	result, err := prompt.RenderHook(nil, nil, "", "", false, "unknown_hook", nil)
	if err == nil {
		t.Fatalf("RenderHook() = %+v, nil; want error", result)
	}
	if !strings.Contains(err.Error(), `no template found for hook "unknown_hook"`) {
		t.Fatalf("error = %q", err)
	}
}

func TestListProjectPromptSources_ConfigAndReferencedFiles(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	sources := prompt.ListProjectPromptSources(map[string]string{
		prompt.HookFinishApproved:          "inline:done",
		prompt.HookFinishUnresolved:        "file:.crit/prompts/custom.md",
		prompt.HookFinishUnresolved + ":x": "file:.crit/prompts/custom.md",
	}, projectDir)

	want := map[string]bool{
		"project:.crit.config.json":       true,
		"project:.crit/prompts/custom.md": true,
	}
	if len(sources) != len(want) {
		t.Fatalf("sources = %v, want exactly %v", sources, want)
	}
	for _, source := range sources {
		if !want[source] {
			t.Errorf("unexpected source %q in %v", source, sources)
		}
	}
}

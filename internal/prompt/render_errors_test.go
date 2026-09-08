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

func TestRenderFinish_MalformedTemplateUsesSafeFallback(t *testing.T) {
	result := prompt.RenderFinish(
		nil,
		map[string]string{prompt.HookFinishApproved: "inline:{{"},
		t.TempDir(),
		"",
		true,
		prompt.Context{Approved: true, Mode: "files"},
	)

	if result.Prompt != "Review finished." {
		t.Fatalf("Prompt = %q, want safe fallback", result.Prompt)
	}
	if result.Meta != nil {
		t.Fatalf("Meta = %+v, want nil when rendering fails", result.Meta)
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

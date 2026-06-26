package prompt_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/prompt"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestLookupPrompt_ModeFallback(t *testing.T) {
	prompts := map[string]string{
		"on_finish_unresolved":      "inline:generic",
		"on_finish_unresolved:diff": "inline:diff-specific",
	}
	if v, k := prompt.LookupPrompt(prompts, prompt.HookFinishUnresolved, "diff"); v != "inline:diff-specific" || k != "on_finish_unresolved:diff" {
		t.Fatalf("mode-specific = %q %q", v, k)
	}
	if v, k := prompt.LookupPrompt(prompts, prompt.HookFinishUnresolved, "files"); v != "inline:generic" || k != "on_finish_unresolved" {
		t.Fatalf("fallback = %q %q", v, k)
	}
}

func TestPromptMode(t *testing.T) {
	if got := prompt.PromptMode("live", "git"); got != "live" {
		t.Fatalf("live = %q", got)
	}
	if got := prompt.PromptMode("", "git"); got != "diff" {
		t.Fatalf("git = %q", got)
	}
	if got := prompt.PromptMode("", "plan"); got != "files" {
		t.Fatalf("plan = %q", got)
	}
}

func TestDiscoverPromptFile_ModeSpecific(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, ".crit", "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "on_finish_unresolved.diff.md"), []byte("DIFF TEMPLATE"), 0644); err != nil {
		t.Fatal(err)
	}
	text, source, ok := prompt.DiscoverPromptFile(dir, prompt.HookFinishUnresolved, "diff", prompt.LayerProject)
	if !ok || text != "DIFF TEMPLATE" {
		t.Fatalf("discover = %q %q %v", text, source, ok)
	}
	if source != "project:.crit/prompts/on_finish_unresolved.diff.md" {
		t.Fatalf("source = %q", source)
	}
}

func TestRenderFinish_DiscoveredProjectFile(t *testing.T) {
	dir := t.TempDir()
	promptsDir := filepath.Join(dir, ".crit", "prompts")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "on_finish_unresolved.md"), []byte("AUTO {{.unresolved_count}}"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx := prompt.Context{
		Mode:                "files",
		UnresolvedCount:     2,
		InternalSessionMode: "files",
		Approved:            false,
	}
	result := prompt.RenderFinish(nil, nil, dir, "", true, ctx)
	if result.Prompt != "AUTO 2" {
		t.Fatalf("prompt = %q", result.Prompt)
	}
	if result.Meta == nil || result.Meta.TemplateSource != "project:.crit/prompts/on_finish_unresolved.md" {
		t.Fatalf("meta = %+v", result.Meta)
	}
}

func TestRenderFinish_DefaultUnchanged(t *testing.T) {
	ctx := prompt.Context{
		ReviewPath:             "/tmp/review.json",
		CommentsCmd:            "crit comments --json '/tmp/review.json'",
		NextRoundCmd:           "crit --session abcd",
		Mode:                   "files",
		UnresolvedCount:        2,
		TotalCount:             2,
		CommentsUnresolvedJSON: `[{"id":"c_1","body":"fix"}]`,
		InternalSessionMode:    "files",
		Approved:               false,
	}
	result := prompt.RenderFinish(nil, nil, "", "", false, ctx)
	if !strings.Contains(result.Prompt, "2 unresolved comments") {
		t.Fatalf("prompt: %q", result.Prompt)
	}
	if !strings.Contains(result.Prompt, "fix") {
		t.Fatalf("prompt should embed comments_json: %q", result.Prompt)
	}
	if !strings.Contains(result.Prompt, "Address each comment") {
		t.Fatalf("prompt: %q", result.Prompt)
	}
	if result.Meta == nil || !strings.HasPrefix(result.Meta.TemplateSource, "stock:") {
		t.Fatalf("expected stock meta, got %+v", result.Meta)
	}
}

func TestRenderFinish_CustomTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.md")
	if err := os.WriteFile(path, []byte("CUSTOM {{.unresolved_count}}"), 0644); err != nil {
		t.Fatal(err)
	}
	project := map[string]string{
		"on_finish_unresolved": "file:" + path,
	}
	ctx := prompt.Context{
		Mode:                "files",
		UnresolvedCount:     3,
		InternalSessionMode: "files",
		Approved:            false,
		NextRoundCmd:        "crit",
	}
	result := prompt.RenderFinish(nil, project, dir, "", true, ctx)
	if !strings.Contains(result.Prompt, "CUSTOM 3") {
		t.Fatalf("rendered: %q", result.Prompt)
	}
	if result.Meta == nil || result.Meta.Hook != "on_finish_unresolved" {
		t.Fatalf("meta: %+v", result.Meta)
	}
}

func TestEvaluateTrust_UntilChange(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	projectDir := t.TempDir()
	projectPrompts := map[string]string{
		"on_finish_approved": "inline:Approved custom",
	}
	hash := prompt.ContentHash(projectPrompts, projectDir)

	st, err := prompt.EvaluateTrust(projectDir, projectPrompts)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Untrusted {
		t.Fatal("expected untrusted initially")
	}

	if err := prompt.SaveTrustChoice(projectDir, prompt.TrustUntilChange, hash); err != nil {
		t.Fatal(err)
	}
	st, err = prompt.EvaluateTrust(projectDir, projectPrompts)
	if err != nil {
		t.Fatal(err)
	}
	if st.Untrusted || !st.UseProject {
		t.Fatalf("expected trusted: %+v", st)
	}

	projectPrompts["on_finish_approved"] = "inline:changed"
	st, err = prompt.EvaluateTrust(projectDir, projectPrompts)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Untrusted {
		t.Fatal("expected re-block after prompt change")
	}
}

func TestEvaluateTrust_DefaultsIgnoresProject(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	projectDir := t.TempDir()
	projectPrompts := map[string]string{"on_finish_approved": "inline:custom"}
	if err := prompt.SaveTrustChoice(projectDir, prompt.TrustDefaults, ""); err != nil {
		t.Fatal(err)
	}
	st, err := prompt.EvaluateTrust(projectDir, projectPrompts)
	if err != nil {
		t.Fatal(err)
	}
	if st.Untrusted || st.UseProject {
		t.Fatalf("defaults mode: %+v", st)
	}
}

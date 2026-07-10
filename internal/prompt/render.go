package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Layer identifies whether a template value came from global or project config.
type Layer string

const (
	LayerGlobal  Layer = "global"
	LayerProject Layer = "project"
)

// ResolvedTemplate is a loaded template ready to render.
type ResolvedTemplate struct {
	Text   string
	Hook   string
	Source string // e.g. project:.crit/prompts/foo.md
	Layer  Layer
}

// ResolveFinishTemplate picks the effective template for a finish hook.
// Project values override global. Returns nil when no custom template is configured.
func ResolveFinishTemplate(globalPrompts, projectPrompts map[string]string, projectDir, homeDir, hook, mode string, useProject bool) (*ResolvedTemplate, error) {
	if v, key := LookupPrompt(projectPrompts, hook, mode); v != "" && useProject {
		text, err := LoadTemplate(v, projectDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: loading project prompt %s: %v\n", key, err)
		} else {
			return &ResolvedTemplate{
				Text:   text,
				Hook:   key,
				Source: TemplateSource(string(LayerProject), v),
				Layer:  LayerProject,
			}, nil
		}
	}
	if v, key := LookupPrompt(globalPrompts, hook, mode); v != "" {
		text, err := LoadTemplate(v, homeDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: loading global prompt %s: %v\n", key, err)
			return nil, nil
		}
		return &ResolvedTemplate{
			Text:   text,
			Hook:   key,
			Source: TemplateSource(string(LayerGlobal), v),
			Layer:  LayerGlobal,
		}, nil
	}
	if useProject {
		if text, source, ok := DiscoverPromptFile(projectDir, hook, mode, LayerProject); ok {
			specific, _ := ResolveHookKey(hook, mode)
			return &ResolvedTemplate{
				Text:   text,
				Hook:   specific,
				Source: source,
				Layer:  LayerProject,
			}, nil
		}
	}
	if text, source, ok := DiscoverPromptFile(homeDir, hook, mode, LayerGlobal); ok {
		specific, _ := ResolveHookKey(hook, mode)
		return &ResolvedTemplate{
			Text:   text,
			Hook:   specific,
			Source: source,
			Layer:  LayerGlobal,
		}, nil
	}
	return nil, nil
}

// ResolveFinishTemplateSpecific is like ResolveFinishTemplate, but does not
// fall back from a mode-specific hook to the generic hook. Pass mode="" to
// resolve only the generic hook.
func ResolveFinishTemplateSpecific(globalPrompts, projectPrompts map[string]string, projectDir, homeDir, hook, mode string, useProject bool) (*ResolvedTemplate, error) {
	key := hook
	if mode != "" {
		key, _ = ResolveHookKey(hook, mode)
	}
	if v, ok := projectPrompts[key]; ok && strings.TrimSpace(v) != "" && useProject {
		text, err := LoadTemplate(v, projectDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: loading project prompt %s: %v\n", key, err)
		} else {
			return &ResolvedTemplate{
				Text:   text,
				Hook:   key,
				Source: TemplateSource(string(LayerProject), v),
				Layer:  LayerProject,
			}, nil
		}
	}
	if v, ok := globalPrompts[key]; ok && strings.TrimSpace(v) != "" {
		text, err := LoadTemplate(v, homeDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: loading global prompt %s: %v\n", key, err)
			return nil, nil
		}
		return &ResolvedTemplate{
			Text:   text,
			Hook:   key,
			Source: TemplateSource(string(LayerGlobal), v),
			Layer:  LayerGlobal,
		}, nil
	}
	if useProject {
		if text, source, ok := DiscoverPromptFileSpecific(projectDir, hook, mode, LayerProject); ok {
			return &ResolvedTemplate{
				Text:   text,
				Hook:   key,
				Source: source,
				Layer:  LayerProject,
			}, nil
		}
	}
	if text, source, ok := DiscoverPromptFileSpecific(homeDir, hook, mode, LayerGlobal); ok {
		return &ResolvedTemplate{
			Text:   text,
			Hook:   key,
			Source: source,
			Layer:  LayerGlobal,
		}, nil
	}
	return nil, nil
}

// FinishResult holds the rendered finish prompt (stdout, modal, and API).
type FinishResult struct {
	Prompt string
	Meta   *Meta
}

// RenderFinish produces the finish prompt for stdout, the finish modal, and API JSON.
func RenderFinish(globalPrompts, projectPrompts map[string]string, projectDir, homeDir string, useProject bool, ctx Context) FinishResult {
	hook := HookForFinish(ctx.Approved)
	mode := ctx.Mode

	text, source, hookKey := resolveTemplateText(globalPrompts, projectPrompts, projectDir, homeDir, hook, mode, useProject)
	if text == "" {
		return FinishResult{Prompt: "Review finished."}
	}

	rendered, err := Render(text, ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: rendering prompt %s: %v\n", hookKey, err)
		return FinishResult{Prompt: "Review finished."}
	}

	return FinishResult{
		Prompt: strings.TrimRight(rendered, "\n"),
		Meta:   &Meta{Hook: hookKey, TemplateSource: source},
	}
}

// HookResult holds a rendered non-finish hook (e.g. on_story_generate).
type HookResult struct {
	Text string
	Meta *Meta
}

// RenderHook resolves and renders a mode-less hook (no :mode split) through
// the same 5-level precedence as finish hooks: project config -> global
// config -> project .crit/prompts/ file -> global ~/.crit/prompts/ file ->
// stock. data supplies the template variables (snake_case keys, as returned
// by a TemplateData()-shaped map).
func RenderHook(globalPrompts, projectPrompts map[string]string, projectDir, homeDir string, useProject bool, hook string, data map[string]any) (HookResult, error) {
	text, source, hookKey := resolveTemplateText(globalPrompts, projectPrompts, projectDir, homeDir, hook, "", useProject)
	if text == "" {
		return HookResult{}, fmt.Errorf("no template found for hook %q", hook)
	}
	rendered, err := RenderData(text, data)
	if err != nil {
		return HookResult{}, fmt.Errorf("rendering prompt %s: %w", hookKey, err)
	}
	return HookResult{
		Text: rendered,
		Meta: &Meta{Hook: hookKey, TemplateSource: source},
	}, nil
}

func resolveTemplateText(globalPrompts, projectPrompts map[string]string, projectDir, homeDir, hook, mode string, useProject bool) (text, source, hookKey string) {
	if mode == "story" {
		if resolved, _ := ResolveFinishTemplateSpecific(globalPrompts, projectPrompts, projectDir, homeDir, hook, mode, useProject); resolved != nil {
			return resolved.Text, resolved.Source, resolved.Hook
		}
		if stockText, stockSource, ok := LoadStockTemplateSpecific(hook, mode); ok {
			specific, _ := ResolveHookKey(hook, mode)
			return stockText, stockSource, specific
		}
		if resolved, _ := ResolveFinishTemplateSpecific(globalPrompts, projectPrompts, projectDir, homeDir, hook, "", useProject); resolved != nil {
			return resolved.Text, resolved.Source, resolved.Hook
		}
		if stockText, stockSource, ok := LoadStockTemplateSpecific(hook, ""); ok {
			return stockText, stockSource, hook
		}
		return "", "", ""
	}
	if resolved, _ := ResolveFinishTemplate(globalPrompts, projectPrompts, projectDir, homeDir, hook, mode, useProject); resolved != nil {
		return resolved.Text, resolved.Source, resolved.Hook
	}
	if stockText, stockSource, ok := LoadStockTemplate(hook, mode); ok {
		specific, _ := ResolveHookKey(hook, mode)
		return stockText, stockSource, specific
	}
	return "", "", ""
}

// ListProjectPromptSources returns human-readable source paths for project prompt config.
func ListProjectPromptSources(projectPrompts map[string]string, projectDir string) []string {
	if len(projectPrompts) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	configPath := filepath.Join(projectDir, ".crit.config.json")
	if _, err := os.Stat(configPath); err == nil {
		out = append(out, "project:.crit.config.json")
		seen["project:.crit.config.json"] = struct{}{}
	}
	for _, v := range projectPrompts {
		if !strings.HasPrefix(v, prefixFile) {
			continue
		}
		rel := strings.TrimPrefix(v, prefixFile)
		label := "project:" + filepath.ToSlash(rel)
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	return out
}

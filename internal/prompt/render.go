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

func resolveTemplateText(globalPrompts, projectPrompts map[string]string, projectDir, homeDir, hook, mode string, useProject bool) (text, source, hookKey string) {
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

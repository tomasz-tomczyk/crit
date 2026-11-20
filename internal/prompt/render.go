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

type resolvedTemplate struct {
	text   string
	hook   string
	source string
	layer  Layer
}

// Problem identifies an invalid finish prompt.
type Problem struct {
	Hook   string `json:"hook"`
	Source string `json:"source"`
	Error  string `json:"error"`
}

// String formats the problem as "hook (source): error" for CLI output.
func (p Problem) String() string {
	return fmt.Sprintf("%s (%s): %s", p.Hook, p.Source, p.Error)
}

func (t resolvedTemplate) problem(err error) *Problem {
	return &Problem{Hook: t.hook, Source: t.source, Error: err.Error()}
}

func resolveCustomTemplate(globalPrompts, projectPrompts map[string]string, projectDir, homeDir, hook, mode string, useProject, finish bool) (*resolvedTemplate, *Problem) {
	type layerConfig struct {
		prompts map[string]string
		dir     string
		name    Layer
	}
	var layers []layerConfig
	if useProject {
		layers = append(layers, layerConfig{projectPrompts, projectDir, LayerProject})
	}
	layers = append(layers, layerConfig{globalPrompts, homeDir, LayerGlobal})
	for _, layer := range layers {
		value, key := LookupPrompt(layer.prompts, hook, mode)
		if value == "" {
			continue
		}
		text, err := LoadTemplate(value, layer.dir)
		t := &resolvedTemplate{text: text, hook: key, source: TemplateSource(string(layer.name), value), layer: layer.name}
		if err == nil {
			return t, nil
		}
		if finish {
			return nil, t.problem(fmt.Errorf("loading template: %w", err))
		}
		fmt.Fprintf(os.Stderr, "Warning: loading %s prompt %s: %v\n", layer.name, key, err)
		if layer.name == LayerGlobal {
			return nil, nil
		}
	}
	for _, layer := range layers {
		if t, problem := discoverPromptTemplate(layer.dir, hook, mode, layer.name, finish); t != nil || problem != nil {
			return t, problem
		}
	}
	return nil, nil
}

func resolveFinishTemplate(globalPrompts, projectPrompts map[string]string, projectDir, homeDir, hook, mode string, useProject bool) (*resolvedTemplate, *Problem) {
	specific, _ := ResolveHookKey(hook, mode)
	if mode == "story" {
		if t, problem := resolveCustomTemplate(globalPrompts, projectPrompts, projectDir, homeDir, specific, "", useProject, true); t != nil || problem != nil {
			return t, problem
		}
		if text, source, ok := LoadStockTemplateSpecific(hook, mode); ok {
			return &resolvedTemplate{text: text, hook: specific, source: source}, nil
		}
		mode = ""
		specific = hook
	}
	if t, problem := resolveCustomTemplate(globalPrompts, projectPrompts, projectDir, homeDir, hook, mode, useProject, true); t != nil || problem != nil {
		return t, problem
	}
	if text, source, ok := LoadStockTemplate(hook, mode); ok {
		return &resolvedTemplate{text: text, hook: specific, source: source}, nil
	}
	return nil, nil
}

// FinishResult holds the rendered finish prompt or a problem with its template.
type FinishResult struct {
	Prompt  string
	Meta    *Meta
	Problem *Problem
}

// RenderFinish produces the finish prompt for stdout, the finish modal, and API JSON.
func RenderFinish(globalPrompts, projectPrompts map[string]string, projectDir, homeDir string, useProject bool, ctx Context) FinishResult {
	t, problem := resolveFinishTemplate(globalPrompts, projectPrompts, projectDir, homeDir, HookForFinish(ctx.Approved), ctx.Mode, useProject)
	if problem != nil {
		return FinishResult{Problem: problem}
	}
	if t == nil {
		return FinishResult{Prompt: "Review finished."}
	}
	rendered, err := Render(t.text, ctx)
	if err != nil {
		return FinishResult{Problem: t.problem(err)}
	}
	return FinishResult{
		Prompt: strings.TrimRight(rendered, "\n"),
		Meta:   &Meta{Hook: t.hook, TemplateSource: t.source},
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
	t, _ := resolveCustomTemplate(globalPrompts, projectPrompts, projectDir, homeDir, hook, "", useProject, false)
	if t == nil {
		if text, source, ok := LoadStockTemplate(hook, ""); ok {
			t = &resolvedTemplate{text: text, hook: hook, source: source}
		}
	}
	if t == nil || t.text == "" {
		return HookResult{}, fmt.Errorf("no template found for hook %q", hook)
	}
	rendered, err := RenderData(t.text, data)
	if err != nil {
		return HookResult{}, fmt.Errorf("rendering prompt %s: %w", hook, err)
	}
	return HookResult{
		Text: rendered,
		Meta: &Meta{Hook: hook, TemplateSource: t.source},
	}, nil
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

package prompt

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

const (
	prefixInline = "inline:"
	prefixFile   = "file:"
)

// LoadTemplate resolves a config value (inline: or file:) to template text.
// baseDir is the directory for relative file: paths (project root or home).
func LoadTemplate(value, baseDir string) (string, error) {
	switch {
	case strings.HasPrefix(value, prefixInline):
		return strings.TrimPrefix(value, prefixInline), nil
	case strings.HasPrefix(value, prefixFile):
		path := strings.TrimPrefix(value, prefixFile)
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	default:
		return "", fmt.Errorf("prompt value must start with %q or %q", prefixInline, prefixFile)
	}
}

// Render executes a finish template with strict lookup of context keys.
func Render(tmplText string, ctx Context) (string, error) {
	return renderData(tmplText, ctx.TemplateData(), true)
}

// RenderData executes a Go text/template against a raw snake_case data map
// (used by hooks that don't have a dedicated Context type, e.g. on_story_generate).
func RenderData(tmplText string, data map[string]any) (string, error) {
	return renderData(tmplText, data, false)
}

func renderData(tmplText string, data map[string]any, finish bool) (string, error) {
	tmpl, err := parseTemplate(tmplText, finish)
	if err != nil {
		return "", err
	}
	return executeTemplate(tmpl, data)
}

func parseTemplate(text string, finish bool) (*template.Template, error) {
	tmpl := template.New("prompt")
	if finish {
		tmpl.Option("missingkey=error")
	}
	parsed, err := tmpl.Parse(text)
	if err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}
	return parsed, nil
}

func executeTemplate(tmpl *template.Template, data map[string]any) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}
	return buf.String(), nil
}

// TemplateSource describes where a template value came from.
func TemplateSource(layer, value string) string {
	if strings.HasPrefix(value, prefixFile) {
		return layer + ":" + strings.TrimPrefix(value, prefixFile)
	}
	return layer + ":inline"
}

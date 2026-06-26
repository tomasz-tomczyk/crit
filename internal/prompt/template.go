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

// Render executes a Go text/template with the given context.
func Render(tmplText string, ctx Context) (string, error) {
	tmpl, err := template.New("prompt").Parse(tmplText)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx.TemplateData()); err != nil {
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

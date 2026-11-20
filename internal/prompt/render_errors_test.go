package prompt_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/prompt"
)

func TestInvalidFinishTemplates(t *testing.T) {
	for _, tc := range []struct {
		name, hook, mode, value, source, problem string
		approved                                 bool
	}{
		{"missing file", "on_finish_approved", "files", "file:missing.md", "global:missing.md", "loading template", true},
		{"parse error", "on_finish_approved", "files", "inline:{{", "global:inline", "parsing template", true},
		{"execution error", "on_finish_unresolved", "files", "inline:{{len .unresolved_count}}", "global:inline", "executing template", false},
		{"missing key", "on_finish_unresolved", "files", "inline:{{.renamed_count}}", "global:inline", "renamed_count", false},
		{"story variant", "on_finish_unresolved:story", "story", "inline:{{.unresolved_count", "global:inline", "parsing template", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			global := map[string]string{tc.hook: tc.value}
			problems := prompt.Check(global, nil, "", home, tc.mode, false)
			if len(problems) != 1 || problems[0].Hook != tc.hook || problems[0].Source != tc.source || !strings.Contains(problems[0].Error, tc.problem) {
				t.Fatalf("Check() = %+v, want %s at %s containing %q", problems, tc.hook, tc.source, tc.problem)
			}
			result := prompt.RenderFinish(global, nil, "", home, false, prompt.Context{Approved: tc.approved, Mode: tc.mode})
			if result.Prompt != "" || result.Meta != nil || result.Problem == nil ||
				result.Problem.Hook != tc.hook || result.Problem.Source != tc.source || !strings.Contains(result.Problem.Error, tc.problem) {
				t.Fatalf("RenderFinish() = %+v, problem = %+v, want no prompt and a problem containing %q", result, result.Problem, tc.problem)
			}
		})
	}
}

func TestCheck_FindsUnknownFieldsThroughoutTemplate(t *testing.T) {
	global := map[string]string{
		"on_finish_approved": `inline:{{$root := .}}{{if false}}
			{{printf "%v" .renamed_count}}
			{{with .session_stats}}{{.renamed_duration}}{{end}}
			{{range .files_with_comments}}{{$root.renamed_mode}}{{end}}
			{{template "details" .}}
			{{(index . "session_stats").renamed_files}}
		{{end}}
		{{define "details"}}{{if false}}{{$.renamed_session}}{{end}}{{end}}
		{{define "unused"}}{{.renamed_unused}}{{end}}`,
	}
	problems := prompt.Check(global, nil, "", t.TempDir(), "files", false)
	if len(problems) != 1 {
		t.Fatalf("Check() = %+v, want one problem", problems)
	}
	for _, key := range []string{"renamed_count", "renamed_duration", "renamed_mode", "renamed_session", "renamed_files", "renamed_unused"} {
		if !strings.Contains(problems[0].Error, key) {
			t.Errorf("Check() error %q omits %q", problems[0].Error, key)
		}
	}
}

func TestCheck_RespectsModeTrustAndDotScope(t *testing.T) {
	global := map[string]string{
		"on_finish_approved":       `inline:{{template "stats" .session_stats}}{{define "stats"}}{{.duration}}{{end}}{{$value := .}}{{if .approved}}{{$value = .session_stats}}{{end}}{{$value.duration}}`,
		"on_finish_approved:diff":  "inline:{{.invalid_diff}}",
		"on_finish_approved:story": "inline:{{.invalid_story}}",
	}
	project := map[string]string{
		"on_finish_approved":   `inline:{{with .session_stats}}{{.duration}}{{else}}{{.mode}}{{end}}`,
		"on_finish_unresolved": "file:missing.md",
	}
	projectDir, home := t.TempDir(), t.TempDir()
	if problems := prompt.Check(global, project, projectDir, home, "files", false); len(problems) != 0 {
		t.Fatalf("Check(files, untrusted) = %+v", problems)
	}
	if problems := prompt.Check(global, project, projectDir, home, "files", true); len(problems) != 1 || problems[0].Source != "project:missing.md" {
		t.Fatalf("Check(files, trusted) = %+v, want missing project file", problems)
	}
	if problems := prompt.Check(global, project, projectDir, home, "story", true); len(problems) != 1 || problems[0].Hook != "on_finish_approved:story" {
		t.Fatalf("Check(story, trusted) = %+v, want invalid story prompt only", problems)
	}
}

func TestRenderFinish_OptionalStatsAndEmptyTemplate(t *testing.T) {
	global := map[string]string{
		"on_finish_approved": `inline:{{if .session_stats}}{{.session_stats.duration}}{{end}}`,
	}
	result := prompt.RenderFinish(global, nil, "", t.TempDir(), false, prompt.Context{Approved: true, Mode: "files"})
	if result.Prompt != "" || result.Meta == nil || result.Problem != nil {
		t.Fatalf("RenderFinish() = %+v, want an empty rendered prompt from the custom template", result)
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

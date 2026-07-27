package comment

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCommentFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want commentFlags
	}{
		{
			name: "no flags",
			args: []string{"hello", "world"},
			want: commentFlags{args: []string{"hello", "world"}},
		},
		{
			name: "author flag",
			args: []string{"--author", "alice", "comment body"},
			want: commentFlags{author: "alice", args: []string{"comment body"}},
		},
		{
			name: "reply-to flag",
			args: []string{"--reply-to", "c_abc123", "reply body"},
			want: commentFlags{replyTo: "c_abc123", args: []string{"reply body"}},
		},
		{
			name: "resolve flag",
			args: []string{"--resolve", "done"},
			want: commentFlags{resolve: true, args: []string{"done"}},
		},
		{
			name: "path flag",
			args: []string{"--path", "main.go", "fix here"},
			want: commentFlags{path: "main.go", args: []string{"fix here"}},
		},
		{
			name: "json flag",
			args: []string{"--json"},
			want: commentFlags{json: true},
		},
		{
			name: "plan flag",
			args: []string{"--plan", "my-plan", "comment"},
			want: commentFlags{plan: "my-plan", args: []string{"comment"}},
		},
		{
			name: "multiple flags combined",
			args: []string{"--author", "bob", "--reply-to", "c1", "--resolve", "fixed it"},
			want: commentFlags{
				author:  "bob",
				replyTo: "c1",
				resolve: true,
				args:    []string{"fixed it"},
			},
		},
		{
			name: "empty args",
			args: []string{},
			want: commentFlags{},
		},
		{
			name: "output flag",
			args: []string{"--output", "/tmp/review", "body"},
			want: commentFlags{outputDir: "/tmp/review", args: []string{"body"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCommentFlags(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if got.author != tt.want.author {
				t.Errorf("author = %q, want %q", got.author, tt.want.author)
			}
			if got.replyTo != tt.want.replyTo {
				t.Errorf("replyTo = %q, want %q", got.replyTo, tt.want.replyTo)
			}
			if got.resolve != tt.want.resolve {
				t.Errorf("resolve = %v, want %v", got.resolve, tt.want.resolve)
			}
			if got.path != tt.want.path {
				t.Errorf("path = %q, want %q", got.path, tt.want.path)
			}
			if got.json != tt.want.json {
				t.Errorf("json = %v, want %v", got.json, tt.want.json)
			}
			if got.plan != tt.want.plan {
				t.Errorf("plan = %q, want %q", got.plan, tt.want.plan)
			}
			if got.outputDir != tt.want.outputDir {
				t.Errorf("outputDir = %q, want %q", got.outputDir, tt.want.outputDir)
			}
			if len(got.args) != len(tt.want.args) {
				t.Errorf("args len = %d, want %d", len(got.args), len(tt.want.args))
			} else {
				for i := range got.args {
					if got.args[i] != tt.want.args[i] {
						t.Errorf("args[%d] = %q, want %q", i, got.args[i], tt.want.args[i])
					}
				}
			}
		})
	}
}

func TestResolveCommentFlagsOutputPrecedence(t *testing.T) {
	projectDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Chdir(projectDir)

	configuredOutput := filepath.Join(projectDir, "reviews")
	configData := []byte(`{"output":` + `"` + configuredOutput + `"` + `}`)
	if err := os.WriteFile(filepath.Join(projectDir, ".crit.config.json"), configData, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("configured output", func(t *testing.T) {
		f, err := parseCommentFlags([]string{"body"})
		if err != nil {
			t.Fatal(err)
		}
		if err := resolveCommentFlags(&f); err != nil {
			t.Fatal(err)
		}
		if f.configuredOutput != configuredOutput {
			t.Fatalf("configuredOutput = %q, want %q", f.configuredOutput, configuredOutput)
		}
		if f.reviewPath != filepath.Join(configuredOutput, ".crit") {
			t.Fatalf("reviewPath = %q, want configured review path", f.reviewPath)
		}
	})

	t.Run("explicit output wins", func(t *testing.T) {
		explicitOutput := filepath.Join(projectDir, "explicit")
		f, err := parseCommentFlags([]string{"--output", explicitOutput, "body"})
		if err != nil {
			t.Fatal(err)
		}
		if err := resolveCommentFlags(&f); err != nil {
			t.Fatal(err)
		}
		if f.outputDir != explicitOutput {
			t.Fatalf("outputDir = %q, want explicit output %q", f.outputDir, explicitOutput)
		}
		if f.reviewPath != filepath.Join(explicitOutput, ".crit") {
			t.Fatalf("reviewPath = %q, want explicit review path", f.reviewPath)
		}
	})

	t.Run("plan storage wins without conflict", func(t *testing.T) {
		f, err := parseCommentFlags([]string{"--plan", "my-plan", "body"})
		if err != nil {
			t.Fatal(err)
		}
		if err := resolveCommentFlags(&f); err != nil {
			t.Fatal(err)
		}
		if f.outputDir == "" || f.outputDir == configuredOutput {
			t.Fatalf("outputDir = %q, want plan storage", f.outputDir)
		}
		if f.reviewPath != filepath.Join(f.outputDir, ".crit") {
			t.Fatalf("reviewPath = %q, want plan review path", f.reviewPath)
		}
	})
}

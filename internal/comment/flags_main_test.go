package comment

import (
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

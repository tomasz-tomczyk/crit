package vcs

import (
	"strings"
	"testing"
)

func TestResolveCommitSHA(t *testing.T) {
	ClearGitEnvForTest(t) // robust under git hooks that export GIT_DIR
	dir := InitTestRepo(t)
	head := CommitAtForTest(t, dir, "a.txt", "a\n", "second")
	parent := GitRun(t, dir, "rev-parse", "HEAD~1")
	missing40 := strings.Repeat("ab", 20)
	missing64 := strings.Repeat("cd", 32)

	tests := []struct {
		name    string
		ref     string
		want    string
		wantErr bool
	}{
		{name: "HEAD", ref: "HEAD", want: head},
		{name: "short sha", ref: head[:8], want: head},
		{name: "full sha", ref: head, want: head},
		{name: "uppercase full sha", ref: strings.ToUpper(head), want: head},
		{name: "relative ref", ref: "HEAD~1", want: parent},
		{name: "branch", ref: "main", want: head},
		{name: "surrounding space", ref: "  HEAD\n", want: head},
		{name: "unknown full sha passes through", ref: strings.ToUpper(missing40), want: missing40},
		{name: "unknown sha256 passes through", ref: missing64, want: missing64},
		{name: "unknown short sha", ref: "abcdef12", wantErr: true},
		{name: "garbage", ref: "not-a-commit", wantErr: true},
		{name: "option-like", ref: "--all", wantErr: true},
		{name: "empty", ref: "", wantErr: true},
		{name: "tree object is not a commit", ref: "HEAD^{tree}", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveCommitSHA(dir, tt.ref)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveCommitSHA(%q) = %q, want error", tt.ref, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveCommitSHA(%q): %v", tt.ref, err)
			}
			if got != tt.want {
				t.Fatalf("ResolveCommitSHA(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

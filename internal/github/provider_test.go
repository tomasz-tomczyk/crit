package github

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/forge"
)

func installProviderFakeGH(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-gh shim is a POSIX shell script; not portable to Windows")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
case "$1 $2" in
  "auth status") exit 0 ;;
  "pr view") echo 42 ;;
  "pr list") echo '[{"number":4,"title":"Open PR","url":"https://github.com/acme/widget/pull/4","headRefName":"feature","headRefOid":"head","baseRefName":"main","isDraft":true}]' ;;
  api\ repos/*) echo '{"content":"aGVsbG8=","encoding":"base64"}' ;;
  *) echo "unsupported fake gh call: $*" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestGitHubChangeNumber(t *testing.T) {
	for _, input := range []string{"42", "https://github.com/acme/widget/pull/42", "https://github.example/acme/widget/pull/42/files"} {
		got, err := githubChangeNumber(input)
		if err != nil || got != 42 {
			t.Errorf("githubChangeNumber(%q) = (%d, %v)", input, got, err)
		}
	}
}

func TestGitHubProviderAuthDetectListAndFetch(t *testing.T) {
	installProviderFakeGH(t)
	p := Provider{}
	if p.Kind() != forge.GitHub {
		t.Fatalf("kind = %q", p.Kind())
	}
	if err := p.RequireAuth(context.Background(), forge.RepoContext{}); err != nil {
		t.Fatal(err)
	}
	id, err := p.Detect(context.Background(), forge.RepoContext{})
	if err != nil || id.Number != 42 {
		t.Fatalf("detect = (%+v, %v)", id, err)
	}
	changes, err := p.ListOpen(context.Background(), forge.RepoContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Number != 4 || changes[0].Provider != forge.GitHub || !changes[0].Draft {
		t.Fatalf("open changes = %+v", changes)
	}
	content, err := p.FetchFile(context.Background(), forge.RepoContext{}, forge.RepoRef{Project: "acme/widget"}, "head", "main.go")
	if err != nil || string(content) != "hello" {
		t.Fatalf("FetchFile = (%q, %v)", content, err)
	}
	p.Invalidate(forge.ChangeID{Number: 42})
}

func TestGitHubProviderGetTranslatesMetadata(t *testing.T) {
	wantErr := errors.New("fetch failed")
	withFetchPRByNumber(t, func(number int) (*PRInfo, error) {
		if number == 99 {
			return nil, wantErr
		}
		if number == 98 {
			return nil, nil
		}
		return &PRInfo{
			URL: "https://github.com/acme/widget/pull/7", Title: "Feature", Body: "Body", State: "OPEN", IsDraft: true,
			BaseRefName: "main", HeadRefName: "feature", BaseRefOid: "base", HeadRefOid: "head",
			HeadRepoURL: "https://github.com/alice/widget.git", IsCrossRepository: true,
			Additions: 10, Deletions: 3, ChangedFiles: 2, AuthorLogin: "alice", CreatedAt: "2026-01-01T00:00:00Z",
		}, nil
	})
	p := Provider{}
	change, err := p.Get(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 7})
	if err != nil {
		t.Fatal(err)
	}
	if change.Title != "Feature" || change.HeadRepo.Project != "alice/widget" || !change.CrossRepository || change.Additions != 10 {
		t.Fatalf("change = %+v", change)
	}
	if _, err := p.Get(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 99}); !errors.Is(err, wantErr) {
		t.Fatalf("fetch error = %v", err)
	}
	if _, err := p.Get(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 98}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("nil PR error = %v", err)
	}
}

func TestGitHubProviderArgumentTranslation(t *testing.T) {
	pull, err := githubPullArgs(forge.PullRequest{ChangeSpec: "https://github.com/acme/widget/pull/12/files", OutputDir: "reviews"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pull, []string{"--output", "reviews", "12"}) {
		t.Fatalf("pull args = %v", pull)
	}
	pull, err = githubPullArgs(forge.PullRequest{ChangeSpec: "12", SessionID: "aaaaaaaaaaaa"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pull, []string{"--session", "aaaaaaaaaaaa", "12"}) {
		t.Fatalf("session pull args = %v", pull)
	}
	compat := []string{"--output", "legacy", "3"}
	pull, err = githubPullArgs(forge.PullRequest{Args: compat})
	if err != nil || !reflect.DeepEqual(pull, compat) {
		t.Fatalf("compat pull args = (%v, %v)", pull, err)
	}
	push, err := githubPushArgs(forge.PushRequest{ChangeSpec: "12", OutputDir: "reviews", DryRun: true, Event: "approve", Message: "ship"})
	if err != nil {
		t.Fatal(err)
	}
	wantPush := []string{"--dry-run", "--event", "approve", "--message", "ship", "--output", "reviews", "12"}
	if !reflect.DeepEqual(push, wantPush) {
		t.Fatalf("push args = %v, want %v", push, wantPush)
	}
	push, err = githubPushArgs(forge.PushRequest{ChangeSpec: "12", SessionID: "bbbbbbbbbbbb", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	wantSessionPush := []string{"--dry-run", "--session", "bbbbbbbbbbbb", "12"}
	if !reflect.DeepEqual(push, wantSessionPush) {
		t.Fatalf("session push args = %v, want %v", push, wantSessionPush)
	}
	if _, err := githubPullArgs(forge.PullRequest{ChangeSpec: "bad"}); err == nil {
		t.Fatal("invalid pull spec unexpectedly accepted")
	}
	if _, err := githubPushArgs(forge.PushRequest{ChangeSpec: "bad"}); err == nil {
		t.Fatal("invalid push spec unexpectedly accepted")
	}
}

func TestGitHubProviderMethodsRejectInvalidSpecsBeforeCLI(t *testing.T) {
	p := Provider{}
	if _, err := p.Pull(context.Background(), forge.PullRequest{ChangeSpec: "bad"}); err == nil {
		t.Fatal("Pull unexpectedly accepted invalid spec")
	}
	if _, err := p.Push(context.Background(), forge.PushRequest{ChangeSpec: "bad"}); err == nil {
		t.Fatal("Push unexpectedly accepted invalid spec")
	}
	if _, err := p.FetchFile(context.Background(), forge.RepoContext{}, forge.RepoRef{Project: "invalid"}, "sha", "a.go"); err == nil {
		t.Fatal("FetchFile unexpectedly accepted invalid project")
	}
}

func TestGitHubProjectNormalization(t *testing.T) {
	tests := map[string]string{
		"https://github.com/acme/widget.git": "acme/widget",
		"git@github.com:acme/widget.git":     "git@github.com:acme/widget",
		"/acme/widget/":                      "acme/widget",
		"":                                   "",
	}
	for input, want := range tests {
		if got := githubProject(input); got != want {
			t.Errorf("githubProject(%q) = %q, want %q", input, got, want)
		}
	}
}

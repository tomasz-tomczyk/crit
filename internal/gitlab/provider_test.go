package gitlab

import (
	"context"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/forge"
)

func TestProviderIdentityAndRepoHost(t *testing.T) {
	p := Provider{Host: "gitlab.example.com"}
	if p.Kind() != forge.GitLab {
		t.Fatalf("kind = %q", p.Kind())
	}
	repo := p.repo(forge.RepoContext{Host: "gitlab.com", Project: "acme/widget"})
	if repo.Host != p.Host || repo.Project != "acme/widget" {
		t.Fatalf("repo = %+v", repo)
	}
	id, err := p.changeID(forge.ChangeID{Number: 4})
	if err != nil || id.Host != p.Host {
		t.Fatalf("changeID = (%+v, %v)", id, err)
	}
}

func TestProviderRequireAuth(t *testing.T) {
	calls := stubCommands(t, commandResponse{})
	p := Provider{Host: "gitlab.example.com"}
	if err := p.RequireAuth(context.Background(), forge.RepoContext{}); err != nil {
		t.Fatal(err)
	}
	assertCommand(t, (*calls)[0], "glab", "auth", "status", "--hostname", "gitlab.example.com")
}

func TestProviderDetectCurrentBranchMR(t *testing.T) {
	calls := stubCommands(t,
		commandResponse{stdout: "feature/with space\n"},
		commandResponse{stdout: `[{"iid":17}]`},
	)
	id, err := (Provider{Host: "gitlab.example.com"}).Detect(context.Background(), forge.RepoContext{Project: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	if id != (forge.ChangeID{Number: 17, Host: "gitlab.example.com", Project: "acme/widget"}) {
		t.Fatalf("detected ID = %+v", id)
	}
	assertCommand(t, (*calls)[0], "git", "branch", "--show-current")
	if !strings.Contains(strings.Join((*calls)[1].args, " "), "source_branch=feature%2Fwith+space") {
		t.Fatalf("detect endpoint was not query escaped: %v", (*calls)[1].args)
	}
}

func TestProviderDetectFailures(t *testing.T) {
	t.Run("no branch", func(t *testing.T) {
		stubCommands(t, commandResponse{exitCode: 1})
		_, err := (Provider{}).Detect(context.Background(), forge.RepoContext{})
		if err == nil || !strings.Contains(err.Error(), "cannot detect current branch") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("invalid response", func(t *testing.T) {
		stubCommands(t, commandResponse{stdout: "feature\n"}, commandResponse{stdout: "{"})
		_, err := (Provider{}).Detect(context.Background(), forge.RepoContext{})
		if err == nil || !strings.Contains(err.Error(), "parsing GitLab merge request list") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("no merge request", func(t *testing.T) {
		stubCommands(t, commandResponse{stdout: "feature\n"}, commandResponse{stdout: "[]"})
		_, err := (Provider{}).Detect(context.Background(), forge.RepoContext{})
		if err == nil || !strings.Contains(err.Error(), "no GitLab merge request") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestProviderGetTranslatesMergeRequestAndProjects(t *testing.T) {
	stubCommands(t,
		commandResponse{stdout: `{
          "iid":7,"web_url":"https://gitlab.example/acme/widget/-/merge_requests/7",
          "title":"Add widget","description":"Body","state":"opened","work_in_progress":true,
          "source_branch":"feature","target_branch":"main","source_project_id":22,"target_project_id":11,
          "changes_count":"13","author":{"name":"Alice","username":"alice"},"created_at":"2026-01-02T03:04:05Z",
          "diff_refs":{"base_sha":"base","start_sha":"start","head_sha":"head"}
        }`},
		commandResponse{stdout: `{"path_with_namespace":"acme/widget","web_url":"https://gitlab.example/acme/widget"}`},
		commandResponse{stdout: `{"path_with_namespace":"alice/widget","web_url":"https://gitlab.example/alice/widget/"}`},
	)
	change, err := (Provider{Host: "gitlab.example"}).Get(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 7, Project: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	if change.Title != "Add widget" || !change.Draft || !change.CrossRepository || change.ChangedFiles != 13 || change.Author != "Alice" {
		t.Fatalf("change = %+v", change)
	}
	if change.BaseRepo.Project != "acme/widget" || change.HeadRepo.Project != "alice/widget" || change.HeadRepo.CloneURL != "https://gitlab.example/alice/widget.git" {
		t.Fatalf("repo refs = base %+v, head %+v", change.BaseRepo, change.HeadRepo)
	}
}

func TestProviderGetValidationAndDecodeErrors(t *testing.T) {
	p := Provider{Host: "gitlab.example"}
	if _, err := p.Get(context.Background(), forge.RepoContext{}, forge.ChangeID{}); err == nil || !strings.Contains(err.Error(), "invalid GitLab merge request IID") {
		t.Fatalf("invalid IID error = %v", err)
	}
	if _, err := p.Get(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 1, Host: "gitlab.com"}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("host mismatch error = %v", err)
	}
	stubCommands(t, commandResponse{stdout: "{"})
	if _, err := p.Get(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 1}); err == nil || !strings.Contains(err.Error(), "parsing GitLab merge request") {
		t.Fatalf("decode error = %v", err)
	}
}

func TestProviderListOpen(t *testing.T) {
	stubCommands(t, commandResponse{stdout: `[
      {"iid":2,"title":"Two","web_url":"u2","source_branch":"f2","target_branch":"main","sha":"fallback","draft":true},
      {"iid":3,"title":"Three","web_url":"u3","source_branch":"f3","target_branch":"develop","diff_refs":{"head_sha":"head3"}}
    ]`})
	changes, err := (Provider{Host: "gitlab.example"}).ListOpen(context.Background(), forge.RepoContext{Project: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].HeadSHA != "fallback" || !changes[0].Draft || changes[1].HeadSHA != "head3" || changes[1].Provider != forge.GitLab {
		t.Fatalf("changes = %+v", changes)
	}
}

func TestProviderFetchFile(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		calls := stubCommands(t, commandResponse{stdout: "package main\n"})
		content, err := (Provider{Host: "gitlab.example"}).FetchFile(context.Background(), forge.RepoContext{}, forge.RepoRef{Project: "acme/widget", Host: "gitlab.example"}, "abc 123", "dir/a b.go")
		if err != nil || string(content) != "package main\n" {
			t.Fatalf("FetchFile = (%q, %v)", content, err)
		}
		endpoint := (*calls)[0].args[len((*calls)[0].args)-1]
		if endpoint != "projects/acme%2Fwidget/repository/files/dir%2Fa%20b.go/raw?ref=abc+123" {
			t.Fatalf("endpoint = %q", endpoint)
		}
	})
	t.Run("host mismatch", func(t *testing.T) {
		if _, err := (Provider{Host: "gitlab.example"}).FetchFile(context.Background(), forge.RepoContext{}, forge.RepoRef{Host: "gitlab.com"}, "sha", "a.go"); err == nil {
			t.Fatal("host mismatch unexpectedly succeeded")
		}
	})
}

func TestProviderFetchDiffsTranslatesStatusesAndHunks(t *testing.T) {
	stubCommands(t, commandResponse{stdout: `[
      {"old_path":"a.go","new_path":"a.go","diff":"@@ -1 +1 @@\n-old\n+new\n"},
      {"old_path":"/dev/null","new_path":"b.go","new_file":true,"diff":"@@ -0,0 +1 @@\n+new\n"},
      {"old_path":"c.go","new_path":"c.go","deleted_file":true,"diff":"@@ -1 +0,0 @@\n-old\n"},
      {"old_path":"d.go","new_path":"e.go","renamed_file":true,"diff":""}
    ]`})
	files, err := (Provider{Host: "gitlab.example"}).FetchDiffs(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 4, Project: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 || files[0].Status != "modified" || files[1].Status != "added" || files[2].Status != "deleted" || files[3].Status != "renamed" {
		t.Fatalf("diff files = %+v", files)
	}
	if len(files[0].Hunks) != 1 || files[0].Hunks[0].NewStart != 1 {
		t.Fatalf("parsed hunks = %+v", files[0].Hunks)
	}
}

func TestProviderHelpers(t *testing.T) {
	if first("", "second", "third") != "second" || first("", "") != "" {
		t.Fatal("first returned unexpected value")
	}
	if atoi("12") != 12 || atoi("bad") != 0 {
		t.Fatal("atoi returned unexpected value")
	}
	if displayName(gitlabUser{Name: "  ", Username: "alice"}) != "alice" {
		t.Fatal("displayName did not fall back to username")
	}
	if ref := projectRef(context.Background(), "gitlab.example", 0); ref.Host != "gitlab.example" || ref.Project != "" {
		t.Fatalf("zero project ref = %+v", ref)
	}
	(Provider{}).Invalidate(forge.ChangeID{Number: 1})
}

func TestProviderPullAndPushValidateCompatibilityArgs(t *testing.T) {
	p := Provider{Host: "gitlab.example"}
	if _, err := p.Pull(context.Background(), forge.PullRequest{Args: []string{"1", "2"}}); err == nil {
		t.Fatal("Pull unexpectedly accepted duplicate change specs")
	}
	if _, err := p.Push(context.Background(), forge.PushRequest{Args: []string{"--event", "invalid", "1"}}); err == nil {
		t.Fatal("Push unexpectedly accepted invalid event")
	}
}

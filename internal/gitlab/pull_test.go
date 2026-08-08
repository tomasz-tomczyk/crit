package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func isolatedReviewOutput(t *testing.T) string {
	t.Helper()
	testutil.SetHome(t, t.TempDir())
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })
	return t.TempDir()
}

func TestParseGitLabPullFlags(t *testing.T) {
	flags, err := parsePullFlags([]string{"-o", "reviews", "https://gitlab.example/a/b/-/merge_requests/7"})
	if err != nil {
		t.Fatal(err)
	}
	if flags.outputDir != "reviews" || flags.spec == "" {
		t.Fatalf("flags = %+v", flags)
	}
	if _, err := parsePullFlags([]string{"--output"}); err == nil {
		t.Fatal("missing output value unexpectedly accepted")
	}
	_, err = parsePullFlags([]string{"1", "2"})
	var exitErr clicmd.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("duplicate specs error = %v", err)
	}
}

func TestRunPullImportsAndPersistsGitLabDiscussion(t *testing.T) {
	outputDir := isolatedReviewOutput(t)
	discussions := `[{
      "id":"discussion-1",
      "notes":[{
        "id":101,"body":"Please fix this","author":{"username":"reviewer"},"created_at":"2026-01-01T00:00:00Z",
        "position":{"position_type":"text","base_sha":"base","start_sha":"start","head_sha":"head","new_path":"main.go","new_line":3}
      }]
    }]`
	calls := stubCommands(t, commandResponse{}, commandResponse{stdout: discussions})
	result, err := runPull(context.Background(), forge.PullRequest{
		Repo: forge.RepoContext{Host: "gitlab.example", Project: "acme/widget"}, ChangeSpec: "7", OutputDir: outputDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Imported != 1 || result.Updated != 0 {
		t.Fatalf("pull result = %+v", result)
	}
	if len(*calls) != 2 {
		t.Fatalf("commands = %+v", *calls)
	}
	identity, err := review.ResolveReviewPath(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	cj, err := review.LoadCritJSON(identity)
	if err != nil {
		t.Fatal(err)
	}
	comment := cj.Files["main.go"].Comments[0]
	if comment.Body != "Please fix this" || comment.GitLabNoteID != 101 || comment.GitLabDiscussionID != "discussion-1" {
		t.Fatalf("persisted comment = %+v", comment)
	}
}

func TestRunPullSupportsCompatibilityArgsAndAuthFailure(t *testing.T) {
	outputDir := isolatedReviewOutput(t)
	stubCommands(t, commandResponse{stderr: "expired", exitCode: 1})
	_, err := runPull(context.Background(), forge.PullRequest{
		Repo: forge.RepoContext{Host: "gitlab.example"}, Args: []string{"--output", outputDir, "9"},
	})
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("auth failure = %v", err)
	}
}

func TestFetchAllDiscussionsPaginates(t *testing.T) {
	firstPage := make([]gitlabDiscussion, 100)
	for i := range firstPage {
		firstPage[i].ID = "page-one"
	}
	firstJSON, err := json.Marshal(firstPage)
	if err != nil {
		t.Fatal(err)
	}
	calls := stubCommands(t,
		commandResponse{stdout: string(firstJSON)},
		commandResponse{stdout: `[{"id":"last"}]`},
	)
	all, err := fetchAllDiscussions(context.Background(), forge.RepoContext{Host: "gitlab.example"}, forge.ChangeID{Number: 3, Project: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 101 || all[100].ID != "last" || len(*calls) != 2 {
		t.Fatalf("discussions = %d, calls = %d", len(all), len(*calls))
	}
	if !strings.Contains((*calls)[1].args[len((*calls)[1].args)-1], "page=2") {
		t.Fatalf("second page endpoint = %v", (*calls)[1].args)
	}
}

func TestFetchAllDiscussionsRejectsMalformedJSON(t *testing.T) {
	stubCommands(t, commandResponse{stdout: "{"})
	_, err := fetchAllDiscussions(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 3})
	if err == nil || !strings.Contains(err.Error(), "parsing GitLab discussions") {
		t.Fatalf("decode error = %v", err)
	}
}

func TestLoadOrInitializeReview(t *testing.T) {
	t.Run("initializes missing review", func(t *testing.T) {
		identity := filepath.Join(t.TempDir(), "review")
		cj, err := loadOrInitializeReview(identity)
		if err != nil {
			t.Fatal(err)
		}
		if cj.Files == nil || cj.ReviewRound != 1 {
			t.Fatalf("initialized review = %+v", cj)
		}
	})
	t.Run("loads existing review", func(t *testing.T) {
		identity := filepath.Join(t.TempDir(), "review")
		want := session.CritJSON{ReviewRound: 4, Files: map[string]session.CritJSONFile{"a.go": {Status: "modified"}}}
		if err := review.SaveCritJSON(identity, want); err != nil {
			t.Fatal(err)
		}
		got, err := loadOrInitializeReview(identity)
		if err != nil || got.ReviewRound != 4 || got.Files["a.go"].Status != "modified" {
			t.Fatalf("loaded review = (%+v, %v)", got, err)
		}
	})
	t.Run("rejects invalid review", func(t *testing.T) {
		identity := filepath.Join(t.TempDir(), "review")
		path := session.ReviewPathsFor(identity).Review
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := loadOrInitializeReview(identity); err == nil || !strings.Contains(err.Error(), "invalid review file") {
			t.Fatalf("invalid review error = %v", err)
		}
	})
}

func TestResolveChangeIDAppliesConfiguredHost(t *testing.T) {
	id, err := resolveChangeID(context.Background(), forge.RepoContext{Host: "gitlab.example"}, "12")
	if err != nil {
		t.Fatal(err)
	}
	if id.Number != 12 || id.Host != "gitlab.example" {
		t.Fatalf("change ID = %+v", id)
	}
}

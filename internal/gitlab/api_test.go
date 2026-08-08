package gitlab

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/forge"
)

func TestRequireGLabUsesConfiguredHost(t *testing.T) {
	calls := stubCommands(t, commandResponse{})
	if err := requireGLab(context.Background(), forge.RepoContext{Host: "gitlab.example.com"}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(*calls))
	}
	assertCommand(t, (*calls)[0], "glab", "auth", "status", "--hostname", "gitlab.example.com")
}

func TestRequireGLabSurfacesAuthenticationFailure(t *testing.T) {
	stubCommands(t, commandResponse{stderr: "token expired", exitCode: 1})
	err := requireGLab(context.Background(), forge.RepoContext{Host: "gitlab.example.com"})
	if err == nil || !strings.Contains(err.Error(), "not authenticated for gitlab.example.com") || !strings.Contains(err.Error(), "token expired") {
		t.Fatalf("auth error = %v", err)
	}
}

func TestRunAPIBuildsRequestAndPayload(t *testing.T) {
	calls := stubCommands(t, commandResponse{stdout: `{"id":7}`, wantStdin: `{"body":"hello"}`, checkStdin: true})
	out, err := runAPI(context.Background(), "gitlab.example.com", "projects/1/notes", "POST", map[string]any{"body": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"id":7}` {
		t.Fatalf("output = %q", out)
	}
	assertCommand(t, (*calls)[0], "glab", "api", "--method", "POST", "--hostname", "gitlab.example.com", "projects/1/notes", "--header", "Content-Type: application/json", "--input", "-")
}

func TestRunAPIGetAndFailures(t *testing.T) {
	t.Run("get", func(t *testing.T) {
		calls := stubCommands(t, commandResponse{stdout: "[]"})
		if _, err := runAPI(context.Background(), "", "projects/:fullpath", "GET", nil); err != nil {
			t.Fatal(err)
		}
		assertCommand(t, (*calls)[0], "glab", "api", "projects/:fullpath")
	})

	t.Run("command error", func(t *testing.T) {
		stubCommands(t, commandResponse{stderr: "forbidden", exitCode: 1})
		_, err := runAPI(context.Background(), "", "projects/1", "GET", nil)
		if err == nil || !strings.Contains(err.Error(), "glab api projects/1: forbidden") {
			t.Fatalf("API error = %v", err)
		}
	})

	t.Run("marshal error", func(t *testing.T) {
		calls := stubCommands(t)
		_, err := runAPI(context.Background(), "", "projects/1", "POST", make(chan int))
		if err == nil || !strings.Contains(err.Error(), "marshal GitLab API payload") {
			t.Fatalf("marshal error = %v", err)
		}
		if len(*calls) != 0 {
			t.Fatalf("command ran after marshal failure: %+v", *calls)
		}
	})
}

func TestGitLabAPIHelpers(t *testing.T) {
	if hostSuffix("") != "" || hostSuffix("gitlab.example") != " for gitlab.example" {
		t.Fatal("hostSuffix did not format host")
	}
	id := forge.ChangeID{Number: 9, Project: "group/sub/repo"}
	if got := projectEndpoint(id, "/discussions"); got != "projects/group%2Fsub%2Frepo/merge_requests/9/discussions" {
		t.Fatalf("project endpoint = %q", got)
	}
	if got := projectEndpoint(forge.ChangeID{Number: 2}, ""); got != "projects/:fullpath/merge_requests/2" {
		t.Fatalf("default project endpoint = %q", got)
	}
}

func TestIsNotFoundAPIError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("glab api projects/x: HTTP 404: exit status 1"), true},
		{fmt.Errorf(`glab api projects/x: {"message":"404 Not Found"}: exit status 1`), true},
		{fmt.Errorf("glab api projects/x: token expired: exit status 1"), false},
	}
	for _, tc := range cases {
		if got := isNotFoundAPIError(tc.err); got != tc.want {
			t.Fatalf("isNotFoundAPIError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

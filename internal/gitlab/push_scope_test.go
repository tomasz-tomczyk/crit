package gitlab

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

// TestRefreshGitLabIDsStampsImportsWithDaemonFocusKey guards #1234 for the
// notes crit push imports after publishing: they must get the MR's
// project-qualified focus key, the one the daemon's page filters by, both
// with a daemon running and headless.
func TestRefreshGitLabIDsStampsImportsWithDaemonFocusKey(t *testing.T) {
	daemonFocus := &session.Focus{
		Kind: session.FocusRange, Forge: "gitlab", ChangeNumber: 7, BaseSHA: "base", HeadSHA: "head",
		RemoteBaseProject: "acme/widget", RemoteHost: "gitlab.example", DiffScope: session.DiffScopeLayer,
	}
	cases := []struct {
		name        string
		daemonFocus *session.Focus
		responses   []commandResponse
		wantCalls   int
	}{
		{
			name:        "daemon running",
			daemonFocus: daemonFocus,
			wantCalls:   1,
		},
		{
			name: "headless looks up the target project",
			responses: []commandResponse{
				{stdout: `{"path_with_namespace":"acme/widget","web_url":"https://gitlab.example/acme/widget"}`},
			},
			wantCalls: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outputDir := isolatedReviewOutput(t)
			t.Cleanup(session.SetProbeDaemonFocusFnForTest(func() *session.Focus { return tc.daemonFocus }))
			identity := savePushReview(t, outputDir, session.CritJSON{ReviewRound: 1, Files: map[string]session.CritJSONFile{}})
			cj, err := review.LoadCritJSON(identity)
			if err != nil {
				t.Fatal(err)
			}
			discussions, err := json.Marshal([]gitlabDiscussion{{ID: "discussion-1", Notes: []gitlabNote{{
				ID: 101, Body: "remote note", Position: &gitlabPosition{PositionType: "text", HeadSHA: "head", NewPath: "main.go", NewLine: 3},
			}}}})
			if err != nil {
				t.Fatal(err)
			}
			calls := stubCommands(t, append([]commandResponse{{stdout: string(discussions)}}, tc.responses...)...)

			repo := forge.RepoContext{Host: "gitlab.example"}
			id := forge.ChangeID{Number: 7, Host: "gitlab.example"}
			saved := &pushReview{path: identity, base: session.CloneCritJSON(cj)}
			if err := refreshGitLabIDs(context.Background(), repo, id, mrRaw{TargetProjectID: 99}, saved, &cj); err != nil {
				t.Fatal(err)
			}
			if len(*calls) != tc.wantCalls {
				t.Fatalf("calls = %d, want %d: %+v", len(*calls), tc.wantCalls, *calls)
			}
			if tc.wantCalls == 2 && !strings.HasSuffix((*calls)[1].args[len((*calls)[1].args)-1], "projects/99") {
				t.Fatalf("project lookup = %v", (*calls)[1].args)
			}
			comments := cj.Files["main.go"].Comments
			if len(comments) != 1 {
				t.Fatalf("imported comments = %+v", comments)
			}
			want := session.StampWithFocus(session.Comment{}, *daemonFocus)
			if got := comments[0]; got.FocusKey != want.FocusKey || got.DiffScope != want.DiffScope {
				t.Fatalf("imported note stamped (key %q, scope %q), daemon shows (key %q, scope %q)",
					got.FocusKey, got.DiffScope, want.FocusKey, want.DiffScope)
			}
		})
	}
}

package gitlab

import (
	"context"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

// Full SHAs pass through vcs.ResolveCommitSHA unresolved: the tests run in a
// temp directory that is not a git repository.
var (
	pinnedSHA  = strings.Repeat("a", 40)
	currentSHA = strings.Repeat("c", 40)
)

func mrWithHead(head string) string {
	return `{"diff_refs":{"base_sha":"base","start_sha":"start","head_sha":"` + head + `"}}`
}

func pinnedPushRequest(outputDir, event string) forge.PushRequest {
	return forge.PushRequest{
		Repo:       forge.RepoContext{Host: "gitlab.example", Project: "acme/widget"},
		ChangeSpec: "7", OutputDir: outputDir, Event: event, Commit: pinnedSHA,
	}
}

func savePinnedReview(t *testing.T, outputDir string) {
	t.Helper()
	savePushReview(t, outputDir, session.CritJSON{ReviewRound: 1, Files: map[string]session.CritJSONFile{
		"main.go": {Comments: []session.Comment{{ID: "c_local", StartLine: 3, EndLine: 3, Side: "new", Body: "fix this", HeadSHA: pinnedSHA}}},
	}})
}

func TestParseGitLabPushFlagsCommit(t *testing.T) {
	flags, err := parsePushFlags([]string{"--commit", "HEAD", "7"})
	if err != nil {
		t.Fatal(err)
	}
	if flags.commit != "HEAD" || flags.spec != "7" {
		t.Fatalf("flags = %+v", flags)
	}
	if _, err := parsePushFlags([]string{"--commit"}); err == nil {
		t.Fatal("--commit without a value unexpectedly accepted")
	}
}

// When the pinned commit is an older MR version, diff notes are positioned
// against that version's refs, not the MR's current diff_refs.
func TestRunPushPinnedCommitUsesMatchingVersionRefs(t *testing.T) {
	outputDir := isolatedReviewOutput(t)
	savePinnedReview(t, outputDir)
	versions := `[{"head_commit_sha":"` + currentSHA + `","base_commit_sha":"base2","start_commit_sha":"start2"},` +
		`{"head_commit_sha":"` + pinnedSHA + `","base_commit_sha":"base1","start_commit_sha":"start1"}]`
	calls := stubCommands(t,
		commandResponse{},
		commandResponse{stdout: mrWithHead(currentSHA)},
		commandResponse{stdout: versions},
		commandResponse{stdout: `[]`},
		commandResponse{stdout: `{"id":55}`, checkStdin: true,
			wantStdin: `{"note":"fix this\n\n\u003c!-- crit-local-id:c_local --\u003e","position":{"base_sha":"base1","head_sha":"` + pinnedSHA + `","new_line":3,"new_path":"main.go","old_path":"main.go","position_type":"text","start_sha":"start1"}}`},
		commandResponse{stdout: `{}`, wantStdin: `{"reviewer_state":"reviewed"}`, checkStdin: true},
		commandResponse{stdout: `[]`},
	)
	result, err := runPush(context.Background(), pinnedPushRequest(outputDir, "comment"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 1 || len(*calls) != 7 {
		t.Fatalf("result = %+v, calls = %d", result, len(*calls))
	}
	if got := (*calls)[2].args; !strings.Contains(strings.Join(got, " "), "merge_requests/7/versions?per_page=100&page=1") {
		t.Fatalf("versions call args = %q", got)
	}
}

// Every failure here must happen before any draft note is staged, edited,
// deleted, or published.
func TestRunPushPinnedCommitFailsBeforeStaging(t *testing.T) {
	tests := []struct {
		name      string
		event     string
		responses []commandResponse
		wantErr   string
	}{
		{
			name:  "commit is not an MR version",
			event: "comment",
			responses: []commandResponse{
				{},
				{stdout: mrWithHead(currentSHA)},
				{stdout: `[{"head_commit_sha":"` + currentSHA + `","base_commit_sha":"b","start_commit_sha":"s"}]`},
			},
			wantErr: "commit aaaaaaa is not a version of MR !7",
		},
		{
			name:  "approve when the MR head moved",
			event: "approve",
			responses: []commandResponse{
				{},
				{stdout: mrWithHead(currentSHA)},
			},
			wantErr: "MR !7 head moved past aaaaaaa (now ccccccc); approval not submitted",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir := isolatedReviewOutput(t)
			savePinnedReview(t, outputDir)
			calls := stubCommands(t, tt.responses...)
			_, err := runPush(context.Background(), pinnedPushRequest(outputDir, tt.event))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
			if len(*calls) != len(tt.responses) {
				t.Fatalf("calls = %d, want %d (nothing after the check): %q", len(*calls), len(tt.responses), *calls)
			}
		})
	}
}

func TestRunPushPinnedCommitApprovesWithPinnedSHA(t *testing.T) {
	approveResponse := func(r commandResponse) []commandResponse {
		return []commandResponse{
			{},
			{stdout: mrWithHead(pinnedSHA)},
			{stdout: `[]`},
			{stdout: `{"id":55}`},
			{stdout: `{}`, wantStdin: `{"reviewer_state":"reviewed"}`, checkStdin: true},
			{stdout: `[]`},
			r,
		}
	}
	tests := []struct {
		name     string
		approve  commandResponse
		wantErr  string
		wantCall int
	}{
		{
			name:    "approved",
			approve: commandResponse{stdout: `{}`, wantStdin: `{"sha":"` + pinnedSHA + `"}`, checkStdin: true},
		},
		{
			name:    "head moved during publish",
			approve: commandResponse{stderr: "glab: 409 Conflict (HTTP 409)", exitCode: 1},
			wantErr: "comments published, but MR !7 head moved past aaaaaaa; approval not submitted",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir := isolatedReviewOutput(t)
			savePinnedReview(t, outputDir)
			calls := stubCommands(t, approveResponse(tt.approve)...)
			result, err := runPush(context.Background(), pinnedPushRequest(outputDir, "approve"))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Created != 1 || len(*calls) != 7 {
				t.Fatalf("result = %+v, calls = %d", result, len(*calls))
			}
			if got := strings.Join((*calls)[6].args, " "); !strings.Contains(got, "merge_requests/7/approve") {
				t.Fatalf("last call = %q, want approve", got)
			}
		})
	}
}

func TestRunPushRejectsUnresolvableCommit(t *testing.T) {
	outputDir := isolatedReviewOutput(t)
	savePinnedReview(t, outputDir)
	calls := stubCommands(t)
	request := pinnedPushRequest(outputDir, "comment")
	request.Commit = "not-a-commit"
	_, err := runPush(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), `--commit: cannot resolve "not-a-commit"`) {
		t.Fatalf("error = %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("calls = %d, want none", len(*calls))
	}
}

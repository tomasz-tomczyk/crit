package gitlab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

func savePushReview(t *testing.T, outputDir string, cj session.CritJSON) string {
	t.Helper()
	identity, err := review.ResolveReviewPath(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := review.SaveCritJSON(identity, cj); err != nil {
		t.Fatal(err)
	}
	return identity
}

func TestParseGitLabPushFlags(t *testing.T) {
	flags, err := parsePushFlags([]string{"--dry-run", "-e", "request-changes", "-m", "fix it", "-o", "reviews", "7"})
	if err != nil {
		t.Fatal(err)
	}
	if !flags.dryRun || flags.event != "request-changes" || flags.message != "fix it" || flags.outputDir != "reviews" || flags.spec != "7" {
		t.Fatalf("flags = %+v", flags)
	}
	for _, args := range [][]string{{"--message"}, {"--output"}, {"--event"}, {"--event", "bad"}, {"--event", "request-changes", "7"}} {
		if _, err := parsePushFlags(args); err == nil {
			t.Errorf("parsePushFlags(%v) unexpectedly succeeded", args)
		}
	}
	_, err = parsePushFlags([]string{"1", "2"})
	var exitErr clicmd.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("duplicate specs error = %v", err)
	}
}

func TestRunPushDryRun(t *testing.T) {
	outputDir := isolatedReviewOutput(t)
	savePushReview(t, outputDir, session.CritJSON{Files: map[string]session.CritJSONFile{
		"main.go": {Comments: []session.Comment{{ID: "c_1", StartLine: 3, EndLine: 3, Body: "fix", HeadSHA: "head"}}},
	}})
	stubCommands(t,
		commandResponse{},
		commandResponse{stdout: `{"diff_refs":{"base_sha":"base","start_sha":"start","head_sha":"head"}}`},
	)
	result, err := runPush(context.Background(), forge.PushRequest{
		Repo: forge.RepoContext{Host: "gitlab.example", Project: "acme/widget"}, ChangeSpec: "7", OutputDir: outputDir, DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 0 || result.Edited != 0 || result.Deleted != 0 || result.Replied != 0 || result.Resolved != 0 || len(result.Warnings) != 0 {
		t.Fatalf("dry-run result = %+v", result)
	}
}

func TestRunPushNoOp(t *testing.T) {
	outputDir := isolatedReviewOutput(t)
	savePushReview(t, outputDir, session.CritJSON{Files: map[string]session.CritJSONFile{}})
	calls := stubCommands(t,
		commandResponse{},
		commandResponse{stdout: `{"diff_refs":{"base_sha":"base","start_sha":"start","head_sha":"head"}}`},
		commandResponse{stdout: `[]`},
	)
	result, err := runPush(context.Background(), forge.PushRequest{
		Repo: forge.RepoContext{Host: "gitlab.example", Project: "acme/widget"}, ChangeSpec: "7", OutputDir: outputDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 0 || result.Edited != 0 || result.Deleted != 0 || result.Replied != 0 || result.Resolved != 0 || len(result.Warnings) != 0 || len(*calls) != 3 {
		t.Fatalf("result = %+v, calls = %d", result, len(*calls))
	}
}

func TestRunPushPublishesDraftAndRefreshesRemoteIDs(t *testing.T) {
	outputDir := isolatedReviewOutput(t)
	identity := savePushReview(t, outputDir, session.CritJSON{ReviewRound: 1, Files: map[string]session.CritJSONFile{
		"main.go": {Comments: []session.Comment{{ID: "c_local", StartLine: 3, EndLine: 3, Side: "new", Body: "fix this", HeadSHA: "head"}}},
	}})
	remoteBody := withLocalMarker("fix this", "c_local")
	discussions, err := json.Marshal([]gitlabDiscussion{{ID: "discussion-1", Notes: []gitlabNote{{
		ID: 101, Body: remoteBody, Position: &gitlabPosition{PositionType: "text", BaseSHA: "base", StartSHA: "start", HeadSHA: "head", NewPath: "main.go", NewLine: 3},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	calls := stubCommands(t,
		commandResponse{},
		commandResponse{stdout: `{"diff_refs":{"base_sha":"base","start_sha":"start","head_sha":"head"}}`},
		commandResponse{stdout: `[]`},
		commandResponse{stdout: `{"id":55}`, wantStdin: `{"note":"fix this\n\n\u003c!-- crit-local-id:c_local --\u003e","position":{"base_sha":"base","head_sha":"head","new_line":3,"new_path":"main.go","old_path":"main.go","position_type":"text","start_sha":"start"}}`, checkStdin: true},
		commandResponse{stdout: `{}`, wantStdin: `{"reviewer_state":"reviewed"}`, checkStdin: true},
		commandResponse{stdout: string(discussions)},
	)
	result, err := runPush(context.Background(), forge.PushRequest{
		Repo: forge.RepoContext{Host: "gitlab.example", Project: "acme/widget"}, ChangeSpec: "7", OutputDir: outputDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 1 || len(*calls) != 6 {
		t.Fatalf("result = %+v, calls = %d", result, len(*calls))
	}
	cj, err := review.LoadCritJSON(identity)
	if err != nil {
		t.Fatal(err)
	}
	comment := cj.Files["main.go"].Comments[0]
	if comment.GitLabNoteID != 101 || comment.GitLabDiscussionID != "discussion-1" {
		t.Fatalf("refreshed comment = %+v", comment)
	}
}

func TestStageDraftNotesCreatesCommentsAndReplies(t *testing.T) {
	refs := diffRefs{BaseSHA: "base", StartSHA: "start", HeadSHA: "head"}
	candidates := []pushCandidate{
		{Path: "main.go", CommentID: "c_1", Body: "root", StartLine: 2, EndLine: 2},
		{CommentID: "c_1", ReplyID: "rp_1", Body: "reply", DiscussionID: "d1"},
	}
	stubCommands(t,
		commandResponse{stdout: `{"id":1}`},
		commandResponse{stdout: `{"id":2}`},
	)
	created, replied, err := stageDraftNotes(context.Background(), forge.RepoContext{Host: "gitlab.example"}, forge.ChangeID{Number: 1}, refs, candidates, nil)
	if err != nil || created != 1 || replied != 1 {
		t.Fatalf("stage result = (%d, %d, %v)", created, replied, err)
	}
}

func TestStageDraftNotesSkipsMatchingAndReportsPartialFailure(t *testing.T) {
	candidate := pushCandidate{CommentID: "c_1", Body: "root", Path: "a.go", EndLine: 1}
	existing := []draftNoteRaw{{ID: 1, Note: withLocalMarker("root", "c_1")}}
	calls := stubCommands(t)
	created, replied, err := stageDraftNotes(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 1}, diffRefs{}, []pushCandidate{candidate}, existing)
	if err != nil || created != 0 || replied != 0 || len(*calls) != 0 {
		t.Fatalf("matching stage = (%d, %d, %v), calls=%d", created, replied, err, len(*calls))
	}

	stubCommands(t, commandResponse{}, commandResponse{stderr: "rejected", exitCode: 1})
	created, replied, err = stageDraftNotes(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 1}, diffRefs{}, []pushCandidate{
		{CommentID: "c_2", Body: "one", Path: "a.go", EndLine: 2},
		{ReplyID: "rp_2", Body: "two", DiscussionID: "d1"},
	}, nil)
	if err == nil || created != 1 || replied != 0 || !strings.Contains(err.Error(), "staging comment rp_2") {
		t.Fatalf("partial stage = (%d, %d, %v)", created, replied, err)
	}
}

func TestMatchingDraftFallsBackToReplyAndPositionIdentity(t *testing.T) {
	reply := pushCandidate{ReplyID: "rp_1", Body: "same", DiscussionID: "d1"}
	if !matchingDraft(reply, []draftNoteRaw{{Note: "same", DiscussionID: "d1"}}) {
		t.Fatal("reply draft was not matched by discussion and body")
	}
	if matchingDraft(reply, []draftNoteRaw{{Note: "same", DiscussionID: "other"}}) {
		t.Fatal("reply draft matched the wrong discussion")
	}
	position := &gitlabPosition{PositionType: "text", NewPath: "a.go", NewLine: 4}
	comment := pushCandidate{CommentID: "c_1", Body: "same", Path: "a.go", StartLine: 4, EndLine: 4, Side: "RIGHT"}
	if !matchingDraft(comment, []draftNoteRaw{{Note: "same", Position: position}}) {
		t.Fatal("comment draft was not matched by position and body")
	}
	if matchingDraft(comment, []draftNoteRaw{{Note: "different", Position: position}, {Note: "same"}}) {
		t.Fatal("comment draft matched different content or an unpositioned note")
	}
}

func TestPushGitLabEdits(t *testing.T) {
	cj := session.CritJSON{Files: map[string]session.CritJSONFile{"a.go": {Comments: []session.Comment{{
		Body: "edited root", GitLabNoteID: 10, GitLabDiscussionID: "d1", LastPushedBodyHash: bodyHash("old root"),
		Replies: []session.Reply{{Body: "edited reply", GitLabNoteID: 11, GitLabDiscussionID: "d1", LastPushedBodyHash: bodyHash("old reply")}},
	}}}}}
	calls := stubCommands(t,
		commandResponse{wantStdin: `{"body":"edited root"}`, checkStdin: true},
		commandResponse{wantStdin: `{"body":"edited reply"}`, checkStdin: true},
	)
	edited, err := pushGitLabEdits(context.Background(), forge.RepoContext{Host: "gitlab.example"}, forge.ChangeID{Number: 7}, &cj)
	if err != nil || edited != 2 || len(*calls) != 2 {
		t.Fatalf("edit result = (%d, %v), calls=%d", edited, err, len(*calls))
	}
	comment := cj.Files["a.go"].Comments[0]
	if comment.LastPushedBodyHash != bodyHash("edited root") || comment.Replies[0].LastPushedBodyHash != bodyHash("edited reply") {
		t.Fatalf("hashes not updated: %+v", comment)
	}
}

func TestSyncGitLabResolutionDeduplicatesDiscussion(t *testing.T) {
	remoteResolved := false
	cj := session.CritJSON{Files: map[string]session.CritJSONFile{
		"a.go": {Comments: []session.Comment{{Resolved: true, GitLabDiscussionID: "d1", GitLabResolved: &remoteResolved}}},
		"b.go": {Comments: []session.Comment{{Resolved: true, GitLabDiscussionID: "d1", GitLabResolved: &remoteResolved}}},
	}}
	calls := stubCommands(t, commandResponse{wantStdin: `{"resolved":true}`, checkStdin: true})
	count, err := syncGitLabResolution(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 2}, &cj)
	if err != nil || count != 1 || len(*calls) != 1 {
		t.Fatalf("resolution = (%d, %v), calls=%d", count, err, len(*calls))
	}
}

func TestPushGitLabDeletesRemovesRepliesBeforeRoot(t *testing.T) {
	cj := session.CritJSON{PendingRemoteDeletes: []session.RemoteRef{{Forge: "gitlab", CommentID: 10, ThreadID: "d1"}}}
	discussion := gitlabDiscussion{ID: "d1", Notes: []gitlabNote{
		{ID: 10, Position: &gitlabPosition{PositionType: "text", NewPath: "a.go", NewLine: 1}},
		{ID: 11},
	}}
	raw, _ := json.Marshal(discussion)
	calls := stubCommands(t, commandResponse{stdout: string(raw)}, commandResponse{}, commandResponse{})
	deleted, err := pushGitLabDeletes(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 2}, &cj)
	if err != nil || deleted != 1 || len(session.RemoteDeletesFor(cj, forge.GitLab)) != 0 {
		t.Fatalf("delete result = (%d, %v), queue=%+v", deleted, err, cj.PendingRemoteDeletes)
	}
	if !strings.HasSuffix((*calls)[1].args[len((*calls)[1].args)-1], "/notes/11") || !strings.HasSuffix((*calls)[2].args[len((*calls)[2].args)-1], "/notes/10") {
		t.Fatalf("delete order = %+v", *calls)
	}
}

func TestPushGitLabDeletesKeepsFailedQueue(t *testing.T) {
	cj := session.CritJSON{PendingRemoteDeletes: []session.RemoteRef{{Forge: "gitlab", CommentID: 10, ThreadID: "d1"}}}
	stubCommands(t, commandResponse{stderr: "gone", exitCode: 1})
	deleted, err := pushGitLabDeletes(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 2}, &cj)
	if err == nil || deleted != 0 || len(session.RemoteDeletesFor(cj, forge.GitLab)) != 1 {
		t.Fatalf("failed delete = (%d, %v), queue=%+v", deleted, err, cj.PendingRemoteDeletes)
	}
}

func TestFetchMRRawAndDraftNotesValidation(t *testing.T) {
	t.Run("MR success", func(t *testing.T) {
		stubCommands(t, commandResponse{stdout: `{"iid":3,"diff_refs":{"base_sha":"b","start_sha":"s","head_sha":"h"}}`})
		mr, err := fetchMRRaw(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 3})
		if err != nil || mr.IID != 3 || mr.DiffRefs.HeadSHA != "h" {
			t.Fatalf("MR = (%+v, %v)", mr, err)
		}
	})
	t.Run("MR missing refs", func(t *testing.T) {
		stubCommands(t, commandResponse{stdout: `{"iid":3}`})
		if _, err := fetchMRRaw(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 3}); err == nil || !strings.Contains(err.Error(), "diff is not ready") {
			t.Fatalf("missing refs error = %v", err)
		}
	})
	t.Run("draft decode", func(t *testing.T) {
		stubCommands(t, commandResponse{stdout: "{"})
		if _, err := fetchDraftNotes(context.Background(), forge.RepoContext{}, forge.ChangeID{Number: 3}); err == nil || !strings.Contains(err.Error(), "parsing GitLab draft notes") {
			t.Fatalf("draft decode error = %v", err)
		}
	})
}

func TestPushHelpers(t *testing.T) {
	if reviewerState("request-changes") != "requested_changes" || reviewerState("approve") != "reviewed" {
		t.Fatal("reviewerState mapping is wrong")
	}
	if candidateLocalID(pushCandidate{CommentID: "c", ReplyID: "rp"}) != "rp" || candidateLocalID(pushCandidate{CommentID: "c"}) != "c" {
		t.Fatal("candidateLocalID mapping is wrong")
	}
	if normalizedSide("LEFT") != "old" || normalizedSide("right") != "new" || lineForSide(3, "old") != "3_0" {
		t.Fatal("side helpers returned unexpected values")
	}
	position := buildPosition(diffRefs{}, pushCandidate{Path: "a.go", StartLine: 4, EndLine: 5, Side: "old"})
	if position["old_line"] != 5 || position["new_line"] != nil {
		t.Fatalf("old position = %+v", position)
	}
	if body, id := splitLocalMarker("body\n<!-- crit-local-id: -->"); body == "" || id != "" {
		t.Fatalf("empty marker = (%q, %q)", body, id)
	}
}

func TestLoadPushReviewErrors(t *testing.T) {
	outputDir := isolatedReviewOutput(t)
	if _, _, err := loadPushReview(outputDir); err == nil || !strings.Contains(err.Error(), "no review file found") {
		t.Fatalf("missing review error = %v", err)
	}
	identity, err := review.ResolveReviewPath(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	path := session.ReviewPathsFor(identity).Review
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadPushReview(outputDir); err == nil || !strings.Contains(err.Error(), "invalid review file") {
		t.Fatalf("invalid review error = %v", err)
	}
}

func TestSavePartialGitLabPushJoinsPersistenceFailure(t *testing.T) {
	operationErr := fmt.Errorf("operation failed")
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := savePartialGitLabPush(filepath.Join(blocked, "review"), session.CritJSON{}, operationErr)
	if err == nil || !errors.Is(err, operationErr) || !strings.Contains(err.Error(), "persisting successful GitLab operations") {
		t.Fatalf("joined error = %v", err)
	}
}

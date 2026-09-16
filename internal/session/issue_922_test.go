package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIssue922_CarryForwardPreservesCommentIDs(t *testing.T) {
	anchor := &DOMAnchor{Pathname: "/", CSSSelector: "#submit"}
	s := &Session{Files: []*FileEntry{{
		Path:            "main.go",
		Content:         "line one\nline two\n",
		PreviousContent: "line one\nline two\n",
		PreviousComments: []Comment{
			{ID: "c_line", StartLine: 2, EndLine: 2, Scope: "line"},
			{ID: "c_file", Scope: "file"},
			{ID: "c_old", StartLine: 1, EndLine: 1, Scope: "line", Side: "old"},
			{ID: "c_pin", DOMAnchor: anchor, PinNumber: 1},
		},
	}}}

	s.carryForwardComments()

	comments := s.Files[0].Comments
	if len(comments) != 4 {
		t.Fatalf("carried comments = %d, want 4", len(comments))
	}
	wantIDs := []string{"c_line", "c_file", "c_old", "c_pin"}
	for i, want := range wantIDs {
		if comments[i].ID != want {
			t.Errorf("comment %d ID = %q, want %q", i, comments[i].ID, want)
		}
	}
}

func TestIssue922_ReplyByOriginalIDAcrossTwoRounds(t *testing.T) {
	s, identity := newIssue922RoundSession(t)
	const commentID = "c_stable"

	for wantRound := 2; wantRound <= 3; wantRound++ {
		s.SignalRoundComplete()
		<-s.roundComplete
		s.handleRoundCompleteFiles()

		comments := s.GetComments("plan.md")
		if len(comments) != 1 {
			t.Fatalf("round %d comments = %d, want 1", wantRound, len(comments))
		}
		if comments[0].ID != commentID {
			t.Fatalf("round %d comment ID = %q, want %q", wantRound, comments[0].ID, commentID)
		}

		reply, ok := s.AddReply("plan.md", commentID, "reply", "agent", "")
		if !ok {
			t.Fatalf("round %d: AddReply could not find original comment ID %q", wantRound, commentID)
		}
		if reply.ID == "" {
			t.Fatalf("round %d: AddReply returned an empty reply ID", wantRound)
		}
		if err := s.SyncWriteFiles(); err != nil {
			t.Fatalf("round %d SyncWriteFiles: %v", wantRound, err)
		}
	}

	disk, err := readCritJSONFromDisk(identity)
	if err != nil {
		t.Fatalf("read review.json: %v", err)
	}
	comments := disk.Files["plan.md"].Comments
	if len(comments) != 1 {
		t.Fatalf("disk comments after two rounds = %d, want 1", len(comments))
	}
	if comments[0].ID != commentID {
		t.Errorf("disk comment ID = %q, want %q", comments[0].ID, commentID)
	}
	if len(comments[0].Replies) != 2 {
		t.Errorf("disk replies = %d, want 2", len(comments[0].Replies))
	}
}

func TestIssue922_RoundBumpSaveLoadHasNoDuplicate(t *testing.T) {
	s, identity := newIssue922RoundSession(t)

	s.SignalRoundComplete()
	<-s.roundComplete
	s.handleRoundCompleteFiles()

	disk, err := readCritJSONFromDisk(identity)
	if err != nil {
		t.Fatalf("read review.json: %v", err)
	}
	comments := disk.Files["plan.md"].Comments
	if len(comments) != 1 {
		t.Fatalf("disk comments = %d, want 1", len(comments))
	}
	if comments[0].ID != "c_stable" {
		t.Errorf("disk comment ID = %q, want c_stable", comments[0].ID)
	}

	resumed := &Session{
		RepoRoot:       s.RepoRoot,
		ReviewFilePath: identity,
		Files: []*FileEntry{{
			Path:     "plan.md",
			AbsPath:  s.Files[0].AbsPath,
			Status:   "modified",
			FileType: "markdown",
			Content:  s.Files[0].Content,
		}},
	}
	resumed.loadCritJSON()
	loaded := resumed.GetComments("plan.md")
	if len(loaded) != 1 {
		t.Fatalf("loaded comments = %d, want 1", len(loaded))
	}
	if loaded[0].ID != "c_stable" {
		t.Errorf("loaded comment ID = %q, want c_stable", loaded[0].ID)
	}
}

func TestIssue922_DeletedCommentStaysDeletedAcrossRoundBump(t *testing.T) {
	s, identity := newIssue922RoundSession(t)

	if !s.DeleteComment("plan.md", "c_stable") {
		t.Fatal("DeleteComment returned false")
	}
	// The on-disk copy still exists here. The genuine-delete tombstone must
	// keep the merge in SyncWriteFiles from resurrecting it.
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatalf("SyncWriteFiles after delete: %v", err)
	}

	s.SignalRoundComplete()
	<-s.roundComplete
	s.handleRoundCompleteFiles()

	if comments := s.GetComments("plan.md"); len(comments) != 0 {
		t.Fatalf("memory comments after delete and round bump = %d, want 0", len(comments))
	}
	disk, err := readCritJSONFromDisk(identity)
	if err != nil {
		t.Fatalf("read review.json: %v", err)
	}
	if file, ok := disk.Files["plan.md"]; ok && len(file.Comments) != 0 {
		t.Fatalf("disk comments after delete and round bump = %d, want 0", len(file.Comments))
	}
}

func newIssue922RoundSession(t *testing.T) (*Session, string) {
	t.Helper()
	dir := t.TempDir()
	identity := filepath.Join(dir, "review")
	path := filepath.Join(dir, "plan.md")
	const content = "# Plan\n\nDo the thing\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	comment := Comment{
		ID:          "c_stable",
		StartLine:   3,
		EndLine:     3,
		Scope:       "line",
		Body:        "Please revise this",
		Anchor:      "Do the thing",
		ReviewRound: 1,
	}
	if err := saveCritJSONToDisk(identity, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {Status: "modified", Comments: []Comment{comment}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	s := &Session{
		Mode:           "files",
		RepoRoot:       dir,
		ReviewFilePath: identity,
		ReviewRound:    1,
		Files: []*FileEntry{{
			Path:            "plan.md",
			AbsPath:         path,
			Status:          "modified",
			FileType:        "markdown",
			Content:         content,
			PreviousContent: content,
			Comments:        []Comment{comment},
		}},
	}
	s.InitTestChannels()
	t.Cleanup(func() { quiesceSession(t, s) })
	return s, identity
}

package session

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestWriteFilesWaitsForInFlightSave(t *testing.T) {
	s := &Session{RepoRoot: t.TempDir()}
	comment := s.AddReviewComment("delete me", "", "")
	s.WriteFiles()
	// Model a background save holding writeMu. A direct flush must wait for it.
	s.writeMu.Lock()
	done := make(chan struct{})
	go func() { s.WriteFiles(); close(done) }()
	premature := false
	select {
	case <-done:
		premature = true
	case <-time.After(100 * time.Millisecond):
	}
	s.DeleteReviewComment(comment.ID)
	s.writeMu.Unlock()
	<-done
	quiesceSession(t, s)
	if premature {
		t.Fatal("WriteFiles bypassed the in-flight save")
	}
	if _, err := os.Stat(ReviewPathsFor(s.critJSONPath()).Review); !os.IsNotExist(err) {
		t.Fatalf("deleted comment's review file remains: %v", err)
	}
}

// An empty flush with no review folder must still clear pendingWrite, or the
// watcher ignores review.json written by another process (crit comment).
func TestEmptyWriteWithoutFolderClearsPendingWrite(t *testing.T) {
	s := &Session{RepoRoot: t.TempDir()}
	comment := s.AddReviewComment("gone", "", "")
	s.DeleteReviewComment(comment.ID)
	s.WriteFiles()
	quiesceSession(t, s)
	if s.PendingWriteForTest() {
		t.Fatal("pendingWrite still set after empty write")
	}
	critPath := s.critJSONPath()
	if _, err := os.Stat(critPath); !os.IsNotExist(err) {
		t.Fatalf("empty write created the review folder: %v", err)
	}
	external := CritJSON{ReviewComments: []Comment{{ID: "r_ext", Body: "external"}}}
	if err := SaveCritJSON(critPath, external); err != nil {
		t.Fatal(err)
	}
	if !s.MergeExternalCritJSONForTest() {
		t.Fatal("external review comment was not merged")
	}
}

// A write after the review folder was deleted externally clears the session
// without recreating the folder for the lock file.
func TestWriteAfterExternalDeleteKeepsFolderDeleted(t *testing.T) {
	s := &Session{RepoRoot: t.TempDir()}
	s.AddReviewComment("x", "", "")
	s.WriteFiles()
	critPath := s.critJSONPath()
	if err := os.RemoveAll(critPath); err != nil {
		t.Fatal(err)
	}
	s.AddReviewComment("y", "", "")
	s.WriteFiles()
	quiesceSession(t, s)
	if _, err := os.Stat(critPath); !os.IsNotExist(err) {
		t.Fatalf("review folder recreated after external delete: %v", err)
	}
}

// Deleted replies and review comments stay deleted although disk still has
// them when the next write merges disk-only content.
func TestDeletedRepliesAndReviewCommentsNotReAddedFromDisk(t *testing.T) {
	s := &Session{RepoRoot: t.TempDir(), Files: []*FileEntry{{Path: "plan.md"}}}
	c, _ := s.AddComment("plan.md", 1, 1, "", "file comment", "", "", "")
	fileReply, _ := s.AddReply("plan.md", c.ID, "file reply", "", "")
	rc := s.AddReviewComment("review comment", "", "")
	reviewReply, _ := s.AddReviewCommentReply(rc.ID, "review reply", "", "")
	keep := s.AddReviewComment("kept", "", "")
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}

	s.DeleteReply("plan.md", c.ID, fileReply.ID)
	s.DeleteReviewCommentReply(rc.ID, reviewReply.ID)
	s.DeleteReviewComment(rc.ID)
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}
	quiesceSession(t, s)

	data, err := os.ReadFile(ReviewPathsFor(s.critJSONPath()).Review)
	if err != nil {
		t.Fatal(err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatal(err)
	}
	if len(cj.ReviewComments) != 1 || cj.ReviewComments[0].ID != keep.ID {
		t.Fatalf("review comments = %+v", cj.ReviewComments)
	}
	if replies := cj.Files["plan.md"].Comments[0].Replies; len(replies) != 0 {
		t.Fatalf("file replies = %+v", replies)
	}
}

// A change scheduled while a save runs is not on disk yet, so the save must
// leave pendingWrite set for the watcher's guard.
func TestSaveKeepsPendingWriteForLaterChange(t *testing.T) {
	s := &Session{RepoRoot: t.TempDir()}
	s.mu.Lock()
	s.scheduleWrite()
	snap := writeFilesSnapshot{writeSeq: s.writeSeq}
	s.scheduleWrite() // a delete lands after the snapshot
	s.clearPendingWrite(snap)
	pending := s.pendingWrite
	s.mu.Unlock()
	quiesceSession(t, s)
	if !pending {
		t.Fatal("pendingWrite cleared although a later change was not saved")
	}
}

// The watcher's round guard must clear the reload flag, or every tick
// re-reads the file.
func TestRoundGuardClearsUnloadedFlag(t *testing.T) {
	s := &Session{RepoRoot: t.TempDir(), ReviewRound: 3}
	critPath := s.critJSONPath()
	if err := SaveCritJSON(critPath, CritJSON{ReviewRound: 2, ReviewComments: []Comment{{ID: "r1"}}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(ReviewPathsFor(critPath).Review)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.lastCritJSONMtime = info.ModTime()
	s.diskHasUnloadedComments = true
	s.mu.Unlock()
	s.MergeExternalCritJSONForTest()
	s.mu.RLock()
	stuck := s.diskHasUnloadedComments
	s.mu.RUnlock()
	if stuck {
		t.Fatal("reload flag still set after the round guard skipped the reload")
	}
}

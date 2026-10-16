package session

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// cliAddReply adds a reply to the review file the way `crit comment
// --reply-to` does while a daemon runs: straight to disk, behind its back.
func cliAddReply(t *testing.T, s *Session, commentID, replyID, body string) {
	t.Helper()
	path := ReviewPathsFor(s.critJSONPath()).Review
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatal(err)
	}
	file := cj.Files["plan.md"]
	for i := range file.Comments {
		if file.Comments[i].ID == commentID {
			file.Comments[i].Replies = append(file.Comments[i].Replies, Reply{
				ID: replyID, Body: body, Author: "Claude Code",
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
			})
		}
	}
	cj.Files["plan.md"] = file
	out, _ := json.MarshalIndent(cj, "", "  ")
	if err := os.WriteFile(path, out, 0644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

func diskReplyIDs(t *testing.T, s *Session, commentID string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(ReviewPathsFor(s.critJSONPath()).Review)
	if err != nil {
		t.Fatal(err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, c := range cj.Files["plan.md"].Comments {
		if c.ID == commentID {
			for _, r := range c.Replies {
				ids[r.ID] = true
			}
		}
	}
	return ids
}

func TestDaemonWrite_KeepsReplyTheCLIAddedWhileAWriteWasPending(t *testing.T) {
	// GIVEN - a written comment, then the agent's CLI reply lands on disk
	// while the reviewer's own edit waits in a pending daemon write
	s := newTestSession(t)
	c, _ := s.AddComment("plan.md", 1, 1, "", "make it readable", "", "Ondra", "")
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}
	cliAddReply(t, s, c.ID, "r_cli", "Written as proposed.")
	s.AddComment("plan.md", 3, 3, "", "another note", "", "Ondra", "")
	s.mergeExternalCritJSON() // the watcher tick skips its merge: a write is pending

	// WHEN - the daemon writes its state
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}

	// THEN - the agent's reply is still in the review file
	if !diskReplyIDs(t, s, c.ID)["r_cli"] {
		t.Error("the daemon write dropped the reply the CLI had added")
	}
}

func TestDaemonWrite_DoesNotBringBackAReplyTheReviewerDeleted(t *testing.T) {
	// GIVEN - a written reply the reviewer then deletes, while the agent's
	// CLI adds another reply on disk
	s := newTestSession(t)
	c, _ := s.AddComment("plan.md", 1, 1, "", "make it readable", "", "Ondra", "")
	old, _ := s.AddReply("plan.md", c.ID, "typo, ignore", "Ondra", "")
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}
	// The deleted reply and the daemon's last write fall in the same second,
	// as they do on a fast machine.
	created, _ := time.Parse(time.RFC3339, old.CreatedAt)
	s.mu.Lock()
	s.lastCritJSONMtime = created.Add(500 * time.Millisecond)
	s.mu.Unlock()
	cliAddReply(t, s, c.ID, "r_cli", "Written as proposed.")
	s.DeleteReply("plan.md", c.ID, old.ID)

	// WHEN - the daemon writes its state
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}

	// THEN - the agent's reply stays and the deleted one does not come back
	ids := diskReplyIDs(t, s, c.ID)
	if !ids["r_cli"] {
		t.Error("the daemon write dropped the reply the CLI had added")
	}
	if ids[old.ID] {
		t.Error("the daemon write brought back the reply the reviewer deleted")
	}
}

func TestDaemonWrite_KeepsReviewCommentReplyTheCLIAdded(t *testing.T) {
	// GIVEN - a written review-level comment, then a CLI reply to it on disk
	// while another reviewer edit waits in a pending daemon write
	s := newTestSession(t)
	rc := s.AddReviewComment("overall: split the plan", "Ondra", "")
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}
	path := ReviewPathsFor(s.critJSONPath()).Review
	data, _ := os.ReadFile(path)
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatal(err)
	}
	cj.ReviewComments[0].Replies = append(cj.ReviewComments[0].Replies, Reply{
		ID: "r_cli", Body: "Split into two plans.", Author: "Claude Code",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	out, _ := json.MarshalIndent(cj, "", "  ")
	if err := os.WriteFile(path, out, 0644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Second)
	_ = os.Chtimes(path, later, later)
	s.AddReviewComment("another note", "Ondra", "")

	// WHEN - the daemon writes its state
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}

	// THEN - the reply is in memory and on disk
	got, _ := s.FindReviewCommentByID(rc.ID)
	if len(got.Replies) != 1 || got.Replies[0].ID != "r_cli" {
		t.Errorf("replies = %+v, want the CLI reply", got.Replies)
	}
}

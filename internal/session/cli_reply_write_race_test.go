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

// cliReplyAndSetResolved adds a reply the way `crit comment --reply-to` does
// and sets the parent's resolved state with it: --resolve resolves, a plain
// reply unresolves. It looks in file comments on plan.md and in review
// comments.
func cliReplyAndSetResolved(t *testing.T, s *Session, commentID, replyID string, resolved bool) {
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
	now := time.Now().Add(time.Second).UTC().Format(time.RFC3339)
	apply := func(c *Comment) {
		c.Replies = append(c.Replies, Reply{ID: replyID, Body: "done", Author: "Claude Code", CreatedAt: now})
		c.Resolved = resolved
		c.ResolvedRound = 0
		if resolved {
			c.ResolvedRound = cj.ReviewRound
		}
		c.UpdatedAt = now
	}
	file := cj.Files["plan.md"]
	for i := range file.Comments {
		if file.Comments[i].ID == commentID {
			apply(&file.Comments[i])
		}
	}
	if _, ok := cj.Files["plan.md"]; ok {
		cj.Files["plan.md"] = file
	}
	for i := range cj.ReviewComments {
		if cj.ReviewComments[i].ID == commentID {
			apply(&cj.ReviewComments[i])
		}
	}
	out, _ := json.MarshalIndent(cj, "", "  ")
	if err := os.WriteFile(path, out, 0644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

// diskCommentResolved reports the resolved flag of commentID in the review
// file, looking in plan.md comments and review comments.
func diskCommentResolved(t *testing.T, s *Session, commentID string) bool {
	t.Helper()
	data, err := os.ReadFile(ReviewPathsFor(s.critJSONPath()).Review)
	if err != nil {
		t.Fatal(err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatal(err)
	}
	for _, c := range cj.Files["plan.md"].Comments {
		if c.ID == commentID {
			return c.Resolved
		}
	}
	for _, c := range cj.ReviewComments {
		if c.ID == commentID {
			return c.Resolved
		}
	}
	t.Fatalf("comment %s not in the review file", commentID)
	return false
}

func TestDaemonWrite_KeepsResolveFromCLIReplyWhileAWriteWasPending(t *testing.T) {
	// GIVEN - a written comment, then `crit comment --reply-to --resolve`
	// lands on disk while another reviewer edit waits in a pending write
	s := newTestSession(t)
	c, _ := s.AddComment("plan.md", 1, 1, "", "make it readable", "", "Ondra", "")
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}
	cliReplyAndSetResolved(t, s, c.ID, "r_cli", true)
	s.AddComment("plan.md", 3, 3, "", "another note", "", "Ondra", "")
	s.mergeExternalCritJSON()

	// WHEN - the daemon writes its state
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}

	// THEN - the comment is still resolved on disk and in memory
	if !diskCommentResolved(t, s, c.ID) {
		t.Error("the daemon write undid the resolve the CLI reply set")
	}
	got, _, _ := s.FindCommentByID(c.ID, "plan.md")
	if !got.Resolved {
		t.Error("memory did not take the resolve from the CLI reply")
	}
}

func TestDaemonWrite_KeepsUnresolveFromCLIReplyWhileAWriteWasPending(t *testing.T) {
	// GIVEN - a written, resolved comment, then a plain CLI reply (which
	// unresolves) lands on disk while another edit waits in a pending write
	s := newTestSession(t)
	c, _ := s.AddComment("plan.md", 1, 1, "", "make it readable", "", "Ondra", "")
	s.SetCommentResolved("plan.md", c.ID, true)
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}
	cliReplyAndSetResolved(t, s, c.ID, "r_cli", false)
	s.AddComment("plan.md", 3, 3, "", "another note", "", "Ondra", "")
	s.mergeExternalCritJSON()

	// WHEN - the daemon writes its state
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}

	// THEN - the comment stays unresolved on disk
	if diskCommentResolved(t, s, c.ID) {
		t.Error("the daemon write put back the resolve the CLI reply cleared")
	}
}

func TestDaemonWrite_KeepsReviewCommentResolveFromCLIReply(t *testing.T) {
	// GIVEN - a written review comment, then a resolving CLI reply on disk
	// while another edit waits in a pending write
	s := newTestSession(t)
	rc := s.AddReviewComment("overall: split the plan", "Ondra", "")
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}
	cliReplyAndSetResolved(t, s, rc.ID, "r_cli", true)
	s.AddReviewComment("another note", "Ondra", "")

	// WHEN - the daemon writes its state
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}

	// THEN - the review comment is still resolved on disk
	if !diskCommentResolved(t, s, rc.ID) {
		t.Error("the daemon write undid the resolve on the review comment")
	}
}

func TestDaemonWrite_KeepsReviewCommentUnresolveFromCLIReply(t *testing.T) {
	// GIVEN - a written, resolved review comment, then a plain CLI reply on
	// disk while another edit waits in a pending write
	s := newTestSession(t)
	rc := s.AddReviewComment("overall: split the plan", "Ondra", "")
	s.ResolveReviewComment(rc.ID, true)
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}
	cliReplyAndSetResolved(t, s, rc.ID, "r_cli", false)
	s.AddReviewComment("another note", "Ondra", "")

	// WHEN - the daemon writes its state
	if err := s.SyncWriteFiles(); err != nil {
		t.Fatal(err)
	}

	// THEN - the review comment stays unresolved on disk
	if diskCommentResolved(t, s, rc.ID) {
		t.Error("the daemon write put back the resolve on the review comment")
	}
}

// A resolve click the reviewer makes after the CLI reply landed on disk is
// newer than the CLI's resolved state, so the write keeps the click.
func TestDaemonWrite_KeepsReviewerResolveMadeAfterCLIReply(t *testing.T) {
	cases := []struct {
		name       string
		cliResolve bool // what the CLI reply set on disk
		clickTo    bool // what the reviewer then clicked
	}{
		{name: "CLI resolves, reviewer unresolves", cliResolve: true, clickTo: false},
		{name: "CLI unresolves, reviewer resolves", cliResolve: false, clickTo: true},
	}
	for _, tc := range cases {
		t.Run("file comment: "+tc.name, func(t *testing.T) {
			// GIVEN - a written comment in the state the reviewer will undo,
			// a CLI reply on disk, then the reviewer's click in memory
			s := newTestSession(t)
			c, _ := s.AddComment("plan.md", 1, 1, "", "make it readable", "", "Ondra", "")
			s.SetCommentResolved("plan.md", c.ID, !tc.cliResolve)
			if err := s.SyncWriteFiles(); err != nil {
				t.Fatal(err)
			}
			cliReplyAndSetResolved(t, s, c.ID, "r_cli", tc.cliResolve)
			s.SetCommentResolved("plan.md", c.ID, tc.clickTo)

			// WHEN - the daemon writes its state
			if err := s.SyncWriteFiles(); err != nil {
				t.Fatal(err)
			}

			// THEN - the reply is kept and so is the reviewer's click
			if !diskReplyIDs(t, s, c.ID)["r_cli"] {
				t.Error("the daemon write dropped the CLI reply")
			}
			if got := diskCommentResolved(t, s, c.ID); got != tc.clickTo {
				t.Errorf("resolved on disk = %v, want the reviewer's %v", got, tc.clickTo)
			}
		})
		t.Run("review comment: "+tc.name, func(t *testing.T) {
			s := newTestSession(t)
			rc := s.AddReviewComment("overall: split the plan", "Ondra", "")
			s.ResolveReviewComment(rc.ID, !tc.cliResolve)
			if err := s.SyncWriteFiles(); err != nil {
				t.Fatal(err)
			}
			cliReplyAndSetResolved(t, s, rc.ID, "r_cli", tc.cliResolve)
			s.ResolveReviewComment(rc.ID, tc.clickTo)

			if err := s.SyncWriteFiles(); err != nil {
				t.Fatal(err)
			}

			got, _ := s.FindReviewCommentByID(rc.ID)
			if len(got.Replies) != 1 || got.Replies[0].ID != "r_cli" {
				t.Errorf("replies = %+v, want the CLI reply", got.Replies)
			}
			if resolved := diskCommentResolved(t, s, rc.ID); resolved != tc.clickTo {
				t.Errorf("resolved on disk = %v, want the reviewer's %v", resolved, tc.clickTo)
			}
		})
	}
}

package session

import "testing"

// TestRemappedCarriedCommentReplacesDiskCopy verifies that a carried comment
// with a remapped position updates the persisted identity instead of leaving
// the old position beside the new one.
func TestRemappedCarriedCommentReplacesDiskCopy(t *testing.T) {
	cj := CritJSON{Files: map[string]CritJSONFile{
		"plan.md": {Comments: []Comment{{
			ID: "c_thread", StartLine: 3, EndLine: 3, Body: "same anchor",
		}}},
	}}
	mergeFileSnapshotIntoCritJSON(&cj, writeFileSnapshot{
		path: "plan.md",
		comments: []Comment{{
			ID: "c_thread", StartLine: 4, EndLine: 4, Body: "same anchor",
		}},
	})

	comments := cj.Files["plan.md"].Comments
	if len(comments) != 1 {
		t.Fatalf("merged comments = %d, want 1: %+v", len(comments), comments)
	}
	if comments[0].ID != "c_thread" || comments[0].StartLine != 4 || comments[0].EndLine != 4 {
		t.Fatalf("merged comment = %+v, want c_thread at line 4", comments[0])
	}
}

// TestExportedCarryForwardCommentPreservesID covers the exported wrapper used
// by internal/live, which reaches carry-forward through session.CarryForwardComment
// rather than the unexported helper.
func TestExportedCarryForwardCommentPreservesID(t *testing.T) {
	carried := CarryForwardComment(Comment{
		ID:        "c_thread",
		StartLine: 7,
		EndLine:   7,
		Body:      "still relevant",
	}, "2026-09-16T00:00:00Z")

	if carried.ID != "c_thread" {
		t.Errorf("ID = %q, want c_thread", carried.ID)
	}
	if !carried.CarriedForward {
		t.Error("CarriedForward = false, want true")
	}
	if carried.UpdatedAt != "2026-09-16T00:00:00Z" {
		t.Errorf("UpdatedAt = %q, want the carry timestamp", carried.UpdatedAt)
	}
}

// TestCarriedCommentTimelineKeepsOneThreadAcrossRounds verifies that a carried
// parent remains visible in both rounds while replies stay scoped to the round
// in which they were authored.
func TestCarriedCommentTimelineKeepsOneThreadAcrossRounds(t *testing.T) {
	comments := []Comment{{
		ID:             "c_thread",
		ReviewRound:    1,
		CarriedForward: true,
		Replies: []Reply{
			{ID: "rp_round1", ReviewRound: 1},
			{ID: "rp_round2", ReviewRound: 2},
		},
	}}

	roundOne := commentsAtOrBeforeRound(comments, 1)
	if len(roundOne) != 1 || roundOne[0].ID != "c_thread" {
		t.Fatalf("round 1 parent = %+v, want c_thread", roundOne)
	}
	if len(roundOne[0].Replies) != 1 || roundOne[0].Replies[0].ID != "rp_round1" {
		t.Fatalf("round 1 replies = %+v, want rp_round1", roundOne[0].Replies)
	}

	roundTwo := commentsAtOrBeforeRound(comments, 2)
	if len(roundTwo) != 1 || roundTwo[0].ID != "c_thread" {
		t.Fatalf("round 2 parent = %+v, want c_thread", roundTwo)
	}
	if len(roundTwo[0].Replies) != 2 {
		t.Fatalf("round 2 replies = %+v, want both replies", roundTwo[0].Replies)
	}
}

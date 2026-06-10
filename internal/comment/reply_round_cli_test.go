package comment

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

func TestAppendReply_StampsCritJSONReviewRound(t *testing.T) {
	cj := &session.CritJSON{
		ReviewRound: 4,
		Files: map[string]session.CritJSONFile{
			"a.go": {
				Status: "modified",
				Comments: []session.Comment{
					{ID: "c1", ReviewRound: 1, Body: "x"},
				},
			},
		},
	}
	if err := AppendReply(cj, "c1", "reply", "agent", "", false, ""); err != nil {
		t.Fatalf("AppendReply: %v", err)
	}
	got := cj.Files["a.go"].Comments[0].Replies[0].ReviewRound
	if got != 4 {
		t.Errorf("expected reply ReviewRound=4 (from cj.ReviewRound), got %d", got)
	}

	cj2 := &session.CritJSON{
		ReviewRound:    7,
		ReviewComments: []session.Comment{{ID: "r0", Scope: "review"}},
		Files:          map[string]session.CritJSONFile{},
	}
	if err := AppendReply(cj2, "r0", "ack", "agent", "", false, ""); err != nil {
		t.Fatalf("AppendReply review: %v", err)
	}
	if cj2.ReviewComments[0].Replies[0].ReviewRound != 7 {
		t.Errorf("expected review reply ReviewRound=7, got %d", cj2.ReviewComments[0].Replies[0].ReviewRound)
	}
}

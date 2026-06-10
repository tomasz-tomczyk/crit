package comment

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

func TestReplyResolveStampsResolvedRound_CLI(t *testing.T) {
	cj := session.CritJSON{
		ReviewRound: 5,
		Files: map[string]session.CritJSONFile{
			"a.md": {
				Status: "modified",
				Comments: []session.Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "open", ReviewRound: 1},
				},
			},
		},
	}
	if err := AppendReply(&cj, "c1", "done", "alice", "", true, ""); err != nil {
		t.Fatal(err)
	}
	got := cj.Files["a.md"].Comments[0]
	if !got.Resolved {
		t.Fatal("expected resolved=true after --resolve")
	}
	if got.ResolvedRound != 5 {
		t.Errorf("ResolvedRound = %d, want 5", got.ResolvedRound)
	}
	if len(got.Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(got.Replies))
	}
}

func TestReplyResolveReviewLevel_CLI(t *testing.T) {
	cj := session.CritJSON{
		ReviewRound: 7,
		ReviewComments: []session.Comment{
			{ID: "r1", Body: "review note", ReviewRound: 1, Scope: "review"},
		},
		Files: map[string]session.CritJSONFile{},
	}
	if err := AppendReply(&cj, "r1", "addressed", "alice", "", true, ""); err != nil {
		t.Fatal(err)
	}
	got := cj.ReviewComments[0]
	if !got.Resolved || got.ResolvedRound != 7 {
		t.Errorf("ResolvedRound=%d resolved=%v, want 7/true", got.ResolvedRound, got.Resolved)
	}
}

func TestAppendReply_NonResolvingClearsResolved(t *testing.T) {
	t.Run("file_comment", func(t *testing.T) {
		cj := session.CritJSON{
			ReviewRound: 4,
			Files: map[string]session.CritJSONFile{
				"a.md": {
					Status: "modified",
					Comments: []session.Comment{
						{
							ID: "c1", StartLine: 1, EndLine: 1, Body: "open", ReviewRound: 1,
							Resolved: true, ResolvedRound: 2,
						},
					},
				},
			},
		}
		if err := AppendReply(&cj, "c1", "actually not done", "alice", "", false, ""); err != nil {
			t.Fatal(err)
		}
		got := cj.Files["a.md"].Comments[0]
		if got.Resolved {
			t.Error("expected Resolved=false after non-resolving reply")
		}
		if got.ResolvedRound != 0 {
			t.Errorf("ResolvedRound = %d, want 0", got.ResolvedRound)
		}
		if len(got.Replies) != 1 {
			t.Errorf("expected 1 reply, got %d", len(got.Replies))
		}
	})

	t.Run("review_comment", func(t *testing.T) {
		cj := session.CritJSON{
			ReviewRound: 4,
			ReviewComments: []session.Comment{
				{ID: "r1", Body: "review note", ReviewRound: 1, Scope: "review",
					Resolved: true, ResolvedRound: 2},
			},
			Files: map[string]session.CritJSONFile{},
		}
		if err := AppendReply(&cj, "r1", "actually not done", "alice", "", false, ""); err != nil {
			t.Fatal(err)
		}
		got := cj.ReviewComments[0]
		if got.Resolved {
			t.Error("expected Resolved=false after non-resolving reply")
		}
		if got.ResolvedRound != 0 {
			t.Errorf("ResolvedRound = %d, want 0", got.ResolvedRound)
		}
	})
}

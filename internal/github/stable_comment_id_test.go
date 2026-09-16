package github

import (
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

// TestCarriedGitHubCommentIsNotRepostedAfterRoundBump verifies the persisted
// state produced by a carry-forward remains an already-pushed comment. A
// stable local ID must not change the GitHub POST/PATCH selection rules.
func TestCarriedGitHubCommentIsNotRepostedAfterRoundBump(t *testing.T) {
	cj := session.CritJSON{
		ReviewRound: 2,
		Files: map[string]session.CritJSONFile{
			"plan.md": {Comments: []session.Comment{{
				ID:                 "c_thread",
				Body:               "keep this detail",
				GitHubID:           417,
				LastPushedBodyHash: bodyHashAtPush("keep this detail"),
				ReviewRound:        1,
				CarriedForward:     true,
			}}},
		},
	}

	if edits := collectEditedForPush(cj); len(edits) != 0 {
		t.Fatalf("carried comment selected for PATCH: %+v", edits)
	}
	if replies := collectNewRepliesForPush(cj.Files["plan.md"], nil); len(replies) != 0 {
		t.Fatalf("carried comment selected for POST: %+v", replies)
	}
}

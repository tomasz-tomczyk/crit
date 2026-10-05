package share

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

func TestApplyWebCommentPlacements_UpdatesMatchedComment(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit")
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{{
				ID: "c1", Body: "Expand this", StartLine: 3, EndLine: 3, Scope: "line",
			}}},
		},
	}
	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session.MustMkdirAll(review.ReviewPathsFor(critPath).Review), data, 0o644); err != nil {
		t.Fatal(err)
	}

	placed := cj.Files["plan.md"].Comments[0]
	placed.StartLine = 5
	placed.EndLine = 5
	placed.Anchor = "Step 1"
	placed.Drifted = true
	if err := applyWebCommentPlacements(critPath, map[string]session.Comment{"c1": placed}); err != nil {
		t.Fatal(err)
	}

	data, err = os.ReadFile(review.ReviewPathsFor(critPath).Review)
	if err != nil {
		t.Fatal(err)
	}
	var got CritJSON
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	c := got.Files["plan.md"].Comments[0]
	if c.StartLine != 5 || c.EndLine != 5 || c.Anchor != "Step 1" || !c.Drifted || c.Body != "Expand this" {
		t.Fatalf("placement = %+v", c)
	}
}

func TestCarriedPlacement_IgnoresUnchangedComment(t *testing.T) {
	local := session.Comment{ID: "c1", StartLine: 3, EndLine: 3, Anchor: "Step 1"}
	wc := WebComment{ExternalID: "c1", StartLine: 3, EndLine: 3, Anchor: "Step 1"}
	if _, ok := carriedPlacement(wc, map[string]session.Comment{"c1": local}); ok {
		t.Fatal("unchanged comment was treated as moved")
	}
}

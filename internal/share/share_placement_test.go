package share

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestFetchHasUpdates(t *testing.T) {
	if fetchHasUpdates(fetchWebCommentsResult{}) {
		t.Fatal("empty fetch was treated as an update")
	}
	fetched := fetchWebCommentsResult{Placements: map[string]session.Comment{"c1": {}}}
	if !fetchHasUpdates(fetched) {
		t.Fatal("a carried placement was ignored")
	}
}

func TestCarriedPlacement_IgnoresUnchangedComment(t *testing.T) {
	local := session.Comment{ID: "c1", StartLine: 3, EndLine: 3, Anchor: "Step 1"}
	wc := WebComment{ExternalID: "c1", StartLine: 3, EndLine: 3, Anchor: "Step 1"}
	if _, ok := carriedPlacement(wc, map[string]session.Comment{"c1": local}); ok {
		t.Fatal("unchanged comment was treated as moved")
	}
}

func TestCarriedPlacement_CopiesMovedFields(t *testing.T) {
	local := session.Comment{ID: "c1", Body: "Expand this", StartLine: 3, EndLine: 3}
	wc := WebComment{ExternalID: "c1", StartLine: 5, EndLine: 5, Anchor: "Step 1", Drifted: true}
	got, ok := carriedPlacement(wc, map[string]session.Comment{"c1": local})
	if !ok || got.StartLine != 5 || got.EndLine != 5 || got.Anchor != "Step 1" || !got.Drifted || got.Body != "Expand this" {
		t.Fatalf("placement = %+v ok=%v", got, ok)
	}
	if _, ok := carriedPlacement(WebComment{}, nil); ok {
		t.Fatal("blank identity was treated as a placement")
	}
	if _, ok := carriedPlacement(WebComment{ExternalID: "missing"}, map[string]session.Comment{"c1": local}); ok {
		t.Fatal("unknown comment was treated as a placement")
	}
}

func TestLocalCommentsByID_SkipsBlankIDs(t *testing.T) {
	cj := session.CritJSON{
		Files: map[string]session.CritJSONFile{
			"plan.md": {Comments: []session.Comment{{ID: "c1", Body: "keep"}, {Body: "no id"}}},
		},
		ReviewComments: []session.Comment{{ID: "r1", Body: "whole review"}, {Body: "blank"}},
	}
	got := localCommentsByID(cj)
	if len(got) != 2 || got["c1"].Body != "keep" || got["r1"].Body != "whole review" {
		t.Fatalf("indexed = %+v", got)
	}
}

func TestApplyWebCommentPlacements_UpdatesReviewComment(t *testing.T) {
	critPath := writePlacementReview(t, CritJSON{
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{
				{ID: "c1", Body: "stay", StartLine: 1, EndLine: 1},
				{ID: "c2", Body: "move", StartLine: 3, EndLine: 3},
			}},
		},
		ReviewComments: []Comment{{ID: "r1", Body: "scope", Scope: "review"}},
	})
	if err := ApplyWebCommentPlacements(critPath, map[string]session.Comment{
		"c2": {StartLine: 7, EndLine: 7, Anchor: "Step 2"},
		"r1": {StartLine: 0, EndLine: 0, Anchor: "whole", Drifted: true},
	}); err != nil {
		t.Fatal(err)
	}
	got := readPlacementReview(t, critPath)
	if got.Files["plan.md"].Comments[0].StartLine != 1 || got.Files["plan.md"].Comments[1].StartLine != 7 {
		t.Fatalf("file comments = %+v", got.Files["plan.md"].Comments)
	}
	if !got.ReviewComments[0].Drifted || got.ReviewComments[0].Anchor != "whole" {
		t.Fatalf("review comment = %+v", got.ReviewComments[0])
	}
}

func TestApplyWebCommentPlacements_EmptyAndUnreadable(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit")
	if err := applyWebCommentPlacements(critPath, nil); err != nil {
		t.Fatal(err)
	}
	if err := applyWebCommentPlacements(critPath, map[string]session.Comment{"c1": {}}); err == nil {
		t.Fatal("missing review file was accepted")
	}
	if err := os.WriteFile(session.MustMkdirAll(review.ReviewPathsFor(critPath).Review), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyWebCommentPlacements(critPath, map[string]session.Comment{"c1": {}}); err == nil {
		t.Fatal("broken review file was accepted")
	}
}

func TestMergeFetchedComments_AddsCommentAndAppliesPlacement(t *testing.T) {
	critPath := writePlacementReview(t, CritJSON{
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{{ID: "c1", Body: "Expand this", StartLine: 3, EndLine: 3}}},
		},
	})
	err := mergeFetchedComments(critPath, fetchWebCommentsResult{
		NewComments: []WebComment{{Body: "from the web", FilePath: "plan.md", StartLine: 1, EndLine: 1, AuthorDisplayName: "Web"}},
		Placements:  map[string]session.Comment{"c1": {StartLine: 5, EndLine: 5, Anchor: "Step 1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := readPlacementReview(t, critPath)
	comments := got.Files["plan.md"].Comments
	if len(comments) != 2 || comments[0].StartLine != 5 || comments[0].Anchor != "Step 1" || comments[1].Body != "from the web" {
		t.Fatalf("comments = %+v", comments)
	}
}

func TestFetchWebCommentsForReview_RecordsCarriedPlacement(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/comments") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]WebComment{{
			ExternalID: "c1", Body: "Expand this", FilePath: "plan.md",
			StartLine: 5, EndLine: 5, Anchor: "Step 1",
		}})
	}))
	defer ts.Close()

	cj := session.CritJSON{Files: map[string]session.CritJSONFile{
		"plan.md": {Comments: []session.Comment{{ID: "c1", Body: "Expand this", StartLine: 3, EndLine: 3}}},
	}}
	got, err := fetchWebCommentsForReview(ts.URL+"/r/tok", ts.URL, cj, "")
	if err != nil {
		t.Fatal(err)
	}
	placed, ok := got.Placements["c1"]
	if !ok || placed.StartLine != 5 || placed.Anchor != "Step 1" || len(got.NewComments) != 0 {
		t.Fatalf("fetch = %+v", got)
	}
}

func TestPrintFetchResult_ReportsCommentsPlacementsAndReplies(t *testing.T) {
	var buf bytes.Buffer
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printFetchResult(fetchWebCommentsResult{
		NewComments:  []WebComment{{Body: "hello", FilePath: "plan.md", StartLine: 2}},
		Placements:   map[string]session.Comment{"c1": {}},
		ReplyUpdates: map[string][]WebReply{"c1": {{Body: "thanks"}}},
	})
	w.Close()
	os.Stdout = old
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	for _, want := range []string{"Fetched 1 new comment", "Updated 1 comment placement", "Updated 1 comment(s) with 1 new reply"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

func writePlacementReview(t *testing.T, cj CritJSON) string {
	t.Helper()
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit")
	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session.MustMkdirAll(review.ReviewPathsFor(critPath).Review), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return critPath
}

func readPlacementReview(t *testing.T, critPath string) CritJSON {
	t.Helper()
	data, err := os.ReadFile(review.ReviewPathsFor(critPath).Review)
	if err != nil {
		t.Fatal(err)
	}
	var got CritJSON
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

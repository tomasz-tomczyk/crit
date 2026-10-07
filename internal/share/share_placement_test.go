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

func TestMergeFetchedComments_AppliesPlacement(t *testing.T) {
	critPath := writePlacementReview(t, CritJSON{
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{{
				ID: "c1", Body: "Expand this", StartLine: 3, EndLine: 3, Scope: "line",
			}}},
		},
	})

	placed := Comment{StartLine: 5, EndLine: 5, Anchor: "Step 1", Drifted: true}
	if err := mergeFetchedComments(critPath, fetchWebCommentsResult{Placements: map[string]session.Comment{"c1": placed}}); err != nil {
		t.Fatal(err)
	}

	c := readPlacementReview(t, critPath).Files["plan.md"].Comments[0]
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

func TestLocalCommentsByID_OnlyCommentsOnSharedLines(t *testing.T) {
	cj := session.CritJSON{
		Files: map[string]session.CritJSONFile{
			"plan.md": {Comments: []session.Comment{
				{ID: "c1", Body: "keep", StartLine: 3, EndLine: 3},
				{ID: "c2", Body: "carried locally", StartLine: 9, EndLine: 9},
				{ID: "c3", Body: "never shared", StartLine: 1, EndLine: 1},
				{Body: "no id"},
			}},
		},
		ReviewComments: []session.Comment{{ID: "r1", Body: "whole review"}, {Body: "blank"}},
		SharedLines: map[string]session.SharedLine{
			"c1": {StartLine: 3, EndLine: 3},
			"c2": {StartLine: 4, EndLine: 4},
			"r1": {},
		},
	}
	got := localCommentsByID(cj)
	if len(got) != 2 || got["c1"].Body != "keep" || got["r1"].Body != "whole review" {
		t.Fatalf("indexed = %+v", got)
	}
}

func TestSharedLines_KeyedByExternalID(t *testing.T) {
	got := sharedLines([]ShareComment{
		{ExternalID: "c1", StartLine: 3, EndLine: 5},
		{Body: "no id", StartLine: 1, EndLine: 1},
	})
	if len(got) != 1 || got["c1"] != (session.SharedLine{StartLine: 3, EndLine: 5}) {
		t.Fatalf("shared lines = %+v", got)
	}
}

func TestMergeFetchedComments_PlacesReviewComment(t *testing.T) {
	critPath := writePlacementReview(t, CritJSON{
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{
				{ID: "c1", Body: "stay", StartLine: 1, EndLine: 1},
				{ID: "c2", Body: "move", StartLine: 3, EndLine: 3},
			}},
		},
		ReviewComments: []Comment{{ID: "r1", Body: "scope", Scope: "review"}},
	})
	if err := MergeFetchedComments(critPath, FetchWebCommentsResult{Placements: map[string]session.Comment{
		"c2": {StartLine: 7, EndLine: 7, Anchor: "Step 2"},
		"r1": {StartLine: 0, EndLine: 0, Anchor: "whole", Drifted: true},
	}}); err != nil {
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

func TestMergeFetchedComments_EmptyAndUnreadable(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit")
	if err := mergeFetchedComments(critPath, fetchWebCommentsResult{}); err != nil {
		t.Fatal(err)
	}
	placement := fetchWebCommentsResult{Placements: map[string]session.Comment{"c1": {}}}
	if err := mergeFetchedComments(critPath, placement); err == nil {
		t.Fatal("missing review file was accepted")
	}
	if err := os.WriteFile(session.MustMkdirAll(review.ReviewPathsFor(critPath).Review), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mergeFetchedComments(critPath, placement); err == nil {
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

	cj := session.CritJSON{
		Files: map[string]session.CritJSONFile{
			"plan.md": {Comments: []session.Comment{{ID: "c1", Body: "Expand this", StartLine: 3, EndLine: 3}}},
		},
		SharedLines: map[string]session.SharedLine{"c1": {StartLine: 3, EndLine: 3}},
	}
	got, err := fetchWebCommentsForReview(ts.URL+"/r/tok", ts.URL, cj, "")
	if err != nil {
		t.Fatal(err)
	}
	placed, ok := got.Placements["c1"]
	if !ok || placed.StartLine != 5 || placed.Anchor != "Step 1" || len(got.NewComments) != 0 {
		t.Fatalf("fetch = %+v", got)
	}
}

// The agent edited plan.md and the local carry moved c1 from line 10 to 15.
// crit-web still has the copy from the last share at line 10. A share pulls
// before it uploads; the stale web line must not overwrite the local carry.
func TestShareExistingPull_LocalEditKeepsLocalCarry(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]WebComment{{
			ExternalID: "c1", Body: "Expand this", FilePath: "plan.md",
			StartLine: 10, EndLine: 10, Anchor: "Step 1",
		}})
	}))
	defer ts.Close()

	critPath := writePlacementReview(t, CritJSON{
		SharedLines: map[string]session.SharedLine{"c1": {StartLine: 10, EndLine: 10}},
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{{ID: "c1", Body: "Expand this", StartLine: 15, EndLine: 15, Anchor: "Step 1", CarriedForward: true}}},
		},
	})
	cj := readPlacementReview(t, critPath)
	fetched, err := fetchWebCommentsForReview(ts.URL+"/r/tok", ts.URL, cj, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Placements) != 0 {
		t.Fatalf("stale web placement was taken: %+v", fetched.Placements)
	}
	if err := mergeFetchedComments(critPath, fetched); err != nil {
		t.Fatal(err)
	}
	got := readPlacementReview(t, critPath).Files["plan.md"].Comments[0]
	if got.StartLine != 15 || got.EndLine != 15 || got.Drifted {
		t.Fatalf("local carry was overwritten: %+v", got)
	}
}

// Without a record of the shared lines (a review shared before SharedLines
// existed) the local placement wins.
func TestFetchWebCommentsForReview_NoSharedLinesKeepsLocal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]WebComment{{
			ExternalID: "c1", Body: "Expand this", FilePath: "plan.md", StartLine: 5, EndLine: 5,
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
	if len(got.Placements) != 0 {
		t.Fatalf("placement taken without shared lines: %+v", got.Placements)
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

func TestMergeWebComments_CopiesAnchorDriftedQuoteOffset(t *testing.T) {
	critPath := writePlacementReview(t, CritJSON{Files: map[string]CritJSONFile{}})
	offset := 4
	if err := MergeWebComments(critPath, []WebComment{{
		Body: "web note", FilePath: "plan.md", StartLine: 2, EndLine: 2,
		Quote: "Step", QuoteOffset: &offset, Anchor: "Step 1", Drifted: true,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	c := readPlacementReview(t, critPath).Files["plan.md"].Comments[0]
	if c.Anchor != "Step 1" || !c.Drifted || c.QuoteOffset == nil || *c.QuoteOffset != 4 {
		t.Fatalf("imported comment = %+v", c)
	}
}

func TestCommentToShareComment_SendsQuoteOffset(t *testing.T) {
	offset := 7
	sc := commentToShareComment(session.Comment{ID: "c1", Body: "b", Quote: "q", QuoteOffset: &offset}, "plan.md", "line", "", "", false, true)
	data, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"quote_offset":7`) {
		t.Fatalf("payload = %s", data)
	}
	data, _ = json.Marshal(commentToShareComment(session.Comment{ID: "c2", Body: "b"}, "plan.md", "line", "", "", false, true))
	if strings.Contains(string(data), "quote_offset") {
		t.Fatalf("nil offset was sent: %s", data)
	}
}

// When the content and resolved state match the last share, the upsert makes
// no PUT, so crit-web keeps the lines from the share before. The record must
// keep them too: the local carry moved c1 from 3 to 5, and moving the record
// to 5 would let crit-web's stale line 3 win on the next pull.
func TestShareExisting_NoPutKeepsSharedLines(t *testing.T) {
	putCalled := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			putCalled = true
		}
		_ = json.NewEncoder(w).Encode([]WebComment{{
			ExternalID: "c1", Body: "Expand this", FilePath: "plan.md", StartLine: 3, EndLine: 3,
		}})
	}))
	defer ts.Close()

	critPath := writePlacementReview(t, CritJSON{
		ShareURL:     ts.URL + "/r/tok",
		ShareBaseURL: ts.URL,
		DeleteToken:  "dt",
		ReviewRound:  1,
		SharedLines:  map[string]session.SharedLine{"c1": {StartLine: 3, EndLine: 3}},
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{{ID: "c1", Body: "Expand this", StartLine: 5, EndLine: 5}}},
		},
	})
	files := []ShareFile{{Path: "plan.md", Content: "# Plan\n"}}
	paths := []string{"plan.md"}
	comments, _ := LoadCommentsForShare(critPath, paths, "")
	cj := readPlacementReview(t, critPath)
	cj.LastShareHash = computeShareHash(files, comments)
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(review.ReviewPathsFor(critPath).Review, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runShareExisting(cj, critPath, files, paths, ts.URL, "", "", "", "", false); err != nil {
		t.Fatal(err)
	}
	if putCalled {
		t.Fatal("expected no PUT when the share hash matches")
	}
	got := readPlacementReview(t, critPath)
	if got.SharedLines["c1"] != (session.SharedLine{StartLine: 3, EndLine: 3}) {
		t.Fatalf("shared lines = %+v, want c1 kept at 3", got.SharedLines)
	}
	if c := got.Files["plan.md"].Comments[0]; c.StartLine != 5 {
		t.Fatalf("local comment = %+v, want line 5", c)
	}
}

func TestUpdateShareState_RecordsLinesOnlyWhenSent(t *testing.T) {
	critPath := writePlacementReview(t, CritJSON{
		SharedLines: map[string]session.SharedLine{"c1": {StartLine: 3, EndLine: 3}},
		Files:       map[string]CritJSONFile{},
	})
	comments := []ShareComment{{ExternalID: "c1", StartLine: 5, EndLine: 5, Body: "x"}}
	if err := updateShareState(critPath, nil, comments, 2, false); err != nil {
		t.Fatal(err)
	}
	if got := readPlacementReview(t, critPath); got.SharedLines["c1"].StartLine != 3 || got.ReviewRound != 2 || got.LastShareHash == "" {
		t.Fatalf("after unsent update = %+v", got)
	}
	if err := updateShareState(critPath, nil, comments, 2, true); err != nil {
		t.Fatal(err)
	}
	if got := readPlacementReview(t, critPath); got.SharedLines["c1"].StartLine != 5 {
		t.Fatalf("after sent update = %+v", got.SharedLines)
	}
}

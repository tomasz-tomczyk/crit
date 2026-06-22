package comment

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestReview(t *testing.T, dir string, cj CritJSON) string {
	t.Helper()
	critPath := filepath.Join(dir, ".crit")
	if err := os.MkdirAll(critPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
	}
	return critPath
}

func TestListCommentsFromCritJSON_OrderAndFilter(t *testing.T) {
	cj := CritJSON{
		ReviewComments: []Comment{
			{ID: "r_1", Body: "review note", Scope: "review", Resolved: false},
			{ID: "r_2", Body: "resolved review", Scope: "review", Resolved: true},
		},
		Files: map[string]CritJSONFile{
			"b.go": {
				Comments: []Comment{
					{ID: "c_line", Body: "line", StartLine: 20, EndLine: 20},
					{ID: "c_file", Body: "file", Scope: "file"},
					{ID: "c_done", Body: "done", StartLine: 5, Resolved: true},
				},
			},
			"a.go": {
				Comments: []Comment{
					{ID: "c_a", Body: "earlier line", StartLine: 1},
				},
			},
		},
	}

	entries := listCommentsFromCritJSON(cj, true)
	wantIDs := []string{"r_1", "c_a", "c_file", "c_line"}
	if len(entries) != len(wantIDs) {
		t.Fatalf("got %d entries, want %d: %+v", len(entries), len(wantIDs), entries)
	}
	for i, id := range wantIDs {
		if entries[i].ID != id {
			t.Errorf("entry %d: got id %q, want %q", i, entries[i].ID, id)
		}
	}
	if entries[0].Scope != "review" {
		t.Errorf("first entry scope = %q, want review", entries[0].Scope)
	}
	if entries[2].Scope != "file" {
		t.Errorf("third entry scope = %q, want file", entries[2].Scope)
	}
}

func TestListCommentsFromCritJSON_AllIncludesResolved(t *testing.T) {
	cj := CritJSON{
		ReviewComments: []Comment{
			{ID: "r_1", Body: "open", Scope: "review"},
			{ID: "r_2", Body: "closed", Scope: "review", Resolved: true},
		},
	}
	unresolved := listCommentsFromCritJSON(cj, true)
	if len(unresolved) != 1 {
		t.Fatalf("unresolved: got %d, want 1", len(unresolved))
	}
	all := listCommentsFromCritJSON(cj, false)
	if len(all) != 2 {
		t.Fatalf("all: got %d, want 2", len(all))
	}
}

func TestFormatCommentsText_Empty(t *testing.T) {
	if got := formatCommentsText(nil, true); got != "No unresolved comments." {
		t.Errorf("got %q", got)
	}
}

func TestFormatCommentsText_WithReplies(t *testing.T) {
	path := "main.go"
	entries := []ListedComment{{
		Scope: "line", Path: &path,
		Comment: Comment{
			ID: "c_1", StartLine: 10, EndLine: 12, Body: "fix this",
			Quote: "selected text", Drifted: true,
			Replies: []Reply{{ID: "rp_1", Author: "Bot", Body: "on it"}},
		},
	}}
	out := formatCommentsText(entries, true)
	if !strings.Contains(out, "[c_1] line main.go:10-12 (drifted)") {
		t.Errorf("missing header: %s", out)
	}
	if !strings.Contains(out, "quote:  selected text") {
		t.Errorf("missing quote: %s", out)
	}
	if !strings.Contains(out, "[rp_1] Bot: on it") {
		t.Errorf("missing reply: %s", out)
	}
}

func TestRunComments_JSONOutput(t *testing.T) {
	tmp := t.TempDir()
	writeTestReview(t, tmp, CritJSON{
		ReviewComments: []Comment{{ID: "r_1", Body: "note", Scope: "review"}},
	})

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := RunComments([]string{"--output", tmp, "--json"})
	w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	var entries []ListedComment
	if err := json.Unmarshal(buf.Bytes(), &entries); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	if len(entries) != 1 || entries[0].ID != "r_1" {
		t.Fatalf("got %+v", entries)
	}
}

func TestListCommentsFromCritJSON_PreservesPreviewFields(t *testing.T) {
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"/preview-content/": {
				Comments: []Comment{{
					ID: "c_pin", Body: "fix heading", StartLine: 0, EndLine: 0,
					Author: "Alice", CreatedAt: "2026-01-01T00:00:00Z",
					PinNumber: 3,
					DOMAnchor: &DOMAnchor{
						Pathname:       "/preview-content/",
						CSSSelector:    "#deck > h1",
						TagChain:       []string{"MAIN", "H1"},
						AccessibleName: "Give us the reins.",
						OuterHTML:      "<h1>Give us the reins.</h1>",
						ViewportWidth:  1280,
						ViewportHeight: 800,
					},
				}},
			},
		},
	}

	entries := listCommentsFromCritJSON(cj, true)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.PinNumber != 3 {
		t.Errorf("pin_number = %d, want 3", e.PinNumber)
	}
	if e.Author != "Alice" {
		t.Errorf("author = %q, want Alice", e.Author)
	}
	if e.DOMAnchor == nil || e.DOMAnchor.CSSSelector != "#deck > h1" {
		t.Errorf("dom_anchor = %+v", e.DOMAnchor)
	}

	data, err := encodeCommentsJSON(entries)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"dom_anchor"`) {
		t.Fatalf("dom_anchor missing from JSON: %s", data)
	}
	var roundtrip []ListedComment
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if roundtrip[0].DOMAnchor == nil {
		t.Fatalf("dom_anchor missing after JSON roundtrip: %s", data)
	}
}

func TestListedCommentJSONIncludesAllCommentFields(t *testing.T) {
	offset := 4
	c := Comment{
		ID: "c_full", StartLine: 7, EndLine: 9, Side: "RIGHT", Body: "note",
		Quote: "quoted", QuoteOffset: &offset, Anchor: "anchor-text",
		Drifted: true, DriftedOnRound: 2, Author: "alice", UserID: "u1",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z",
		Resolved: true, ResolvedRound: 2, Live: true, CarriedForward: true,
		ReviewRound: 1, GitHubID: 99, HeadSHA: "abc", DiffScope: "layer",
		PinNumber: 5, FocusKey: "pr:42",
		DOMAnchor: &DOMAnchor{
			Pathname: "/preview-content/", CSSSelector: "#main", TagChain: []string{"MAIN"},
			AccessibleName: "title", OuterHTML: "<main/>", ViewportWidth: 100, ViewportHeight: 200,
		},
		Replies: []Reply{{ID: "rp_1", Body: "reply", Author: "bob", CreatedAt: "2026-01-03T00:00:00Z"}},
	}

	commentKeys := jsonObjectKeys(t, c)
	lc := toListedComment("line", "f.go", c)
	listedKeys := jsonObjectKeys(t, lc)

	for key := range commentKeys {
		if key == "scope" {
			continue // ListedComment.scope is the list location, not Comment.scope
		}
		if !listedKeys[key] {
			t.Errorf("ListedComment JSON missing key %q present on Comment", key)
		}
	}
	if !listedKeys["scope"] {
		t.Error("ListedComment JSON missing list-specific key scope")
	}
	if !listedKeys["path"] {
		t.Error("ListedComment JSON missing list-specific key path")
	}
}

func jsonObjectKeys(t *testing.T, v any) map[string]bool {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatal(err)
	}
	keys := make(map[string]bool, len(obj))
	for k := range obj {
		keys[k] = true
	}
	return keys
}

func TestRunComments_ExplicitReviewJSONPath(t *testing.T) {
	tmp := t.TempDir()
	critPath := writeTestReview(t, tmp, CritJSON{
		Files: map[string]CritJSONFile{
			"x.go": {Comments: []Comment{{ID: "c_1", Body: "hello", StartLine: 1}}},
		},
	})
	reviewJSON := filepath.Join(critPath, "review.json")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := RunComments([]string{reviewJSON})
	w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "[c_1]") {
		t.Errorf("output missing comment: %s", buf.String())
	}
}

func TestRunComments_PlanAndOutputConflict(t *testing.T) {
	err := RunComments([]string{"--plan", "x", "--output", "/tmp", "--json"})
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveExplicitReviewPath(t *testing.T) {
	tmp := t.TempDir()
	critPath := filepath.Join(tmp, ".crit")
	if err := os.MkdirAll(critPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(critPath, "review.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveExplicitReviewPath(critPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != critPath {
		t.Errorf("got %q, want %q", got, critPath)
	}

	got, err = resolveExplicitReviewPath(filepath.Join(critPath, "review.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got != critPath {
		t.Errorf("review.json path: got %q, want %q", got, critPath)
	}
}

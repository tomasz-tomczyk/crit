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
	start, end := 10, 12
	quote := "selected text"
	entries := []ListedComment{{
		Scope: "line", ID: "c_1", Path: &path,
		StartLine: &start, EndLine: &end, Body: "fix this",
		Quote: &quote, Drifted: true,
		Replies: []Reply{{ID: "rp_1", Author: "Bot", Body: "on it"}},
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

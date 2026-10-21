package comment

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
)

func subThreadReview() CritJSON {
	return CritJSON{
		ReviewComments: []Comment{
			{ID: "r_root", Body: "general", Scope: "review", Resolved: true},
			{ID: "r_sub", Body: "on the summary", Scope: "review", ParentID: "r_root", Quote: "general"},
		},
		Files: map[string]CritJSONFile{
			"a.go": {Comments: []Comment{
				{ID: "c_sub", Body: "why?", Scope: "line", ParentID: "rp_1", Quote: "use a mutex"},
				{ID: "c_root", Body: "root", Scope: "line", StartLine: 4, EndLine: 4,
					Replies: []Reply{{ID: "rp_1", Body: "I would use a mutex", Author: "agent"}}},
			}},
		},
	}
}

func captureCommentsStdout(t *testing.T, fn func() error) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := fn()
	w.Close()
	os.Stdout = old
	if runErr != nil {
		t.Fatal(runErr)
	}
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestRunComments_JSONExposesSubThreadParentAndQuote(t *testing.T) {
	tmp := t.TempDir()
	writeTestReview(t, tmp, subThreadReview())

	out := captureCommentsStdout(t, func() error { return RunComments([]string{"--output", tmp, "--json"}) })
	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	var ids []string
	byID := map[string]map[string]any{}
	for _, e := range entries {
		id, _ := e["id"].(string)
		ids = append(ids, id)
		byID[id] = e
	}
	// The resolved review root drops out; its open sub-thread stays. File
	// sub-threads follow the file's root comments.
	if got := strings.Join(ids, ","); got != "r_sub,c_root,c_sub" {
		t.Fatalf("listed ids = %s", got)
	}
	if e := byID["c_sub"]; e["parent_id"] != "rp_1" || e["quote"] != "use a mutex" || e["path"] != "a.go" || e["scope"] != "line" {
		t.Errorf("file sub-thread entry = %v", e)
	}
	if e := byID["r_sub"]; e["parent_id"] != "r_root" || e["quote"] != "general" || e["scope"] != "review" {
		t.Errorf("review sub-thread entry = %v", e)
	}
}

func TestFormatCommentsText_ShowsSubThreadParentAndQuote(t *testing.T) {
	text := formatCommentsText(listCommentsFromCritJSON(subThreadReview(), true), true)
	for _, want := range []string{
		"[c_sub] sub-thread a.go\n  parent: rp_1\n  quote:  use a mutex\n  body:   why?",
		"[r_sub] sub-thread\n  parent: r_root\n  quote:  general",
		"[c_root] line a.go:4",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "a.go:0") {
		t.Errorf("sub-thread shown on line 0:\n%s", text)
	}
}

// An agent answers a sub-thread with its own id, as any comment.
func TestRunComment_ReplyAndResolveSubThread(t *testing.T) {
	outputDir := t.TempDir()
	t.Chdir(outputDir)
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		t.Fatal(err)
	}
	reviewPath := filepath.Join(outputDir, "reviews", daemon.SessionKey(cwd, "", nil))
	cj := subThreadReview()
	cj.ReviewRound = 2
	if err := saveCritJSON(reviewPath, cj); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"c_sub", "r_sub"} {
		if err := RunComment([]string{"--output", outputDir, "--reply-to", id, "--resolve", "--author", "agent", "done"}); err != nil {
			t.Fatalf("reply to %s: %v", id, err)
		}
	}

	got, err := loadCritJSON(reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	comments := got.Files["a.go"].Comments
	sub, root := comments[0], comments[1]
	if !sub.Resolved || sub.ResolvedRound != 2 || len(sub.Replies) != 1 || sub.Replies[0].Body != "done" {
		t.Errorf("file sub-thread after reply = %+v", sub)
	}
	if root.Resolved || len(root.Replies) != 1 {
		t.Errorf("file root touched by the sub-thread reply: %+v", root)
	}
	reviewSub := got.ReviewComments[1]
	if !reviewSub.Resolved || len(reviewSub.Replies) != 1 {
		t.Errorf("review sub-thread after reply = %+v", reviewSub)
	}
	if reviewRoot := got.ReviewComments[0]; len(reviewRoot.Replies) != 0 {
		t.Errorf("review root touched by the sub-thread reply: %+v", reviewRoot)
	}
}

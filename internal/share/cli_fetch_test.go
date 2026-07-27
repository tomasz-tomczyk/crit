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
	"sync"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestRunFetch_PrintsReviewFilePath(t *testing.T) {
	tests := []struct {
		name        string
		comments    []WebComment
		wantContain string
	}{
		{
			name:        "no new comments",
			comments:    nil,
			wantContain: "No new comments.",
		},
		{
			name: "with new comments",
			comments: []WebComment{
				{Body: "fix this", FilePath: "main.go", StartLine: 10, EndLine: 10, Scope: "line"},
			},
			wantContain: "Fetched 1 new comment(s)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tc.comments)
			}))
			defer ts.Close()

			tmpDir := t.TempDir()
			cj := session.CritJSON{
				ShareURL: ts.URL + "/r/test123",
				Files:    map[string]session.CritJSONFile{},
			}
			data, err := json.Marshal(cj)
			if err != nil {
				t.Fatal(err)
			}
			critPath := filepath.Join(tmpDir, ".crit")
			if err := os.WriteFile(testutil.MustMkdirAll(review.ReviewPathsFor(critPath).Review), data, 0o644); err != nil {
				t.Fatal(err)
			}

			var buf bytes.Buffer
			old := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w
			err = RunFetch([]string{"--output", tmpDir})
			w.Close()
			os.Stdout = old
			io.Copy(&buf, r)
			if err != nil {
				t.Fatalf("RunFetch: %v", err)
			}
			output := buf.String()

			if !strings.Contains(output, tc.wantContain) {
				t.Errorf("expected output to contain %q, got:\n%s", tc.wantContain, output)
			}
			wantPath := "Review file: " + critPath
			if !strings.Contains(output, wantPath) {
				t.Errorf("expected output to contain %q, got:\n%s", wantPath, output)
			}
		})
	}
}

func TestRunFetch_NoReviewFile(t *testing.T) {
	dir := t.TempDir()
	err := RunFetch([]string{"--output", dir})
	if err == nil {
		t.Fatal("expected error when no review file")
	}
}

func TestRunFetch_NoShareURL(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit")
	if err := os.WriteFile(testutil.MustMkdirAll(review.ReviewPathsFor(critPath).Review), []byte(`{"files":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := RunFetch([]string{"--output", dir})
	if err == nil {
		t.Fatal("expected error when share URL missing")
	}
}

func TestRunFetch_InvalidReviewFile(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, ".crit")
	if err := os.WriteFile(testutil.MustMkdirAll(review.ReviewPathsFor(critPath).Review), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := RunFetch([]string{"--output", dir})
	if err == nil || !strings.Contains(err.Error(), "invalid review file") {
		t.Fatalf("RunFetch() = %v, want invalid review file error", err)
	}
}

func TestRunFetch_ReplyUpdates(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{
				"body":        "web-authored note",
				"file_path":   "plan.md",
				"start_line":  3,
				"end_line":    3,
				"resolved":    false,
				"external_id": nil,
				"replies": []map[string]any{
					{"body": "follow-up reply", "author_display_name": "Alice"},
				},
			},
		})
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	cj := session.CritJSON{
		ShareURL: ts.URL + "/r/tok",
		Files: map[string]session.CritJSONFile{
			"plan.md": {Comments: []session.Comment{{
				ID:        "web-1",
				Body:      "web-authored note",
				StartLine: 3,
				EndLine:   3,
			}}},
		},
	}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	critPath := filepath.Join(tmpDir, ".crit")
	if err := os.WriteFile(testutil.MustMkdirAll(review.ReviewPathsFor(critPath).Review), data, 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err = RunFetch([]string{"--output", tmpDir})
	w.Close()
	os.Stdout = old
	io.Copy(&buf, r)
	if err != nil {
		t.Fatalf("RunFetch: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Updated 1 comment(s) with 1 new reply") {
		t.Fatalf("output = %q, want reply update summary", out)
	}

	merged, err := os.ReadFile(review.ReviewPathsFor(critPath).Review)
	if err != nil {
		t.Fatal(err)
	}
	var after session.CritJSON
	if err := json.Unmarshal(merged, &after); err != nil {
		t.Fatal(err)
	}
	replies := after.Files["plan.md"].Comments[0].Replies
	if len(replies) != 1 || replies[0].Body != "follow-up reply" {
		t.Fatalf("replies = %+v, want follow-up reply", replies)
	}
}

func TestConcurrentFetchSameReview(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nil)
	}))
	defer ts.Close()

	tmpDir := t.TempDir()
	cj := session.CritJSON{
		ShareURL: ts.URL + "/r/tok",
		Files:    map[string]session.CritJSONFile{},
	}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatal(err)
	}
	critPath := filepath.Join(tmpDir, ".crit")
	if err := os.WriteFile(testutil.MustMkdirAll(review.ReviewPathsFor(critPath).Review), data, 0o644); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			errs <- RunFetch([]string{"--output", tmpDir})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent fetch: %v", err)
		}
	}
}

func TestPrintFetchedComments(t *testing.T) {
	var buf bytes.Buffer
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printFetchedComments([]WebComment{
		{Body: "review level", Scope: "review"},
		{Body: "line issue", FilePath: "a.go", StartLine: 3, Scope: "line"},
	})
	w.Close()
	os.Stdout = old
	io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "[review]") || !strings.Contains(out, "[a.go:3]") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestRunShare_MissingFiles(t *testing.T) {
	err := RunShare([]string{})
	if err == nil {
		t.Fatal("expected usage error for missing files")
	}
}

func TestRunShare_MissingFile(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	t.Chdir(t.TempDir())
	err := RunShare([]string{"missing.md"})
	if err == nil || !strings.Contains(err.Error(), "reading missing.md") {
		t.Fatalf("error = %v, want missing file error", err)
	}
}

func TestRunShare_OutputFlagMissingValue(t *testing.T) {
	err := RunShare([]string{"--output"})
	if err == nil {
		t.Fatal("expected error for --output without value")
	}
}

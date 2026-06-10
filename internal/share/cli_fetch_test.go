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

func TestRunShare_OutputFlagMissingValue(t *testing.T) {
	err := RunShare([]string{"--output"})
	if err == nil {
		t.Fatal("expected error for --output without value")
	}
}

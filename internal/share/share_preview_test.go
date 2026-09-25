package share

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// TestParseShareFlagsPreview verifies the --preview flag is parsed into the
// shareFlags.preview field and does not consume positional file arguments.
func TestParseShareFlagsPreview(t *testing.T) {
	sf, err := parseShareFlags([]string{"--preview", "index.html"})
	if err != nil {
		t.Fatalf("parseShareFlags: %v", err)
	}
	if sf.preview != "index.html" {
		t.Errorf("preview = %q, want %q", sf.preview, "index.html")
	}
	if len(sf.files) != 0 {
		t.Errorf("files = %v, want empty", sf.files)
	}
}

// TestPostPreviewShareDispatch verifies the --preview path crawls the local
// HTML file (plus its assets) and POSTs a payload with review_type=preview,
// original path metadata, and a base64-encoded binary entry. A stub server
// stands in for crit-web so the test exercises the full dispatch seam without
// the network.
func TestPostPreviewShareDispatch(t *testing.T) {
	dir := t.TempDir()
	writeSharePreviewFixture(t, dir)

	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/reviews" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"url":"https://crit.md/r/abc","delete_token":"t"}`))
	}))
	defer srv.Close()

	url, err := postPreviewShare(filepath.Join(dir, "index.html"), srv.URL, "", "", "")
	if err != nil {
		t.Fatalf("postPreviewShare: %v", err)
	}
	if url != "https://crit.md/r/abc" {
		t.Errorf("url = %q, want the stub URL", url)
	}

	if got["review_type"] != "preview" {
		t.Errorf("review_type = %v, want preview", got["review_type"])
	}
	wantPath := filepath.Join(dir, "index.html")
	cliArgs, ok := got["cli_args"].([]any)
	if !ok || len(cliArgs) != 2 || cliArgs[0] != "preview" || cliArgs[1] != wantPath {
		t.Errorf("cli_args = %v, want [preview %s]", got["cli_args"], wantPath)
	}

	files, ok := got["files"].([]any)
	if !ok || len(files) == 0 {
		t.Fatalf("files = %v, want non-empty slice", got["files"])
	}
	if !sharePreviewHasBase64(files) {
		t.Errorf("expected at least one file with encoding=base64, got %v", files)
	}
}

// writeSharePreviewFixture writes a minimal HTML page referencing a CSS file
// and a binary PNG, exercising both text and base64 crawl paths.
func writeSharePreviewFixture(t *testing.T, dir string) {
	t.Helper()
	html := `<!DOCTYPE html><html><head><link rel="stylesheet" href="style.css"></head>` +
		`<body><img src="logo.png"></body></html>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "style.css"), []byte("body{margin:0}"), 0o644); err != nil {
		t.Fatal(err)
	}
	png := []byte{137, 80, 78, 71, 13, 10, 26, 10}
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
}

// sharePreviewHasBase64 reports whether any decoded file entry carries
// encoding=base64.
func sharePreviewHasBase64(files []any) bool {
	for _, f := range files {
		if m, ok := f.(map[string]any); ok && m["encoding"] == "base64" {
			return true
		}
	}
	return false
}

// runSharePreviewStub runs `crit share --preview` against a stub crit-web and
// returns the decoded POST body plus anything written to stderr.
func runSharePreviewStub(t *testing.T, extraArgs ...string) (map[string]any, string, error) {
	t.Helper()
	testutil.SetHome(t, t.TempDir())
	t.Setenv("CRIT_AUTH_TOKEN", "")
	t.Setenv("CRIT_SHARE_URL", "")
	dir := t.TempDir()
	writeSharePreviewFixture(t, dir)

	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"url":"https://crit.md/r/abc","delete_token":"t"}`))
	}))
	defer srv.Close()

	args := append([]string{"--preview", filepath.Join(dir, "index.html"), "--share-url", srv.URL}, extraArgs...)

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = oldStderr })
	runErr := RunShare(args)
	_ = w.Close()
	os.Stderr = oldStderr
	stderr, _ := io.ReadAll(r)
	return got, string(stderr), runErr
}

func TestRunSharePreview_OrgAndVisibility(t *testing.T) {
	cases := []struct {
		name           string
		args           []string
		wantOrg        any
		wantVisibility any
	}{
		{"flags sent", []string{"--org", "acme", "--visibility", "public"}, "acme", "public"},
		{"omitted by default", nil, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := runSharePreviewStub(t, tc.args...)
			if err != nil {
				t.Fatalf("RunShare: %v", err)
			}
			if got["org"] != tc.wantOrg {
				t.Errorf("org = %v, want %v", got["org"], tc.wantOrg)
			}
			if got["visibility"] != tc.wantVisibility {
				t.Errorf("visibility = %v, want %v", got["visibility"], tc.wantVisibility)
			}
		})
	}
}

func TestRunSharePreview_PrintsQR(t *testing.T) {
	_, stderr, err := runSharePreviewStub(t, "--qr")
	if err != nil {
		t.Fatalf("RunShare: %v", err)
	}
	if !strings.ContainsAny(stderr, "█▀▄") {
		t.Errorf("stderr = %q, want a QR code", stderr)
	}
}

func TestRunSharePreview_RejectsReviewFileFlags(t *testing.T) {
	for _, flag := range []string{"--output", "-o", "--session"} {
		t.Run(flag, func(t *testing.T) {
			got, _, err := runSharePreviewStub(t, flag, "x")
			if err == nil || !strings.Contains(err.Error(), flag) {
				t.Fatalf("err = %v, want usage error naming %s", err, flag)
			}
			if got != nil {
				t.Errorf("share was posted despite %s: %v", flag, got)
			}
		})
	}
}

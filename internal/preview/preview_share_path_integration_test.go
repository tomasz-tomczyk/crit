//go:build integration

package preview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// Regression suite for #983: a preview share must keep the previewed HTML's
// original path for the uploaded artifact, the review title and og/twitter
// tags, and the comments — never collapse it to "index.html".

// TestShareSyncPreviewOriginalPath_CLI covers the standalone
// `crit share --preview <file>` path for the three path shapes the CLI sees.
func TestShareSyncPreviewOriginalPath_CLI(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)

	cases := []struct {
		name string
		// arg builds the --preview argument from the working dir and the HTML's
		// absolute path.
		arg func(cwd, abs string) string
		// htmlDir is where the fixture lives: inside cwd (true) or elsewhere.
		insideCwd bool
		want      string
	}{
		{
			name:      "relative nested path",
			arg:       func(_, _ string) string { return filepath.Join("artifacts", "reports", "docs-minimize.html") },
			insideCwd: true,
			want:      "artifacts/reports/docs-minimize.html",
		},
		{
			name:      "absolute path under cwd",
			arg:       func(_, abs string) string { return abs },
			insideCwd: true,
			want:      "artifacts/reports/docs-minimize.html",
		},
		{
			name:      "path outside cwd falls back to basename",
			arg:       func(_, abs string) string { return abs },
			insideCwd: false,
			want:      "docs-minimize.html",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			siteRoot := cwd
			if !tc.insideCwd {
				siteRoot = t.TempDir()
			}
			abs := writeNamedPreviewFixture(t, filepath.Join(siteRoot, "artifacts", "reports"), "docs-minimize.html")

			cmd := exec.Command(binary, "share", "--share-url", baseURL, "--output", cwd, "--preview", tc.arg(cwd, abs))
			cmd.Dir = cwd
			cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("crit share --preview failed: %s\n%s", err, out)
			}
			token := extractToken(t, strings.TrimSpace(string(out)))
			t.Logf("  → Review: %s", extractURL(t, string(out)))

			assertPreviewArtifacts(t, baseURL, token, tc.want)
			assertPreviewTitle(t, baseURL, token, tc.want)
		})
	}
}

// TestShareSyncPreviewOriginalPath_SessionShareAndReshare covers the in-app
// Share button (POST /api/share) and re-share (POST /api/share/reshare) of a
// live preview session: artifact path, title, and comment round-trip — local
// comments go up on the entry path, a comment added on crit-web comes back on
// pull and survives the re-share upsert.
func TestShareSyncPreviewOriginalPath_SessionShareAndReshare(t *testing.T) {
	baseURL := critWebURL(t)
	testutil.SetHome(t, t.TempDir())
	t.Setenv("CRIT_AUTH_TOKEN", "")

	cwd := t.TempDir()
	const entry = "artifacts/reports/gutter-icons-inline.html"
	abs := writeNamedPreviewFixture(t, filepath.Join(cwd, "artifacts", "reports"), "gutter-icons-inline.html")
	t.Chdir(cwd)

	// Local comments, stored the way a live preview session stores them: a DOM
	// pin on the live-route entry (/preview-content) and a line comment on the
	// HTML's session path.
	reviewPath := filepath.Join(cwd, "review.json")
	if err := saveCritJSON(reviewPath, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"/preview-content": {Comments: []Comment{{
				ID: "c1", Body: "pin on the heading", Author: "Alice",
				DOMAnchor: &DOMAnchor{Pathname: "/preview-content", CSSSelector: "#title"},
			}}},
			entry: {Comments: []Comment{{
				ID: "c2", StartLine: 6, EndLine: 6, Body: "line comment on the title tag", Author: "Alice", Scope: "line",
			}}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	sess, err := createPreviewSession(&serverConfig{previewFile: abs, reviewPath: reviewPath})
	if err != nil {
		t.Fatal(err)
	}
	sess.InitTestChannels()
	s, err := NewServer(sess, frontendFS, baseURL, false, "", "Alice", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	s.SetSession(sess)

	// --- first share ---
	res := serveJSON(t, s, http.MethodPost, "/api/share")
	shareURL, _ := res["url"].(string)
	if shareURL == "" {
		t.Fatalf("share returned no url: %v", res)
	}
	token := filepath.Base(shareURL)
	t.Logf("  → Review: %s", shareURL)

	assertPreviewArtifacts(t, baseURL, token, entry)
	assertPreviewTitle(t, baseURL, token, entry)

	comments := previewCommentsFromAPI(t, baseURL, token)
	if len(comments) != 2 {
		t.Fatalf("expected 2 shared comments, got %d: %+v", len(comments), comments)
	}
	for _, c := range comments {
		if c.FilePath != entry {
			t.Errorf("shared comment %q file = %q, want %q", c.Body, c.FilePath, entry)
		}
	}

	// --- a reviewer comments on crit-web, then the author re-shares ---
	seedPreviewComment(t, baseURL, token, entry, "web reviewer comment")

	res = serveJSON(t, s, http.MethodPost, "/api/share/reshare")
	if merged, _ := res["merged"].(float64); merged != 1 {
		t.Errorf("reshare merged = %v, want 1 (the web comment); body=%v", res["merged"], res)
	}

	// The pulled web comment is stored locally under the entry path...
	data, err := os.ReadFile(session.ReviewPathsFor(sess.CritJSONPath()).Review)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("web reviewer comment")) {
		t.Errorf("pulled web comment not stored locally:\n%s", data)
	}

	// ...and the re-share upsert keeps all three comments on the entry path,
	// without introducing an index.html artifact.
	comments = previewCommentsFromAPI(t, baseURL, token)
	if len(comments) != 3 {
		t.Fatalf("expected 3 comments after reshare, got %d: %+v", len(comments), comments)
	}
	for _, c := range comments {
		if c.FilePath != entry {
			t.Errorf("comment %q file = %q after reshare, want %q", c.Body, c.FilePath, entry)
		}
	}
	assertPreviewArtifacts(t, baseURL, token, entry)
	assertPreviewTitle(t, baseURL, token, entry)
}

// writeNamedPreviewFixture copies the test/fixtures/preview fixture into dir,
// renaming index.html to name, and returns the HTML's absolute path.
func writeNamedPreviewFixture(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"index.html", "style.css", "app.js", "logo.png"} {
		dst := f
		if f == "index.html" {
			dst = name
		}
		if err := os.WriteFile(filepath.Join(dir, dst), readPreviewFixture(t, f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	abs, err := filepath.Abs(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// assertPreviewArtifacts checks the stored files: the entry HTML first at its
// original path, assets alongside it, no "index.html", and both the entry and
// its relative assets served from those paths by the raw endpoint.
func assertPreviewArtifacts(t *testing.T, baseURL, token, entry string) {
	t.Helper()
	dir := filepath.ToSlash(filepath.Dir(entry))
	prefix := ""
	if dir != "." {
		prefix = dir + "/"
	}

	files := previewDocumentFiles(t, baseURL, token)
	if len(files) == 0 {
		t.Fatal("document has no files")
	}
	if files[0] != entry {
		t.Errorf("entry artifact path = %q, want %q (all: %v)", files[0], entry, files)
	}
	for _, p := range files {
		if p == "index.html" {
			t.Errorf("artifact stored as index.html (all: %v)", files)
		}
	}
	for _, asset := range []string{"style.css", "logo.png"} {
		want := prefix + asset
		found := false
		for _, p := range files {
			found = found || p == want
		}
		if !found {
			t.Errorf("asset %q missing (all: %v)", want, files)
		}
	}

	body, _ := getPreviewRaw(t, baseURL, token, entry)
	if !bytes.Contains(body, []byte("/preview-agent/crit-agent.js")) {
		t.Errorf("entry HTML at %s missing injected agent script", entry)
	}
	if !bytes.Contains(body, []byte(`href="style.css"`)) {
		t.Errorf("entry HTML at %s is not the fixture HTML", entry)
	}
	// The HTML's relative refs resolve against its own URL, so the assets
	// must be served next to it.
	css, _ := getPreviewRaw(t, baseURL, token, prefix+"style.css")
	if !textBodyMatches(css, readPreviewFixture(t, "style.css")) {
		t.Errorf("%sstyle.css body mismatch", prefix)
	}
	png, _ := getPreviewRaw(t, baseURL, token, prefix+"logo.png")
	if !bytes.Equal(png, readPreviewFixture(t, "logo.png")) {
		t.Errorf("%slogo.png body mismatch", prefix)
	}
}

var (
	titleRe = regexp.MustCompile(`(?s)<title[^>]*>(.*?)</title>`)
	metaRe  = regexp.MustCompile(`<meta[^>]+(?:property|name)="(og:title|twitter:title)"[^>]+content="([^"]*)"`)
)

// assertPreviewTitle checks the review page's <title> and og/twitter titles
// all carry the entry path.
func assertPreviewTitle(t *testing.T, baseURL, token, entry string) {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/r/%s", baseURL, token))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("review page returned %d", resp.StatusCode)
	}
	page, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	m := titleRe.FindSubmatch(page)
	if m == nil || !bytes.Contains(m[1], []byte(entry)) {
		t.Errorf("<title> = %q, want it to contain %q", m, entry)
	}
	seen := map[string]string{}
	for _, mm := range metaRe.FindAllSubmatch(page, -1) {
		seen[string(mm[1])] = string(mm[2])
	}
	for _, key := range []string{"og:title", "twitter:title"} {
		if got, ok := seen[key]; !ok || !strings.Contains(got, entry) {
			t.Errorf("%s = %q, want it to contain %q", key, got, entry)
		}
	}
}

func previewDocumentFiles(t *testing.T, baseURL, token string) []string {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/api/reviews/%s/document", baseURL, token))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("document returned %d", resp.StatusCode)
	}
	var body struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	paths := make([]string, len(body.Files))
	for i, f := range body.Files {
		paths[i] = f.Path
	}
	return paths
}

type previewWebComment struct {
	Body     string `json:"body"`
	FilePath string `json:"file_path"`
}

func previewCommentsFromAPI(t *testing.T, baseURL, token string) []previewWebComment {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/api/reviews/%s/comments", baseURL, token))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("comments returned %d", resp.StatusCode)
	}
	var comments []previewWebComment
	if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
		t.Fatal(err)
	}
	return comments
}

func seedPreviewComment(t *testing.T, baseURL, token, file, body string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"file": file, "start_line": 1, "end_line": 1, "body": body})
	resp, err := http.Post(baseURL+"/api/reviews/"+token+"/seed-comment", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed-comment returned %d", resp.StatusCode)
	}
}

// serveJSON sends an in-process request to the crit server and decodes the
// JSON response, failing on a non-200 status.
func serveJSON(t *testing.T, s *Server, method, target string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s: status %d, body %s", method, target, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s %s: decode: %v (%s)", method, target, err, rec.Body.String())
	}
	return out
}

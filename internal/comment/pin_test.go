package comment

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/review"
)

func seedPinReview(t *testing.T, dir string, cj CritJSON) {
	t.Helper()
	critPath, err := review.ResolveReviewPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
	}
}

func TestRunCommentSelectorAddsPreviewPin(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "index.html")
	page := `<!doctype html><html><body><main><h1 id="title">Hello</h1></main></body></html>`
	if err := os.WriteFile(htmlPath, []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	seedPinReview(t, dir, CritJSON{
		ReviewType:  "preview",
		Origin:      htmlPath,
		ReviewRound: 1,
		Files:       map[string]CritJSONFile{},
	})

	err := RunComment([]string{"--output", dir, "--author", "bot", "--selector", "h1", "Heading is vague"})
	if err != nil {
		t.Fatal(err)
	}
	loaded := loadOutputReview(t, dir)
	pins := loaded.Files["/preview-content"].Comments
	if len(pins) != 1 {
		t.Fatalf("pins = %+v", loaded.Files)
	}
	pin := pins[0]
	if pin.Body != "Heading is vague" || pin.Author != "bot" || pin.PinNumber != 1 {
		t.Fatalf("pin = %+v", pin)
	}
	if pin.DOMAnchor == nil || pin.DOMAnchor.CSSSelector != "h1" || pin.DOMAnchor.Pathname != "/preview-content" {
		t.Fatalf("anchor = %+v", pin.DOMAnchor)
	}
	if got := pin.DOMAnchor.TagChain; len(got) == 0 || got[len(got)-1] != "H1" {
		t.Fatalf("tag chain = %v", got)
	}
	if pin.DOMAnchor.AccessibleName != "Hello" || pin.DOMAnchor.Role != "heading" || pin.DOMAnchor.Landmark != "main" {
		t.Fatalf("anchor fields = %+v", pin.DOMAnchor)
	}
	if pin.StartLine != 0 || pin.EndLine != 0 {
		t.Fatalf("pin should not use line numbers: %+v", pin)
	}
}

func TestRunCommentSelectorPreviewSubpage(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "chapters"), 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, "index.html")
	chapter := filepath.Join(dir, "chapters", "01.html")
	if err := os.WriteFile(entry, []byte(`<html><body><h1>Index</h1></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chapter, []byte(`<html><body><p id="lead">Chapter</p></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	seedPinReview(t, dir, CritJSON{
		ReviewType:  "preview",
		Origin:      entry,
		ReviewRound: 1,
		Files:       map[string]CritJSONFile{},
	})

	err := RunComment([]string{"--output", dir, "--selector", "#lead", "--route", "/chapters/01.html", "Open stronger"})
	if err != nil {
		t.Fatal(err)
	}
	loaded := loadOutputReview(t, dir)
	pins := loaded.Files["/preview-content/chapters/01.html"].Comments
	if len(pins) != 1 || pins[0].DOMAnchor == nil || pins[0].DOMAnchor.Pathname != "/preview-content/chapters/01.html" {
		t.Fatalf("pins = %+v", loaded.Files)
	}
	if pins[0].DOMAnchor.TagChain[len(pins[0].DOMAnchor.TagChain)-1] != "P" {
		t.Fatalf("tag chain = %v", pins[0].DOMAnchor.TagChain)
	}
}

func TestRunCommentSelectorPreviewMissingElement(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(htmlPath, []byte(`<html><body><h1>Hi</h1></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	seedPinReview(t, dir, CritJSON{ReviewType: "preview", Origin: htmlPath, ReviewRound: 1, Files: map[string]CritJSONFile{}})

	err := RunComment([]string{"--output", dir, "--selector", "#missing", "nope"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v", err)
	}
	loaded := loadOutputReview(t, dir)
	if len(loaded.Files) != 0 {
		t.Fatalf("review should be unchanged, got %+v", loaded.Files)
	}
}

func TestRunCommentSelectorRejectsCodeReview(t *testing.T) {
	dir := t.TempDir()
	seedPinReview(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{}})
	err := RunComment([]string{"--output", dir, "--selector", "h1", "nope"})
	if err == nil || !strings.Contains(err.Error(), "live and preview") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunCommentLineLevelRejectsPreviewReview(t *testing.T) {
	dir := t.TempDir()
	seedPinReview(t, dir, CritJSON{ReviewType: "preview", ReviewRound: 1, Files: map[string]CritJSONFile{}})
	err := RunComment([]string{"--output", dir, "index.html:1", "a line comment"})
	if err == nil || !strings.Contains(err.Error(), "not supported for preview reviews") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunCommentSelectorLiveWithoutFetch(t *testing.T) {
	dir := t.TempDir()
	seedPinReview(t, dir, CritJSON{ReviewType: "live", ReviewRound: 1, Files: map[string]CritJSONFile{}})
	err := RunComment([]string{"--output", dir, "--selector", "button.primary", "--route", "/dashboard", "Label is missing"})
	if err != nil {
		t.Fatal(err)
	}
	loaded := loadOutputReview(t, dir)
	pins := loaded.Files["/dashboard"].Comments
	if len(pins) != 1 {
		t.Fatalf("pins = %+v", loaded.Files)
	}
	if pins[0].DOMAnchor == nil || pins[0].DOMAnchor.CSSSelector != "button.primary" {
		t.Fatalf("anchor = %+v", pins[0].DOMAnchor)
	}
	if len(pins[0].DOMAnchor.TagChain) != 1 || pins[0].DOMAnchor.TagChain[0] != "BUTTON" {
		t.Fatalf("tag chain = %v", pins[0].DOMAnchor.TagChain)
	}
}

func TestRunCommentSelectorLiveFetchesPage(t *testing.T) {
	page := `<!doctype html><html><body><button id="go">Go</button></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dashboard" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	seedPinReview(t, dir, CritJSON{
		ReviewType:  "live",
		Origin:      srv.URL,
		ReviewRound: 1,
		Files:       map[string]CritJSONFile{},
	})
	err := RunComment([]string{"--output", dir, "--selector", "#go", "--route", "/dashboard", "Needs a label"})
	if err != nil {
		t.Fatal(err)
	}
	loaded := loadOutputReview(t, dir)
	pin := loaded.Files["/dashboard"].Comments[0]
	if pin.DOMAnchor.AccessibleName != "Go" || pin.DOMAnchor.Role != "button" {
		t.Fatalf("anchor = %+v", pin.DOMAnchor)
	}
	if pin.DOMAnchor.TagChain[len(pin.DOMAnchor.TagChain)-1] != "BUTTON" {
		t.Fatalf("tag chain = %v", pin.DOMAnchor.TagChain)
	}
}

func TestRunCommentSelectorIncrementsPinNumber(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(htmlPath, []byte(`<html><body><h1>A</h1><h2>B</h2></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	seedPinReview(t, dir, CritJSON{
		ReviewType:  "preview",
		Origin:      htmlPath,
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"/preview-content": {Status: "added", Comments: []Comment{{
				ID: "c_existing", PinNumber: 4, Body: "old",
				DOMAnchor: &DOMAnchor{Pathname: "/preview-content", CSSSelector: "h1"},
			}}},
		},
	})
	if err := RunComment([]string{"--output", dir, "--selector", "h2", "Second heading"}); err != nil {
		t.Fatal(err)
	}
	loaded := loadOutputReview(t, dir)
	var found bool
	for _, c := range loaded.Files["/preview-content"].Comments {
		if c.Body == "Second heading" {
			found = true
			if c.PinNumber != 5 {
				t.Fatalf("pin number = %d", c.PinNumber)
			}
		}
	}
	if !found {
		t.Fatalf("comments = %+v", loaded.Files["/preview-content"].Comments)
	}
}

func TestRunCommentJSONSelectorOnPreview(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(htmlPath, []byte(`<html><body><h1>Hi</h1></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	seedPinReview(t, dir, CritJSON{ReviewType: "preview", Origin: htmlPath, ReviewRound: 1, Files: map[string]CritJSONFile{}})
	jsonPath := filepath.Join(dir, "bulk.json")
	payload := `[{"selector":"h1","body":"from json"}]`
	if err := os.WriteFile(jsonPath, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunComment([]string{"--json", "--file", jsonPath, "--output", dir, "--author", "bot"}); err != nil {
		t.Fatal(err)
	}
	loaded := loadOutputReview(t, dir)
	pins := loaded.Files["/preview-content"].Comments
	if len(pins) != 1 || pins[0].Body != "from json" || pins[0].DOMAnchor == nil {
		t.Fatalf("pins = %+v", loaded.Files)
	}
}

func TestLeafTagFromSelector(t *testing.T) {
	cases := []struct {
		sel  string
		want string
	}{
		{"h1", "H1"},
		{"button.primary", "BUTTON"},
		{"#main > h2:nth-of-type(1)", "H2"},
		{"body > main > h1", "H1"},
		{"#cta", ""},
		{".hero", ""},
	}
	for _, tt := range cases {
		if got := leafTagFromSelector(tt.sel); got != tt.want {
			t.Errorf("leafTagFromSelector(%q) = %q, want %q", tt.sel, got, tt.want)
		}
	}
}

func TestPreviewRouteEscapeRejected(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(htmlPath, []byte(`<html><body><h1>Hi</h1></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	seedPinReview(t, dir, CritJSON{ReviewType: "preview", Origin: htmlPath, ReviewRound: 1, Files: map[string]CritJSONFile{}})

	err := RunComment([]string{"--output", dir, "--selector", "h1", "--route", "/../secret.html", "nope"})
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunCommentJSONLineRejectedOnPreview(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(htmlPath, []byte(`<html><body><h1>Hi</h1></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	seedPinReview(t, dir, CritJSON{ReviewType: "preview", Origin: htmlPath, ReviewRound: 1, Files: map[string]CritJSONFile{}})
	jsonPath := filepath.Join(dir, "bulk.json")
	if err := os.WriteFile(jsonPath, []byte(`[{"file":"index.html","line":1,"body":"line"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := RunComment([]string{"--json", "--file", jsonPath, "--output", dir})
	if err == nil || !strings.Contains(err.Error(), "selector is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestNormalizePinRoute(t *testing.T) {
	if got := normalizePinRoute("preview", "", ""); got != "/preview-content" {
		t.Errorf("preview default = %q", got)
	}
	if got := normalizePinRoute("preview", "/chapters/01.html", ""); got != "/preview-content/chapters/01.html" {
		t.Errorf("preview relative = %q", got)
	}
	if got := normalizePinRoute("preview", "/preview-content/a.html", ""); got != "/preview-content/a.html" {
		t.Errorf("preview absolute = %q", got)
	}
	if got := normalizePinRoute("live", "", "http://localhost:3000/dashboard"); got != "/dashboard" {
		t.Errorf("live origin path = %q", got)
	}
	if got := normalizePinRoute("live", "/settings/", ""); got != "/settings" {
		t.Errorf("live route = %q", got)
	}
}

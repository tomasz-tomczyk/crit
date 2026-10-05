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
	"golang.org/x/net/html"
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
	if got := normalizePinRoute("live", "", "http://localhost:3000/dashboard?tab=1#section"); got != "/dashboard" {
		t.Errorf("live origin query = %q", got)
	}
	if got := normalizePinRoute("live", "", "http://localhost:3000/"); got != "/" {
		t.Errorf("live origin root = %q", got)
	}
	if got := normalizePinRoute("live", "", ""); got != "/" {
		t.Errorf("live empty origin = %q", got)
	}
	if got := normalizePinRoute("live", "settings", ""); got != "/settings" {
		t.Errorf("live relative = %q", got)
	}
	if got := normalizePinRoute("live", "/settings/", ""); got != "/settings" {
		t.Errorf("live route = %q", got)
	}
}

func TestAppendPinRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		cj   *CritJSON
		sel  string
		body string
		want string
	}{
		{"code review", &CritJSON{ReviewType: "diff"}, "h1", "body", "only supported"},
		{"empty selector", &CritJSON{ReviewType: "live"}, "  ", "body", "selector is required"},
		{"empty body", &CritJSON{ReviewType: "live"}, "h1", "  ", "body is required"},
		{"no preview file", &CritJSON{ReviewType: "preview"}, "h1", "body", "no HTML file"},
		{"bad selector", &CritJSON{ReviewType: "live"}, ">>>", "body", "invalid selector"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := appendPin(tt.cj, tt.sel, "/", tt.body, "bot", "u1")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestAppendPinUsesCliArgsOriginAndDefaultsRound(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "index.html")
	page := `<html><body><main aria-label="Primary"><button role="tab" aria-label="` + strings.Repeat("Save ", 30) + `"><style>.x{}</style>Go</button></main></body></html>`
	if err := os.WriteFile(htmlPath, []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	cj := &CritJSON{ReviewType: "preview", CliArgs: []string{"preview", htmlPath}}
	c, err := appendPin(cj, "button", "", "label", "bot", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if c.ReviewRound != 1 {
		t.Fatalf("round = %d", c.ReviewRound)
	}
	if c.DOMAnchor.Role != "tab" || c.DOMAnchor.Landmark != "Primary" {
		t.Fatalf("anchor = %+v", c.DOMAnchor)
	}
	if len([]rune(c.DOMAnchor.AccessibleName)) != 80 {
		t.Fatalf("name len = %d (%q)", len([]rune(c.DOMAnchor.AccessibleName)), c.DOMAnchor.AccessibleName)
	}
	if cj.Files == nil {
		t.Fatal("files map was not created")
	}
}

func TestPreviewPinMissingFile(t *testing.T) {
	dir := t.TempDir()
	seedPinReview(t, dir, CritJSON{
		ReviewType:  "preview",
		Origin:      filepath.Join(dir, "missing.html"),
		ReviewRound: 1,
		Files:       map[string]CritJSONFile{},
	})
	err := RunComment([]string{"--output", dir, "--selector", "h1", "nope"})
	if err == nil || !strings.Contains(err.Error(), "reading preview file") {
		t.Fatalf("error = %v", err)
	}
}

func TestPinFlagConflicts(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--selector"}, "requires a value"},
		{[]string{"--route"}, "requires a value"},
		{[]string{"--selector", "h1", "--json", "body"}, "--selector and --json"},
		{[]string{"--selector", "h1", "--reply-to", "c1", "body"}, "--selector and --reply-to"},
		{[]string{"--route", "/dashboard", "body"}, "--route requires --selector"},
	}
	for _, tt := range cases {
		err := RunComment(tt.args)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("RunComment(%q) = %v, want %q", tt.args, err, tt.want)
		}
	}
}

func TestSelectorCommentRequiresBody(t *testing.T) {
	dir := t.TempDir()
	seedPinReview(t, dir, CritJSON{ReviewType: "live", ReviewRound: 1, Files: map[string]CritJSONFile{}})
	err := RunComment([]string{"--output", dir, "--selector", "h1"})
	if err == nil || !strings.Contains(err.Error(), "body is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestAddPinCommentRejectsInvalidReview(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(htmlPath, []byte(`<html><body><h1>Hi</h1></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	seedPinReview(t, dir, CritJSON{ReviewType: "preview", Origin: htmlPath, ReviewRound: 1, Files: map[string]CritJSONFile{}})
	critPath, err := review.ResolveReviewPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	reviewFile := review.ReviewPathsFor(critPath).Review
	if err := os.WriteFile(reviewFile, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := addPinComment(critPath, "h1", "/", "body", "bot", "u1"); err == nil {
		t.Fatal("expected invalid review file")
	}
}

func TestPinBulkEntryRejections(t *testing.T) {
	cj := &CritJSON{ReviewType: "preview", ReviewRound: 1, Files: map[string]CritJSONFile{}}
	cases := []struct {
		e    BulkCommentEntry
		want string
	}{
		{BulkCommentEntry{ReplyTo: "c1", Selector: "h1", Body: "x"}, "reply_to"},
		{BulkCommentEntry{ReplyTo: "missing", Body: "x"}, "entry 0"},
		{BulkCommentEntry{Selector: "h1", File: "a.go", Body: "x"}, "file or line"},
		{BulkCommentEntry{Selector: "h1", Quote: "quoted", Body: "x"}, "quote"},
		{BulkCommentEntry{CSSSelector: "h1", Pathname: "/", Body: "  "}, "body is required"},
	}
	for _, tt := range cases {
		err := processBulkEntry(cj, 0, tt.e, "bot", "u1", inheritedScope{})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("entry %+v error = %v, want %q", tt.e, err, tt.want)
		}
	}
}

func TestJSONPinUsesSelectorAliases(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(htmlPath, []byte(`<html><body><h1>Hi</h1></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	seedPinReview(t, dir, CritJSON{ReviewType: "preview", Origin: htmlPath, ReviewRound: 1, Files: map[string]CritJSONFile{}})
	jsonPath := filepath.Join(dir, "bulk.json")
	payload := `[{"css_selector":"h1","pathname":"/","body":"aliased"}]`
	if err := os.WriteFile(jsonPath, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunComment([]string{"--json", "--file", jsonPath, "--output", dir}); err != nil {
		t.Fatal(err)
	}
	loaded := loadOutputReview(t, dir)
	pins := loaded.Files["/preview-content"].Comments
	if len(pins) != 1 || pins[0].Body != "aliased" {
		t.Fatalf("pins = %+v", loaded.Files)
	}
}

func TestLeafTagFromSelectorEdges(t *testing.T) {
	cases := []struct {
		sel  string
		want string
	}{
		{"", ""},
		{"*", ""},
		{"::before", ""},
		{`button[title="a:b"]`, "BUTTON"},
		{`div > button[title="a > b"]`, "BUTTON"},
		{"div > @foo", ""},
	}
	for _, tt := range cases {
		if got := leafTagFromSelector(tt.sel); got != tt.want {
			t.Errorf("leafTagFromSelector(%q) = %q, want %q", tt.sel, got, tt.want)
		}
	}
	if err := fillAnchorFromHTML(&DOMAnchor{CSSSelector: ">>>"}, []byte("<html></html>")); err == nil {
		t.Fatal("expected invalid selector")
	}
	if err := enrichPinAnchor(&CritJSON{ReviewType: "diff"}, &DOMAnchor{CSSSelector: "h1"}); err == nil {
		t.Fatal("expected non-pin review to be rejected")
	}
	live := &CritJSON{ReviewType: "live", Files: map[string]CritJSONFile{}}
	if err := processBulkEntry(live, 0, BulkCommentEntry{Selector: "button", Route: "/dash", Body: "ok"}, "bot", "u1", inheritedScope{}); err != nil {
		t.Fatal(err)
	}
	anchor := &DOMAnchor{CSSSelector: "p"}
	if err := fillAnchorFromHTML(anchor, []byte(`<html><body><p>Hi<script>nope</script></p></body></html>`)); err != nil {
		t.Fatal(err)
	}
	if anchor.AccessibleName != "Hi" {
		t.Fatalf("name = %q", anchor.AccessibleName)
	}
	if renderNode(&html.Node{Type: html.ErrorNode}) != "" {
		t.Fatal("error nodes should not render")
	}
}

func TestReadLivePageHTMLDefault(t *testing.T) {
	htmlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plain":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "nope")
		case "/missing":
			http.NotFound(w, r)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/away":
			http.Redirect(w, r, "https://example.com/", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html><body><h1>Hi</h1></body></html>")
		}
	}))
	t.Cleanup(htmlSrv.Close)

	if _, err := readLivePageHTMLDefault(htmlSrv.URL, "/plain"); err == nil || !strings.Contains(err.Error(), "content type") {
		t.Fatalf("plain = %v", err)
	}
	if _, err := readLivePageHTMLDefault(htmlSrv.URL, "/missing"); err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("missing = %v", err)
	}
	if _, err := readLivePageHTMLDefault(htmlSrv.URL, "/loop"); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("loop = %v", err)
	}
	if _, err := readLivePageHTMLDefault(htmlSrv.URL, "/away"); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("away = %v", err)
	}
	if _, err := readLivePageHTMLDefault("http://127.0.0.1:1", "/"); err == nil {
		t.Fatal("expected connection error")
	}
	if _, err := readLivePageHTMLDefault("ftp://example.com/x", "/"); err == nil {
		t.Fatal("expected non-http origin error")
	}
	if _, err := livePageURL("http://example.com", "%zz"); err == nil {
		t.Fatal("expected bad route")
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var lastDispatch string

func testRunDesign(args []string)  { lastDispatch = "design" }
func testRunReview_(args []string) { lastDispatch = "review" }

func dispatchForTest(args []string, designFn, reviewFn func([]string)) {
	if looksLikeDesignArgs(args) {
		designFn(args)
	} else {
		reviewFn(args)
	}
}

func TestDispatch_ExplicitDesign(t *testing.T) {
	lastDispatch = ""
	dispatchForTest([]string{"http://localhost:3000"}, testRunDesign, testRunReview_)
	if lastDispatch != "design" {
		t.Errorf("dispatch = %q, want design", lastDispatch)
	}
}

func TestDispatch_HTTPSAutodetect(t *testing.T) {
	lastDispatch = ""
	dispatchForTest([]string{"https://myapp.test:4000/dashboard"}, testRunDesign, testRunReview_)
	if lastDispatch != "design" {
		t.Errorf("dispatch = %q, want design", lastDispatch)
	}
}

func TestDispatch_URLPlusFile_NotDesign(t *testing.T) {
	lastDispatch = ""
	dispatchForTest([]string{"http://localhost:3000", "./README.md"}, testRunDesign, testRunReview_)
	if lastDispatch != "review" {
		t.Errorf("dispatch = %q, want review (URL+file must not autodetect)", lastDispatch)
	}
}

func TestDispatch_FTPNotDesign(t *testing.T) {
	lastDispatch = ""
	dispatchForTest([]string{"ftp://foo.bar"}, testRunDesign, testRunReview_)
	if lastDispatch != "review" {
		t.Errorf("dispatch = %q, want review (ftp not autodetected)", lastDispatch)
	}
}

func TestDispatch_PlainArgNotDesign(t *testing.T) {
	lastDispatch = ""
	dispatchForTest([]string{"README.md"}, testRunDesign, testRunReview_)
	if lastDispatch != "review" {
		t.Errorf("dispatch = %q, want review", lastDispatch)
	}
}

// Suppress unused import warnings — additional tests in later tasks use these.
var _ = json.Marshal
var _ = fmt.Sprintf
var _ = net.Listen
var _ = http.MethodGet
var _ = httptest.NewServer
var _ = url.Parse
var _ = os.Exit
var _ = filepath.Join
var _ = strings.Contains
var _ = time.Second

func TestSmokeTest_ConnectionRefused(t *testing.T) {
	r := runSmokeTest("http://127.0.0.1:19999")
	if r.kind != smokeConnRefused {
		t.Errorf("kind = %v, want smokeConnRefused", r.kind)
	}
	if !r.fatal {
		t.Error("conn refused should be fatal")
	}
}

func TestSmokeTest_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "auth required", http.StatusUnauthorized)
	}))
	defer srv.Close()
	r := runSmokeTest(srv.URL)
	if r.kind != smokeNon2xx {
		t.Errorf("kind = %v, want smokeNon2xx", r.kind)
	}
	if r.fatal {
		t.Error("non-2xx should warn, not be fatal")
	}
}

func TestSmokeTest_NonHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"ok":true}`)
	}))
	defer srv.Close()
	r := runSmokeTest(srv.URL)
	if r.kind != smokeNonHTML {
		t.Errorf("kind = %v, want smokeNonHTML", r.kind)
	}
	if !r.fatal {
		t.Error("non-HTML should be fatal")
	}
}

func TestSmokeTest_MissingBodyTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><head></head><!-- no closing body -->")
	}))
	defer srv.Close()
	r := runSmokeTest(srv.URL)
	if r.kind != smokeMissingBody {
		t.Errorf("kind = %v, want smokeMissingBody", r.kind)
	}
	if r.fatal {
		t.Error("missing </body> should warn, not be fatal")
	}
}

func TestSmokeTest_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><body><p>hello</p></body></html>")
	}))
	defer srv.Close()
	r := runSmokeTest(srv.URL)
	if r.kind != smokeOK {
		t.Errorf("kind = %v, want smokeOK", r.kind)
	}
	if r.fatal {
		t.Error("OK should not be fatal")
	}
}

func TestSmokeTest_CSPFrameAncestors_Informational(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		fmt.Fprintln(w, "<html><body>app</body></html>")
	}))
	defer srv.Close()
	r := runSmokeTest(srv.URL)
	if r.kind != smokeOK {
		t.Errorf("kind = %v, want smokeOK (CSP stripped by proxy)", r.kind)
	}
	if !r.hasCSPFrameAncestors {
		t.Error("hasCSPFrameAncestors should be true")
	}
}

func TestShareGuard_DesignReview(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, "review")
	cj := CritJSON{ReviewType: "design", Origin: "http://localhost:3000", ReviewRound: 1, Files: map[string]CritJSONFile{}}
	if err := saveCritJSON(critPath, cj); err != nil {
		t.Fatalf("saveCritJSON: %v", err)
	}
	err := checkShareAllowed(critPath)
	if err == nil {
		t.Fatal("expected error for design review share")
	}
	if !strings.Contains(err.Error(), "design") {
		t.Errorf("error should mention design: %v", err)
	}
}

func TestShareGuard_CodeReview_Allowed(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, "review")
	cj := CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{}}
	if err := saveCritJSON(critPath, cj); err != nil {
		t.Fatalf("saveCritJSON: %v", err)
	}
	if err := checkShareAllowed(critPath); err != nil {
		t.Errorf("code review should be shareable: %v", err)
	}
}

func TestCommentCLIGuard_DesignReview(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, "review")
	cj := CritJSON{ReviewType: "design", Origin: "http://localhost:3000", ReviewRound: 1, Files: map[string]CritJSONFile{}}
	if err := saveCritJSON(critPath, cj); err != nil {
		t.Fatalf("saveCritJSON: %v", err)
	}
	err := checkCommentCLIAllowed(critPath)
	if err == nil {
		t.Fatal("expected error for design review")
	}
	if !strings.Contains(err.Error(), "design") {
		t.Errorf("error should mention design: %v", err)
	}
}

func TestCommentCLIGuard_CodeReview_Allowed(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, "review")
	cj := CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{}}
	if err := saveCritJSON(critPath, cj); err != nil {
		t.Fatalf("saveCritJSON: %v", err)
	}
	if err := checkCommentCLIAllowed(critPath); err != nil {
		t.Errorf("code review should allow crit comment: %v", err)
	}
}

func TestCarryForward_DesignPinSkipsRemap(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "page.md")
	writeFile(t, mdPath, "# Page\n\nNew content\n")

	s := &Session{
		Mode:          "files",
		RepoRoot:      dir,
		ReviewRound:   2,
		subscribers:   make(map[chan SSEEvent]struct{}),
		roundComplete: make(chan struct{}, 1),
	}
	fe := &FileEntry{
		Path:            "page.md",
		AbsPath:         mdPath,
		FileType:        "markdown",
		Content:         "# Page\n\nNew content\n",
		PreviousContent: "# Page\n\nOld content\n",
		PreviousComments: []Comment{
			{
				ID: "pin1", StartLine: 0, EndLine: 0, Body: "pin",
				DOMAnchor: &DOMAnchor{Pathname: "/page.md", CSSSelector: "#h1"},
			},
			{ID: "code1", StartLine: 3, EndLine: 3, Body: "code"},
		},
	}
	s.Files = []*FileEntry{fe}
	s.carryForwardFileComments(fe)

	s.mu.RLock()
	defer s.mu.RUnlock()
	found := false
	for _, c := range fe.Comments {
		if c.DOMAnchor != nil {
			found = true
			if c.StartLine != 0 || c.EndLine != 0 {
				t.Errorf("design pin lines remapped to %d/%d; should stay 0/0", c.StartLine, c.EndLine)
			}
		}
	}
	if !found {
		t.Error("design pin not carried forward")
	}
}

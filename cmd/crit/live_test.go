package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAddLivePin_AssignsMonotonicGlobalPinNumbers(t *testing.T) {
	s := newTestSession(t)
	a1 := &DOMAnchor{Pathname: "/foo", CSSSelector: "h1", TagChain: []string{"H1"}}
	a2 := &DOMAnchor{Pathname: "/bar", CSSSelector: "h2", TagChain: []string{"H2"}}
	a3 := &DOMAnchor{Pathname: "/foo", CSSSelector: "h3", TagChain: []string{"H3"}}

	c1, ok := s.AddLivePin("/foo", "first", "alice", "u1", a1)
	if !ok || c1.PinNumber != 1 {
		t.Fatalf("first pin: ok=%v PinNumber=%d, want ok=true PinNumber=1", ok, c1.PinNumber)
	}
	c2, ok := s.AddLivePin("/bar", "second", "alice", "u1", a2)
	if !ok || c2.PinNumber != 2 {
		t.Fatalf("second pin: ok=%v PinNumber=%d, want ok=true PinNumber=2", ok, c2.PinNumber)
	}
	c3, ok := s.AddLivePin("/foo", "third", "alice", "u1", a3)
	if !ok || c3.PinNumber != 3 {
		t.Fatalf("third pin: ok=%v PinNumber=%d, want ok=true PinNumber=3", ok, c3.PinNumber)
	}
}

// TestAddLivePin_DeleteMiddle_DoesNotReuseGap pins down the gap-reuse
// semantics: deleting a non-top pin leaves a gap, and the next add must NOT
// fill that gap — it gets max(remaining)+1. This preserves stable identifiers
// for users referring to "pin #N" after a delete in the middle of the
// sequence.
//
// Top-deletion is a separate case: today's max+1 algorithm DOES re-issue the
// number of the most recent pin if it's deleted before the next add. That's
// considered acceptable: in the live-mode workflow, deleting the
// just-added pin and immediately adding another is effectively an edit, and
// the previous PinNumber hadn't been "spoken about" yet. If product needs
// strict global monotonicity (no reuse ever, even at the top), the fix is a
// session-scoped counter persisted in CritJSON — out of scope here.
func TestAddLivePin_DeleteMiddle_DoesNotReuseGap(t *testing.T) {
	s := newTestSession(t)
	a1 := &DOMAnchor{Pathname: "/foo", CSSSelector: "h1", TagChain: []string{"H1"}}
	a2 := &DOMAnchor{Pathname: "/foo", CSSSelector: "h2", TagChain: []string{"H2"}}
	a3 := &DOMAnchor{Pathname: "/foo", CSSSelector: "h3", TagChain: []string{"H3"}}

	c1, _ := s.AddLivePin("/foo", "first", "alice", "u1", a1)
	c2, _ := s.AddLivePin("/foo", "second", "alice", "u1", a2)
	c3, _ := s.AddLivePin("/foo", "third", "alice", "u1", a3)
	if c1.PinNumber != 1 || c2.PinNumber != 2 || c3.PinNumber != 3 {
		t.Fatalf("setup: pins = %d,%d,%d, want 1,2,3", c1.PinNumber, c2.PinNumber, c3.PinNumber)
	}

	// Delete the MIDDLE pin (#2) — leaves a gap.
	if !s.DeleteComment("/foo", c2.ID) {
		t.Fatal("DeleteComment(c2) returned false")
	}

	a4 := &DOMAnchor{Pathname: "/foo", CSSSelector: "h4", TagChain: []string{"H4"}}
	c4, ok := s.AddLivePin("/foo", "fourth", "alice", "u1", a4)
	if !ok {
		t.Fatal("AddLivePin after middle-delete returned ok=false")
	}
	if c4.PinNumber != 4 {
		t.Fatalf("after middle-delete: PinNumber=%d, want 4 (gap from deleted #2 must NOT be reused)", c4.PinNumber)
	}
}

var lastDispatch string

func testRunLive([]string)   { lastDispatch = "live" }
func testRunReview([]string) { lastDispatch = "review" }

func dispatchForTest(args []string, liveFn, reviewFn func([]string)) {
	if looksLikeLiveArgs(args) {
		liveFn(args)
	} else {
		reviewFn(args)
	}
}

func TestDispatch_ExplicitLive(t *testing.T) {
	lastDispatch = ""
	dispatchForTest([]string{"http://localhost:3000"}, testRunLive, testRunReview)
	if lastDispatch != "live" {
		t.Errorf("dispatch = %q, want live", lastDispatch)
	}
}

func TestDispatch_HTTPSAutodetect(t *testing.T) {
	lastDispatch = ""
	dispatchForTest([]string{"https://myapp.test:4000/dashboard"}, testRunLive, testRunReview)
	if lastDispatch != "live" {
		t.Errorf("dispatch = %q, want live", lastDispatch)
	}
}

func TestDispatch_URLPlusFile_NotLive(t *testing.T) {
	lastDispatch = ""
	dispatchForTest([]string{"http://localhost:3000", "./README.md"}, testRunLive, testRunReview)
	if lastDispatch != "review" {
		t.Errorf("dispatch = %q, want review (URL+file must not autodetect)", lastDispatch)
	}
}

func TestDispatch_FTPNotLive(t *testing.T) {
	lastDispatch = ""
	dispatchForTest([]string{"ftp://foo.bar"}, testRunLive, testRunReview)
	if lastDispatch != "review" {
		t.Errorf("dispatch = %q, want review (ftp not autodetected)", lastDispatch)
	}
}

func TestDispatch_PlainArgNotLive(t *testing.T) {
	lastDispatch = ""
	dispatchForTest([]string{"README.md"}, testRunLive, testRunReview)
	if lastDispatch != "review" {
		t.Errorf("dispatch = %q, want review", lastDispatch)
	}
}

func TestLooksLikeLiveArgs(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"http://localhost:3000"}, true},
		{[]string{"https://example.com"}, true},
		{[]string{"https://localhost:8080/path"}, true},
		{[]string{"ftp://x.com"}, false},
		{[]string{"localhost:3000"}, false},
		{[]string{"README.md"}, false},
		{[]string{"http://a.com", "http://b.com"}, false},
		{nil, false},
		{[]string{}, false},
		{[]string{"://invalid"}, false},
	}
	for _, tc := range cases {
		got := looksLikeLiveArgs(tc.args)
		if got != tc.want {
			t.Errorf("looksLikeLiveArgs(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestSmokeTest_ConnectionRefused(t *testing.T) {
	r := runSmokeTest("http://127.0.0.1:19999", "")
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
	r := runSmokeTest(srv.URL, "")
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
	r := runSmokeTest(srv.URL, "")
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
	r := runSmokeTest(srv.URL, "")
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
	r := runSmokeTest(srv.URL, "")
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
	r := runSmokeTest(srv.URL, "")
	if r.kind != smokeOK {
		t.Errorf("kind = %v, want smokeOK (CSP stripped by proxy)", r.kind)
	}
	if !r.hasCSPFrameAncestors {
		t.Error("hasCSPFrameAncestors should be true")
	}
}

func TestShareGuard_LiveReview(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, "review")
	cj := CritJSON{ReviewType: "live", Origin: "http://localhost:3000", ReviewRound: 1, Files: map[string]CritJSONFile{}}
	if err := saveCritJSON(critPath, cj); err != nil {
		t.Fatalf("saveCritJSON: %v", err)
	}
	err := checkShareAllowed(critPath)
	if err == nil {
		t.Fatal("expected error for live review share")
	}
	if !strings.Contains(err.Error(), "live") {
		t.Errorf("error should mention live: %v", err)
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

func TestGitHubSyncGuard(t *testing.T) {
	tests := []struct {
		name      string
		cj        CritJSON
		op        string
		wantError bool
	}{
		{"live review pull", CritJSON{ReviewType: "live", Origin: "http://localhost:3000"}, "crit pull", true},
		{"live review push", CritJSON{ReviewType: "live", Origin: "http://localhost:3000"}, "crit push", true},
		{"code review pull", CritJSON{ReviewRound: 1}, "crit pull", false},
		{"code review push", CritJSON{ReviewRound: 1}, "crit push", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkGitHubSyncAllowed(tt.cj, tt.op)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error for %s on live review", tt.op)
				}
				if !strings.Contains(err.Error(), "live") {
					t.Errorf("error should mention live: %v", err)
				}
				if !strings.Contains(err.Error(), tt.op) {
					t.Errorf("error should mention op %q: %v", tt.op, err)
				}
			} else if err != nil {
				t.Errorf("code review should be allowed: %v", err)
			}
		})
	}
}

func TestCommentCLIGuard_LiveReview(t *testing.T) {
	dir := t.TempDir()
	critPath := filepath.Join(dir, "review")
	cj := CritJSON{ReviewType: "live", Origin: "http://localhost:3000", ReviewRound: 1, Files: map[string]CritJSONFile{}}
	if err := saveCritJSON(critPath, cj); err != nil {
		t.Fatalf("saveCritJSON: %v", err)
	}
	err := checkCommentCLIAllowed(critPath)
	if err == nil {
		t.Fatal("expected error for live review")
	}
	if !strings.Contains(err.Error(), "live") {
		t.Errorf("error should mention live: %v", err)
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

func TestCarryForward_LivePinSkipsRemap(t *testing.T) {
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
				t.Errorf("live pin lines remapped to %d/%d; should stay 0/0", c.StartLine, c.EndLine)
			}
		}
	}
	if !found {
		t.Error("live pin not carried forward")
	}
}

func TestMergeGHComments_LivePinNotDeduped(t *testing.T) {
	pin := Comment{
		ID: "pin1", StartLine: 0, EndLine: 0, Body: "pin body",
		DOMAnchor: &DOMAnchor{Pathname: "/dashboard", CSSSelector: "#h1"},
	}
	cj := &CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"/dashboard": {Comments: []Comment{pin}},
		},
	}
	ghc := ghComment{
		ID:   42,
		Path: "/dashboard",
		Line: 10,
		Side: "RIGHT",
		Body: "pin body",
	}
	ghc.User.Login = "reviewer"
	merged := mergeGHComments(cj, []ghComment{ghc})
	if merged == 0 {
		t.Error("GH comment should be added (not deduped against live pin); merged = 0")
	}
	pinCount := 0
	for _, c := range cj.Files["/dashboard"].Comments {
		if c.DOMAnchor != nil {
			pinCount++
		}
	}
	if pinCount != 1 {
		t.Errorf("live pin count = %d after merge, want 1", pinCount)
	}
}

func TestParseServerFlags_LiveOrigin(t *testing.T) {
	f := parseServerFlags([]string{"--live-origin", "http://localhost:3000"})
	if f.liveOrigin != "http://localhost:3000" {
		t.Errorf("liveOrigin = %q, want http://localhost:3000", f.liveOrigin)
	}
}

func TestParseServerFlags_NoLiveOrigin(t *testing.T) {
	f := parseServerFlags([]string{"plan.md"})
	if f.liveOrigin != "" {
		t.Errorf("liveOrigin = %q, want empty", f.liveOrigin)
	}
}

func TestCreateLiveSession_EmptyOriginIsFatal(t *testing.T) {
	_, err := createLiveSession(&serverConfig{liveOrigin: ""})
	if err == nil {
		t.Fatal("createLiveSession with empty origin must error")
	}
}

func TestRunLive_SmokeFailFatal(t *testing.T) {
	result := runSmokeTest("http://127.0.0.1:19999", "")
	if !result.fatal {
		t.Error("conn refused must be fatal")
	}
	if result.kind != smokeConnRefused {
		t.Errorf("kind = %v", result.kind)
	}
}

func TestParseLiveCLIFlags(t *testing.T) {
	f := parseLiveCLIFlags([]string{
		"-p", "8080",
		"--host", "0.0.0.0",
		"--no-open",
		"-q",
		"--share-url", "https://share.example",
		"--cookie", "a=1",
		"--cookie", "b=2",
		"--cookie-file", "/tmp/jar.txt",
		"https://example.com/app?q=1#frag",
	})
	if f.origin != "https://example.com/app" {
		t.Fatalf("origin = %q", f.origin)
	}
	if f.port != 8080 || f.host != "0.0.0.0" || !f.noOpen || !f.quiet {
		t.Fatalf("flags = %+v", f)
	}
	if f.shareURL != "https://share.example" || f.cookieFile != "/tmp/jar.txt" {
		t.Fatalf("share/cookie file = %+v", f)
	}
	if len(f.cookieFlags) != 2 || f.cookieFlags[0] != "a=1" || f.cookieFlags[1] != "b=2" {
		t.Fatalf("cookieFlags = %v", f.cookieFlags)
	}
}

func TestBuildLiveDaemonArgs(t *testing.T) {
	f := liveCLIFlags{port: 9000, host: "127.0.0.1", quiet: true}
	cfg := Config{Port: 3000, Quiet: false}

	withCookie := buildLiveDaemonArgs("http://localhost:3000/dashboard", "sess=abc", f, cfg, true)
	if !containsArgPair(withCookie, "--live-origin", "http://localhost:3000/dashboard") {
		t.Fatalf("missing live-origin: %v", withCookie)
	}
	if !containsArgPair(withCookie, "--live-cookie", "sess=abc") {
		t.Fatalf("missing live-cookie: %v", withCookie)
	}
	if !containsArgPair(withCookie, "--port", "9000") {
		t.Fatalf("missing port: %v", withCookie)
	}
	if !containsArgPair(withCookie, "--no-open", "") {
		t.Fatalf("missing no-open: %v", withCookie)
	}
	if !containsArgPair(withCookie, "--quiet", "") {
		t.Fatalf("missing quiet: %v", withCookie)
	}

	withoutCookie := buildLiveDaemonArgs("http://localhost:3000", "", f, cfg, false)
	for i, a := range withoutCookie {
		if a == "--live-cookie" {
			t.Fatalf("unexpected --live-cookie at %d in %v", i, withoutCookie)
		}
	}
}

func containsArgPair(args []string, key, want string) bool {
	for i := 0; i < len(args); i++ {
		if args[i] != key {
			continue
		}
		if want == "" {
			return true
		}
		if i+1 < len(args) && args[i+1] == want {
			return true
		}
	}
	return false
}

func TestRunSmokeTest_WithCookies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "session=abc" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, "<html><body>ok</body></html>")
	}))
	defer srv.Close()

	result := runSmokeTest(srv.URL, "session=abc")
	if result.fatal {
		t.Fatalf("unexpected fatal: %+v", result)
	}
	if result.kind != smokeOK {
		t.Fatalf("kind = %v, want smokeOK", result.kind)
	}
}

func TestRunSmokeTest_WithCookies_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	result := runSmokeTest(srv.URL, "wrong=1")
	if result.kind != smokeNon2xx {
		t.Fatalf("kind = %v, want smokeNon2xx", result.kind)
	}
	if result.fatal {
		t.Fatal("non-2xx should warn, not fatal")
	}
}

func TestCheckLiveSmoke_WarnsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintln(w, "<html><body>nope</body></html>")
	}))
	defer srv.Close()

	checkLiveSmoke(srv.URL, "")
}

func TestCheckLiveSmoke_NotesFrameworkAndCSP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintln(w, `<html><head></head><body id="__next">ok</body></html>`)
	}))
	defer srv.Close()

	checkLiveSmoke(srv.URL, "")
}

func TestRunLive_OriginNormalisedToSchemeHost(t *testing.T) {
	u, _ := url.Parse("https://myapp.test:4000/dashboard?q=1")
	origin := u.Scheme + "://" + u.Host
	if origin != "https://myapp.test:4000" {
		t.Errorf("origin = %q, want https://myapp.test:4000", origin)
	}
}

func TestDetectFrameworks(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"phoenix", `<div phx-track-static></div>`, []string{
			"Phoenix LiveView detected. Make sure your dev endpoint allows iframing — strip CSP locally if needed.",
		}},
		{"phoenix-hook", `<div phx-hook="X"></div>`, []string{
			"Phoenix LiveView detected. Make sure your dev endpoint allows iframing — strip CSP locally if needed.",
		}},
		{"vite", `<script type="module" src="/@vite/client"></script>`, []string{
			"Vite dev server detected. WebSocket HMR will be proxied automatically.",
		}},
		{"nextjs", `<div id="__next"></div>`, []string{
			"Next.js dev detected. SPA route changes via `pushState` are supported.",
		}},
		{"phoenix+vite", `<div phx-hook="X"></div><script src="/@vite/client"></script>`, []string{
			"Phoenix LiveView detected. Make sure your dev endpoint allows iframing — strip CSP locally if needed.",
			"Vite dev server detected. WebSocket HMR will be proxied automatically.",
		}},
		{"plain", `<html><body><h1>hi</h1></body></html>`, nil},
		{"phx-prefix-does-not-falsely-match", `<div class="phx-foo phxbar">x</div>`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := detectFrameworks([]byte(tc.body))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

// TestCarryForwardComment_PreservesLivePinFields guards against silent data
// loss for live pins on round bump. carryForwardComment is called from
// carryForwardAllComments for every file lacking PreviousContent — which is
// the case for live-route entries (no on-disk content). Dropping DOMAnchor
// or PinNumber here makes live pins disappear from /api/file/comments
// after POST /api/round-complete.
//
// Drift fields (Drifted, DriftedOnRound) are NOT preserved for live pins —
// drift detection is disabled for live mode because the live DOM can change
// without any code change (LiveView re-renders, etc.). Live pins must
// always emerge with Drifted=false after carry-forward.
func TestCarryForwardComment_PreservesLivePinFields(t *testing.T) {
	old := Comment{
		ID:             "pin-original",
		Body:           "needs work",
		Author:         "alice",
		UserID:         "u1",
		CreatedAt:      "2026-01-01T00:00:00Z",
		UpdatedAt:      "2026-01-01T00:00:00Z",
		ReviewRound:    1,
		DOMAnchor:      &DOMAnchor{Pathname: "/dashboard", CSSSelector: "#h1", TagChain: []string{"H1"}},
		PinNumber:      7,
		Drifted:        true,
		DriftedOnRound: 2,
	}

	carried := carryForwardComment(old, "pin-new", "2026-02-01T00:00:00Z")

	if carried.DOMAnchor == nil {
		t.Fatal("DOMAnchor lost on carry-forward")
	}
	if carried.DOMAnchor.CSSSelector != "#h1" {
		t.Errorf("DOMAnchor.CSSSelector = %q, want #h1", carried.DOMAnchor.CSSSelector)
	}
	if carried.PinNumber != 7 {
		t.Errorf("PinNumber = %d, want 7", carried.PinNumber)
	}
	if carried.Drifted {
		t.Error("Drifted = true, want false (live pins must never be drifted)")
	}
	if carried.DriftedOnRound != 0 {
		t.Errorf("DriftedOnRound = %d, want 0 (live pins must not carry drift round)", carried.DriftedOnRound)
	}
	if carried.UserID != "u1" {
		t.Errorf("UserID = %q, want u1", carried.UserID)
	}
}

// TestCarryForwardComment_CodeCommentDriftPreserved guards that the live-mode
// drift suppression does NOT regress code-review drift carry-forward. Code
// comments (DOMAnchor == nil) must continue to carry their Drifted and
// DriftedOnRound fields across rounds.
func TestCarryForwardComment_CodeCommentDriftPreserved(t *testing.T) {
	old := Comment{
		ID:             "code-original",
		Body:           "needs work",
		Author:         "alice",
		CreatedAt:      "2026-01-01T00:00:00Z",
		UpdatedAt:      "2026-01-01T00:00:00Z",
		ReviewRound:    1,
		StartLine:      10,
		EndLine:        12,
		Drifted:        true,
		DriftedOnRound: 2,
	}

	carried := carryForwardComment(old, "code-new", "2026-02-01T00:00:00Z")

	if !carried.Drifted {
		t.Error("Drifted = false, want true (code comments preserve drift)")
	}
	if carried.DriftedOnRound != 2 {
		t.Errorf("DriftedOnRound = %d, want 2", carried.DriftedOnRound)
	}
}

// TestHandleRoundCompleteFiles_LivePinsSurvive exercises the round-complete
// pipeline end-to-end for a live session: a pin (open and resolved) added
// in round 1 must remain readable in round 2 with anchor identity intact.
// This is the regression for the gap that left two
// rounds.livemode.spec.ts scenarios fixme'd.
func TestHandleRoundCompleteFiles_LivePinsSurvive(t *testing.T) {
	dir := t.TempDir()
	reviewPath := filepath.Join(dir, "review")

	// Seed a live review file containing an open pin and a resolved pin
	// (both with non-trivial DriftedOnRound to verify it round-trips).
	openAnchor := &DOMAnchor{Pathname: "/", CSSSelector: "#primary-btn", TagChain: []string{"BUTTON"}}
	resolvedAnchor := &DOMAnchor{Pathname: "/", CSSSelector: "#secondary-btn", TagChain: []string{"BUTTON"}}
	cj := CritJSON{
		ReviewType:  "live",
		Origin:      "http://localhost:3000",
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"/": {
				Status: "added",
				Comments: []Comment{
					{
						ID: "pin1", Body: "open pin",
						Author: "alice", UserID: "u1",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
						ReviewRound: 1, PinNumber: 1, DOMAnchor: openAnchor,
						DriftedOnRound: 1, Drifted: true,
					},
					{
						ID: "pin2", Body: "resolved pin",
						Author: "alice", UserID: "u1",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
						ReviewRound: 1, PinNumber: 2, DOMAnchor: resolvedAnchor,
						Resolved: true,
					},
				},
			},
		},
	}
	if err := saveCritJSON(reviewPath, cj); err != nil {
		t.Fatalf("saveCritJSON: %v", err)
	}

	s := &Session{
		Mode:           "files",
		RepoRoot:       dir,
		ReviewRound:    1,
		ReviewType:     "live",
		Origin:         "http://localhost:3000",
		ReviewFilePath: reviewPath,
		subscribers:    make(map[chan SSEEvent]struct{}),
		roundComplete:  make(chan struct{}, 1),
		Files: []*FileEntry{
			{Path: "/", FileType: "live-route", Status: "added", Comments: cj.Files["/"].Comments},
		},
	}

	// SignalRoundComplete (which the server invokes from POST /api/round-complete
	// before the watcher fires) clears f.Comments. The carry-forward pipeline is
	// the only thing that puts pins back. Mirror that here so the test exercises
	// the same state the watcher sees.
	s.SignalRoundComplete()
	// Drain the channel so it doesn't leak across tests.
	<-s.roundComplete

	s.handleRoundCompleteFiles()

	if s.ReviewRound != 2 {
		t.Fatalf("ReviewRound = %d after round-complete, want 2", s.ReviewRound)
	}

	fe := s.Files[0]
	if len(fe.Comments) != 2 {
		t.Fatalf("Comments count = %d after round-complete, want 2 (live pins must survive)", len(fe.Comments))
	}

	byPin := map[int]Comment{}
	for _, c := range fe.Comments {
		byPin[c.PinNumber] = c
	}
	open, okOpen := byPin[1]
	if !okOpen {
		t.Fatalf("open pin (PinNumber=1) missing after carry-forward; got pins %+v", byPin)
	}
	if open.DOMAnchor == nil || open.DOMAnchor.CSSSelector != "#primary-btn" {
		t.Errorf("open pin DOMAnchor lost or mutated: %+v", open.DOMAnchor)
	}
	if open.DriftedOnRound != 0 {
		t.Errorf("open pin DriftedOnRound = %d, want 0 (live pins must not carry drift round)", open.DriftedOnRound)
	}
	if open.Drifted {
		t.Error("open pin Drifted = true, want false (live pins must never be drifted)")
	}
	if !open.CarriedForward {
		t.Error("open pin CarriedForward = false, want true")
	}

	resolved, okResolved := byPin[2]
	if !okResolved {
		t.Fatalf("resolved pin (PinNumber=2) missing after carry-forward; got pins %+v", byPin)
	}
	if !resolved.Resolved {
		t.Error("resolved pin lost Resolved=true on carry-forward")
	}
	if resolved.DOMAnchor == nil || resolved.DOMAnchor.CSSSelector != "#secondary-btn" {
		t.Errorf("resolved pin DOMAnchor lost or mutated: %+v", resolved.DOMAnchor)
	}
}

// TestCreateLiveSession_FreshStartsOnRoundOne pins down Bug 1: a previously
// abandoned live daemon may persist `review_round: 2` to disk after a single
// round-complete. When a new session boots against the same origin and finds
// the file empty of comments, it must reset to round 1 — otherwise the next
// pin authored ships against a stale counter.
func TestCreateLiveSession_FreshStartsOnRoundOne(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "review-id")
	if err := os.MkdirAll(identity, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	stale := CritJSON{
		ReviewRound: 2,
		Files:       map[string]CritJSONFile{},
	}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(filepath.Join(identity, "review.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sc := &serverConfig{
		liveOrigin: "http://localhost:4000",
		reviewPath: identity,
	}
	s, err := createLiveSession(sc)
	if err != nil {
		t.Fatalf("createLiveSession: %v", err)
	}
	if s.ReviewRound != 1 {
		t.Errorf("ReviewRound = %d, want 1 (stale round must not propagate when no comments exist)", s.ReviewRound)
	}
}

// TestCreateLiveSession_HonorsRoundWhenCommentsPresent guards the inverse:
// a real resumed session with persisted comments must keep its round counter
// (carry-forward / drift detection depends on knowing which round each pin
// was created in).
func TestCreateLiveSession_HonorsRoundWhenCommentsPresent(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "review-id")
	if err := os.MkdirAll(identity, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cj := CritJSON{
		ReviewRound: 3,
		Files: map[string]CritJSONFile{
			"/foo": {
				Status:   "added",
				Comments: []Comment{{ID: "c1", Body: "hi", PinNumber: 1}},
			},
		},
	}
	data, _ := json.Marshal(cj)
	if err := os.WriteFile(filepath.Join(identity, "review.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sc := &serverConfig{
		liveOrigin: "http://localhost:4000",
		reviewPath: identity,
	}
	s, err := createLiveSession(sc)
	if err != nil {
		t.Fatalf("createLiveSession: %v", err)
	}
	if s.ReviewRound != 3 {
		t.Errorf("ReviewRound = %d, want 3 (resumed session with comments must keep its round)", s.ReviewRound)
	}
}

// TestLiveSession_ExternalReplyEmitsCommentsChanged guards Bug 4: when
// `crit comment --reply-to` writes a reply to a live pin via a separate
// process, the running live daemon must detect the on-disk change and fan
// out a `comments-changed` SSE event so subscribed clients can refresh
// without a full page reload. Code-review mode already does this through
// mergeExternalCritJSON; this test pins down that live mode does too.
func TestLiveSession_ExternalReplyEmitsCommentsChanged(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "review-id")
	if err := os.MkdirAll(identity, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	reviewPath := filepath.Join(identity, "review.json")

	// Seed a live review file with a single pin.
	pinAnchor := &DOMAnchor{Pathname: "/dashboard", CSSSelector: "#primary-btn", TagChain: []string{"BUTTON"}}
	cj := CritJSON{
		ReviewType:  "live",
		Origin:      "http://localhost:3000",
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"/dashboard": {
				Status: "added",
				Comments: []Comment{
					{
						ID: "pin1", Body: "needs polish",
						Author: "alice", UserID: "u1",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
						ReviewRound: 1, PinNumber: 1, DOMAnchor: pinAnchor,
					},
				},
			},
		},
	}
	data, _ := json.Marshal(cj)
	if err := os.WriteFile(reviewPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile seed: %v", err)
	}
	info, err := os.Stat(reviewPath)
	if err != nil {
		t.Fatalf("Stat seed: %v", err)
	}

	// Build a session that reflects the seeded state. lastCritJSONMtime is set
	// to the seed's mtime so mergeExternalCritJSON will only fire when the
	// file changes again (mirrors a daemon that has just (re)loaded).
	s := &Session{
		Mode:              "files",
		RepoRoot:          dir,
		ReviewType:        "live",
		Origin:            "http://localhost:3000",
		ReviewRound:       1,
		ReviewFilePath:    identity,
		lastCritJSONMtime: info.ModTime(),
		subscribers:       make(map[chan SSEEvent]struct{}),
		roundComplete:     make(chan struct{}, 1),
		Files: []*FileEntry{
			{Path: "/dashboard", FileType: "live-route", Status: "added", Comments: cj.Files["/dashboard"].Comments},
		},
	}

	// Subscribe before mutating so we capture the broadcast.
	sub := s.Subscribe()
	defer s.Unsubscribe(sub)

	// Simulate `crit comment --reply-to pin1 "looking better"` from a separate
	// process: load, append reply, save through the same writer the CLI uses.
	loaded, err := loadCritJSON(identity)
	if err != nil {
		t.Fatalf("loadCritJSON: %v", err)
	}
	if err := appendReply(&loaded, "pin1", "looking better", "bob", "u2", false, ""); err != nil {
		t.Fatalf("appendReply: %v", err)
	}
	// Force a distinct mtime even on filesystems with low timestamp resolution.
	time.Sleep(20 * time.Millisecond)
	if err := saveCritJSON(identity, loaded); err != nil {
		t.Fatalf("saveCritJSON: %v", err)
	}

	// The watcher tick that ultimately fires SSE.
	if !s.mergeExternalCritJSON() {
		t.Fatal("mergeExternalCritJSON returned false; daemon failed to detect external reply")
	}

	select {
	case ev := <-sub:
		if ev.Type != "comments-changed" {
			t.Errorf("SSE event type = %q, want comments-changed", ev.Type)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no SSE event received after external reply (subscribers were not notified)")
	}

	// And the reply must be visible in memory.
	fe := s.Files[0]
	if len(fe.Comments) != 1 {
		t.Fatalf("Comments count = %d, want 1", len(fe.Comments))
	}
	if len(fe.Comments[0].Replies) != 1 {
		t.Fatalf("Replies count = %d, want 1 (reply not merged from disk)", len(fe.Comments[0].Replies))
	}
	if got := fe.Comments[0].Replies[0].Body; got != "looking better" {
		t.Errorf("Reply body = %q, want %q", got, "looking better")
	}
}

// TestHandleFileComments_LivePinFansOutSSE pins down the API-side broadcast
// added for Bug 4: a successful POST /api/file/comments with a DOMAnchor must
// emit a comments-changed SSE event so other tabs reviewing the same live
// session refresh immediately. Without this, cross-tab sync stalls until the
// watcher's 1s mtime tick, and the originating daemon's own writes never fire
// the watcher path at all (lastCritJSONMtime equals the just-written mtime).
func TestHandleFileComments_LivePinFansOutSSE(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "review-id")
	if err := os.MkdirAll(identity, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	sess := &Session{
		Mode:           "files",
		RepoRoot:       dir,
		ReviewType:     "live",
		Origin:         "http://localhost:3000",
		ReviewRound:    1,
		ReviewFilePath: identity,
		subscribers:    make(map[chan SSEEvent]struct{}),
		roundComplete:  make(chan struct{}, 1),
	}

	srv, err := NewServer(nil, frontendFS, "", false, "", "tester", "test", 0, "")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.SetSession(sess)

	sub := sess.Subscribe()
	defer sess.Unsubscribe(sub)

	body := strings.NewReader(`{"body":"first pin","author":"alice","dom_anchor":{"pathname":"/dashboard","css_selector":"#primary-btn","tag_chain":["BUTTON"]}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/file/comments?path=/dashboard", body)
	rec := httptest.NewRecorder()
	srv.handleFileComments(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	select {
	case ev := <-sub:
		if ev.Type != "comments-changed" {
			t.Errorf("event type = %q, want comments-changed", ev.Type)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("no SSE event received after live-pin POST (cross-tab sync would stall)")
	}
}

// TestCreateLiveSession_FreshAwaitsFirstReview pins down the second half of
// Bug 1: a brand-new live session (no on-disk review file, no review dir)
// must report IsAwaitingFirstReview()==true so the daemon-client's first
// review-cycle call does NOT fire SignalRoundComplete() at boot. Without this,
// the watcher's handleRoundCompleteFiles bumps ReviewRound from 1 to 2 before
// the user authors a single pin, and the resulting comment ships against the
// stale counter — surfaced in the UI as "Round #2 on a brand-new review".
func TestCreateLiveSession_FreshAwaitsFirstReview(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "review-id-fresh")
	// Deliberately do NOT create the directory or any review file: we are
	// modelling the never-before-seen URL case.
	sc := &serverConfig{
		liveOrigin: "http://localhost:4000",
		reviewPath: identity,
	}
	s, err := createLiveSession(sc)
	if err != nil {
		t.Fatalf("createLiveSession: %v", err)
	}
	if s.ReviewRound != 1 {
		t.Errorf("ReviewRound = %d, want 1", s.ReviewRound)
	}
	if !s.IsAwaitingFirstReview() {
		t.Fatal("IsAwaitingFirstReview() = false, want true (fresh live session must not auto-fire round-complete on first review-cycle call)")
	}
}

// TestLiveSession_FirstPinAfterBootShipsRound1 is the end-to-end pin-down
// for Bug 1 from the user's reproduction: user pins one element on a fresh
// session, the persisted comment must carry review_round: 1. Without the
// awaitingFirstReview default, the daemon-client's review-cycle call fires
// SignalRoundComplete at boot, the watcher bumps the round to 2, and the
// AddLivePin stamps ReviewRound: 2 onto the user's first pin.
func TestLiveSession_FirstPinAfterBootShipsRound1(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "review-id-firstpin")
	sc := &serverConfig{
		liveOrigin: "http://localhost:4000",
		reviewPath: identity,
	}
	sess, err := createLiveSession(sc)
	if err != nil {
		t.Fatalf("createLiveSession: %v", err)
	}

	// Reproduce the daemon-client review-cycle gate: SignalRoundComplete is
	// only fired when !IsAwaitingFirstReview. Bug repro: when the gate is
	// missing, this fires on boot and the watcher bumps the round.
	if !sess.IsAwaitingFirstReview() {
		sess.SignalRoundComplete()
		// Drain the channel synchronously through the files-mode handler so
		// the round bump observable in this goroutine matches what the
		// watcher would do in the live daemon.
		select {
		case <-sess.roundComplete:
			sess.handleRoundCompleteFiles()
		default:
		}
	}

	anchor := &DOMAnchor{Pathname: "/", CSSSelector: "h1", TagChain: []string{"H1"}}
	c, ok := sess.AddLivePin("/", "first pin", "alice", "u1", anchor)
	if !ok {
		t.Fatal("AddLivePin returned ok=false")
	}
	if c.ReviewRound != 1 {
		t.Errorf("first pin ReviewRound = %d, want 1 (boot-time round bump leaked into the user's first pin)", c.ReviewRound)
	}
	if got := sess.GetReviewRound(); got != 1 {
		t.Errorf("session GetReviewRound() = %d, want 1", got)
	}
}

// TestDOMAnchor_NoScreenshotField pins down that the Screenshot field has
// been removed from DOMAnchor. Persisting base64 JPEGs in every comment
// bloats review.json and the captured screenshots have been visually broken
// for multiple iterations — they're not worth fixing.
//
// This is a structural assertion (Marshal must not produce a "screenshot"
// key) rather than a struct-field absence check, because reflective
// field-presence tests are brittle. The Marshal output is the contract that
// matters.
func TestDOMAnchor_NoScreenshotField(t *testing.T) {
	a := &DOMAnchor{
		Pathname:    "/dashboard",
		CSSSelector: "#h1",
		TagChain:    []string{"H1"},
		OuterHTML:   "<h1>x</h1>",
	}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "screenshot") {
		t.Fatalf("DOMAnchor JSON must not contain a screenshot key, got: %s", data)
	}
}

// TestDOMAnchor_LegacyScreenshotIgnored guards backwards compatibility: an
// existing review.json on disk may contain a legacy `screenshot` JSON field
// from before the field was removed. encoding/json silently ignores unknown
// keys by default, so the load must succeed without error.
func TestDOMAnchor_LegacyScreenshotIgnored(t *testing.T) {
	raw := `{
		"pathname": "/dashboard",
		"css_selector": "#h1",
		"tag_chain": ["H1"],
		"outer_html": "<h1>x</h1>",
		"screenshot": "data:image/jpeg;base64,abcdef",
		"viewport_width": 1280,
		"viewport_height": 800
	}`
	var got DOMAnchor
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("legacy review.json with screenshot must unmarshal cleanly: %v", err)
	}
	if got.CSSSelector != "#h1" {
		t.Errorf("CSSSelector = %q, want #h1", got.CSSSelector)
	}
	if got.ViewportWidth != 1280 {
		t.Errorf("ViewportWidth = %d, want 1280", got.ViewportWidth)
	}
}

// TestLive_PostFileCommentsDropsScreenshot pins down the persistence side:
// a POST to /api/file/comments carrying a legacy screenshot field must
// succeed (forward compat with old frontend builds), but the persisted
// dom_anchor on the saved review.json must NOT contain a screenshot key.
func TestLive_PostFileCommentsDropsScreenshot(t *testing.T) {
	dir := t.TempDir()
	identity := filepath.Join(dir, "review-id")
	if err := os.MkdirAll(identity, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	sess := &Session{
		Mode:           "files",
		RepoRoot:       dir,
		ReviewType:     "live",
		Origin:         "http://localhost:3000",
		ReviewRound:    1,
		ReviewFilePath: identity,
		subscribers:    make(map[chan SSEEvent]struct{}),
		roundComplete:  make(chan struct{}, 1),
	}

	srv, err := NewServer(nil, frontendFS, "", false, "", "tester", "test", 0, "")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.SetSession(sess)

	body := strings.NewReader(`{
		"body": "first pin",
		"author": "alice",
		"dom_anchor": {
			"pathname": "/dashboard",
			"css_selector": "#primary-btn",
			"tag_chain": ["BUTTON"],
			"outer_html": "<button>Go</button>",
			"screenshot": "data:image/jpeg;base64,abcdef",
			"viewport_width": 1280,
			"viewport_height": 800
		}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/file/comments?path=/dashboard", body)
	rec := httptest.NewRecorder()
	srv.handleFileComments(rec, req)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 201/200; body=%s", rec.Code, rec.Body.String())
	}

	// Force the debounced write to flush so we can read the persisted file.
	if err := sess.SyncWriteFiles(); err != nil {
		t.Fatalf("SyncWriteFiles: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(identity, "review.json"))
	if err != nil {
		t.Fatalf("ReadFile review.json: %v", err)
	}
	if strings.Contains(string(data), "screenshot") {
		t.Fatalf("persisted review.json must not contain a screenshot key, got:\n%s", data)
	}
}

func TestLiveSession_ReinvokeCommandIncludesOrigin(t *testing.T) {
	sc := &serverConfig{
		liveOrigin: "http://localhost:4000",
		reviewPath: filepath.Join(t.TempDir(), "review-reinvoke"),
	}
	sess, err := createLiveSession(sc)
	if err != nil {
		t.Fatalf("createLiveSession: %v", err)
	}
	got := sess.ReinvokeCommand()
	want := "crit live http://localhost:4000"
	if got != want {
		t.Errorf("ReinvokeCommand() = %q, want %q", got, want)
	}
}

func TestLiveSession_AuthorFromConfig(t *testing.T) {
	setHome(t, t.TempDir())
	s, _ := newTestServer(t)
	s.author = "Tomasz"

	sess := &Session{
		Mode:        "files",
		ReviewType:  "live",
		ReviewRound: 1,
		Files:       []*FileEntry{{Path: "/", Status: "added"}},
		subscribers: make(map[chan SSEEvent]struct{}),
	}
	s.session.Store(sess)

	body := `{"start_line":0,"end_line":0,"body":"test pin","dom_anchor":{"pathname":"/","css_selector":"#btn","tag_chain":["BUTTON"]}}`
	req := httptest.NewRequest("POST", "/api/file/comments?path=/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	comments := sess.GetComments("/")
	if len(comments) == 0 {
		t.Fatal("no comments after POST")
	}
	if comments[0].Author != "Tomasz" {
		t.Errorf("comment.Author = %q, want %q", comments[0].Author, "Tomasz")
	}
}

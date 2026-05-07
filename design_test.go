package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAddDesignPin_AssignsMonotonicGlobalPinNumbers(t *testing.T) {
	s := newTestSession(t)
	a1 := &DOMAnchor{Pathname: "/foo", CSSSelector: "h1", TagChain: []string{"H1"}}
	a2 := &DOMAnchor{Pathname: "/bar", CSSSelector: "h2", TagChain: []string{"H2"}}
	a3 := &DOMAnchor{Pathname: "/foo", CSSSelector: "h3", TagChain: []string{"H3"}}

	c1, ok := s.AddDesignPin("/foo", "first", "alice", "u1", a1)
	if !ok || c1.PinNumber != 1 {
		t.Fatalf("first pin: ok=%v PinNumber=%d, want ok=true PinNumber=1", ok, c1.PinNumber)
	}
	c2, ok := s.AddDesignPin("/bar", "second", "alice", "u1", a2)
	if !ok || c2.PinNumber != 2 {
		t.Fatalf("second pin: ok=%v PinNumber=%d, want ok=true PinNumber=2", ok, c2.PinNumber)
	}
	c3, ok := s.AddDesignPin("/foo", "third", "alice", "u1", a3)
	if !ok || c3.PinNumber != 3 {
		t.Fatalf("third pin: ok=%v PinNumber=%d, want ok=true PinNumber=3", ok, c3.PinNumber)
	}
}

// TestAddDesignPin_DeleteMiddle_DoesNotReuseGap pins down the gap-reuse
// semantics: deleting a non-top pin leaves a gap, and the next add must NOT
// fill that gap — it gets max(remaining)+1. This preserves stable identifiers
// for users referring to "pin #N" after a delete in the middle of the
// sequence.
//
// Top-deletion is a separate case: today's max+1 algorithm DOES re-issue the
// number of the most recent pin if it's deleted before the next add. That's
// considered acceptable: in the design-mode workflow, deleting the
// just-added pin and immediately adding another is effectively an edit, and
// the previous PinNumber hadn't been "spoken about" yet. If product needs
// strict global monotonicity (no reuse ever, even at the top), the fix is a
// session-scoped counter persisted in CritJSON — out of scope here.
func TestAddDesignPin_DeleteMiddle_DoesNotReuseGap(t *testing.T) {
	s := newTestSession(t)
	a1 := &DOMAnchor{Pathname: "/foo", CSSSelector: "h1", TagChain: []string{"H1"}}
	a2 := &DOMAnchor{Pathname: "/foo", CSSSelector: "h2", TagChain: []string{"H2"}}
	a3 := &DOMAnchor{Pathname: "/foo", CSSSelector: "h3", TagChain: []string{"H3"}}

	c1, _ := s.AddDesignPin("/foo", "first", "alice", "u1", a1)
	c2, _ := s.AddDesignPin("/foo", "second", "alice", "u1", a2)
	c3, _ := s.AddDesignPin("/foo", "third", "alice", "u1", a3)
	if c1.PinNumber != 1 || c2.PinNumber != 2 || c3.PinNumber != 3 {
		t.Fatalf("setup: pins = %d,%d,%d, want 1,2,3", c1.PinNumber, c2.PinNumber, c3.PinNumber)
	}

	// Delete the MIDDLE pin (#2) — leaves a gap.
	if !s.DeleteComment("/foo", c2.ID) {
		t.Fatal("DeleteComment(c2) returned false")
	}

	a4 := &DOMAnchor{Pathname: "/foo", CSSSelector: "h4", TagChain: []string{"H4"}}
	c4, ok := s.AddDesignPin("/foo", "fourth", "alice", "u1", a4)
	if !ok {
		t.Fatal("AddDesignPin after middle-delete returned ok=false")
	}
	if c4.PinNumber != 4 {
		t.Fatalf("after middle-delete: PinNumber=%d, want 4 (gap from deleted #2 must NOT be reused)", c4.PinNumber)
	}
}

var lastDispatch string

func testRunDesign([]string) { lastDispatch = "design" }
func testRunReview([]string) { lastDispatch = "review" }

func dispatchForTest(args []string, designFn, reviewFn func([]string)) {
	if looksLikeDesignArgs(args) {
		designFn(args)
	} else {
		reviewFn(args)
	}
}

func TestDispatch_ExplicitDesign(t *testing.T) {
	lastDispatch = ""
	dispatchForTest([]string{"http://localhost:3000"}, testRunDesign, testRunReview)
	if lastDispatch != "design" {
		t.Errorf("dispatch = %q, want design", lastDispatch)
	}
}

func TestDispatch_HTTPSAutodetect(t *testing.T) {
	lastDispatch = ""
	dispatchForTest([]string{"https://myapp.test:4000/dashboard"}, testRunDesign, testRunReview)
	if lastDispatch != "design" {
		t.Errorf("dispatch = %q, want design", lastDispatch)
	}
}

func TestDispatch_URLPlusFile_NotDesign(t *testing.T) {
	lastDispatch = ""
	dispatchForTest([]string{"http://localhost:3000", "./README.md"}, testRunDesign, testRunReview)
	if lastDispatch != "review" {
		t.Errorf("dispatch = %q, want review (URL+file must not autodetect)", lastDispatch)
	}
}

func TestDispatch_FTPNotDesign(t *testing.T) {
	lastDispatch = ""
	dispatchForTest([]string{"ftp://foo.bar"}, testRunDesign, testRunReview)
	if lastDispatch != "review" {
		t.Errorf("dispatch = %q, want review (ftp not autodetected)", lastDispatch)
	}
}

func TestDispatch_PlainArgNotDesign(t *testing.T) {
	lastDispatch = ""
	dispatchForTest([]string{"README.md"}, testRunDesign, testRunReview)
	if lastDispatch != "review" {
		t.Errorf("dispatch = %q, want review", lastDispatch)
	}
}

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

func TestGitHubSyncGuard(t *testing.T) {
	tests := []struct {
		name      string
		cj        CritJSON
		op        string
		wantError bool
	}{
		{"design review pull", CritJSON{ReviewType: "design", Origin: "http://localhost:3000"}, "crit pull", true},
		{"design review push", CritJSON{ReviewType: "design", Origin: "http://localhost:3000"}, "crit push", true},
		{"code review pull", CritJSON{ReviewRound: 1}, "crit pull", false},
		{"code review push", CritJSON{ReviewRound: 1}, "crit push", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkGitHubSyncAllowed(tt.cj, tt.op)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error for %s on design review", tt.op)
				}
				if !strings.Contains(err.Error(), "design") {
					t.Errorf("error should mention design: %v", err)
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

func TestMergeGHComments_DesignPinNotDeduped(t *testing.T) {
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
		t.Error("GH comment should be added (not deduped against design pin); merged = 0")
	}
	pinCount := 0
	for _, c := range cj.Files["/dashboard"].Comments {
		if c.DOMAnchor != nil {
			pinCount++
		}
	}
	if pinCount != 1 {
		t.Errorf("design pin count = %d after merge, want 1", pinCount)
	}
}

func TestParseServerFlags_DesignOrigin(t *testing.T) {
	f := parseServerFlags([]string{"--design-origin", "http://localhost:3000"})
	if f.designOrigin != "http://localhost:3000" {
		t.Errorf("designOrigin = %q, want http://localhost:3000", f.designOrigin)
	}
}

func TestParseServerFlags_NoDesignOrigin(t *testing.T) {
	f := parseServerFlags([]string{"plan.md"})
	if f.designOrigin != "" {
		t.Errorf("designOrigin = %q, want empty", f.designOrigin)
	}
}

func TestCreateDesignSession_EmptyOriginIsFatal(t *testing.T) {
	_, err := createDesignSession(&serverConfig{designOrigin: ""})
	if err == nil {
		t.Fatal("createDesignSession with empty origin must error")
	}
}

func TestRunDesign_SmokeFailFatal(t *testing.T) {
	result := runSmokeTest("http://127.0.0.1:19999")
	if !result.fatal {
		t.Error("conn refused must be fatal")
	}
	if result.kind != smokeConnRefused {
		t.Errorf("kind = %v", result.kind)
	}
}

func TestRunDesign_OriginNormalisedToSchemeHost(t *testing.T) {
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

// TestCarryForwardComment_PreservesDesignPinFields guards against silent data
// loss for design pins on round bump. carryForwardComment is called from
// carryForwardAllComments for every file lacking PreviousContent — which is
// the case for design-route entries (no on-disk content). Dropping DOMAnchor,
// PinNumber, Drifted or DriftedOnRound here makes design pins disappear from
// /api/file/comments after POST /api/round-complete.
func TestCarryForwardComment_PreservesDesignPinFields(t *testing.T) {
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
	if !carried.Drifted {
		t.Error("Drifted = false, want true (preserved across rounds)")
	}
	if carried.DriftedOnRound != 2 {
		t.Errorf("DriftedOnRound = %d, want 2", carried.DriftedOnRound)
	}
	if carried.UserID != "u1" {
		t.Errorf("UserID = %q, want u1", carried.UserID)
	}
}

// TestHandleRoundCompleteFiles_DesignPinsSurvive exercises the round-complete
// pipeline end-to-end for a design session: a pin (open and resolved) added
// in round 1 must remain readable in round 2 with anchor identity intact.
// This is the regression for the gap that left two
// rounds.designmode.spec.ts scenarios fixme'd.
func TestHandleRoundCompleteFiles_DesignPinsSurvive(t *testing.T) {
	dir := t.TempDir()
	reviewPath := filepath.Join(dir, "review")

	// Seed a design review file containing an open pin and a resolved pin
	// (both with non-trivial DriftedOnRound to verify it round-trips).
	openAnchor := &DOMAnchor{Pathname: "/", CSSSelector: "#primary-btn", TagChain: []string{"BUTTON"}}
	resolvedAnchor := &DOMAnchor{Pathname: "/", CSSSelector: "#secondary-btn", TagChain: []string{"BUTTON"}}
	cj := CritJSON{
		ReviewType:  "design",
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
		ReviewType:     "design",
		Origin:         "http://localhost:3000",
		ReviewFilePath: reviewPath,
		subscribers:    make(map[chan SSEEvent]struct{}),
		roundComplete:  make(chan struct{}, 1),
		Files: []*FileEntry{
			{Path: "/", FileType: "design-route", Status: "added", Comments: cj.Files["/"].Comments},
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
		t.Fatalf("Comments count = %d after round-complete, want 2 (design pins must survive)", len(fe.Comments))
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
	if open.DriftedOnRound != 1 {
		t.Errorf("open pin DriftedOnRound = %d, want 1 (preserved across rounds)", open.DriftedOnRound)
	}
	if !open.Drifted {
		t.Error("open pin Drifted dropped on carry-forward")
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

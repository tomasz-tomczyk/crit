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

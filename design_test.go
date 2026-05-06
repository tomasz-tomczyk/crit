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

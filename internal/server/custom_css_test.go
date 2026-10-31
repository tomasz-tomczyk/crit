package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// customCSSHome gives the test a fresh home directory and returns the path
// of ~/.crit/custom.css inside it (not created).
func customCSSHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	testutil.SetHome(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".crit"), 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, ".crit", "custom.css")
}

func getCustomCSS(t *testing.T, s *Server, method string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(method, "/custom.css", nil))
	return w
}

func TestCustomCSS_ServesTheFile(t *testing.T) {
	s, _ := newTestServer(t)
	path := customCSSHome(t)
	if err := os.WriteFile(path, []byte(".a { color: red; }"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := getCustomCSS(t, s, "GET")
	if w.Code != http.StatusOK || w.Body.String() != ".a { color: red; }" {
		t.Fatalf("GET /custom.css = %d %q", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q", cc)
	}

	// Edits apply on the next request, without restarting.
	if err := os.WriteFile(path, []byte(".a { color: blue; }"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := getCustomCSS(t, s, "GET").Body.String(); got != ".a { color: blue; }" {
		t.Errorf("after edit = %q", got)
	}
}

func TestCustomCSS_NoContentWithoutAReadableFile(t *testing.T) {
	s, _ := newTestServer(t)
	path := customCSSHome(t)

	if w := getCustomCSS(t, s, "GET"); w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Errorf("no file: %d %q, want 204 and no body", w.Code, w.Body.String())
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if w := getCustomCSS(t, s, "GET"); w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Errorf("unreadable: %d %q, want 204 and no body", w.Code, w.Body.String())
	}
}

// Pages link /custom.css from <head>, before the session is ready.
func TestCustomCSS_NotGatedByWithReady(t *testing.T) {
	s, err := NewServer(nil, frontendFS, "", false, "", "", "test", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	customCSSHome(t)
	if w := getCustomCSS(t, s, "GET"); w.Code == http.StatusServiceUnavailable {
		t.Error("GET /custom.css returned 503 before SetSession")
	}
}

func TestCustomCSS_RejectsPost(t *testing.T) {
	s, _ := newTestServer(t)
	customCSSHome(t)
	if w := getCustomCSS(t, s, "POST"); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /custom.css = %d, want 405", w.Code)
	}
}

func TestHandleConfig_CustomCSSExists(t *testing.T) {
	s, _ := newTestServer(t)
	path := customCSSHome(t)
	getConfig := func() map[string]any {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", "/api/config", nil))
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}

	if resp := getConfig(); resp["custom_css_exists"] != false || resp["custom_css_path"] != path {
		t.Errorf("no file: exists=%v path=%v", resp["custom_css_exists"], resp["custom_css_path"])
	}
	if err := os.WriteFile(path, []byte(".a {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if resp := getConfig(); resp["custom_css_exists"] != true {
		t.Errorf("file: exists=%v, want true", resp["custom_css_exists"])
	}

	// A directory at the path isn't a stylesheet.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if resp := getConfig(); resp["custom_css_exists"] != false {
		t.Errorf("directory: exists=%v, want false", resp["custom_css_exists"])
	}
}

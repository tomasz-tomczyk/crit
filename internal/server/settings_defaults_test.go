package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/config"
	webassets "github.com/tomasz-tomczyk/crit/web"
)

func TestHandleSettingsDefaults_Empty(t *testing.T) {
	// Nil session verifies the route works before session readiness (no withReady).
	srv, err := NewServer(nil, webassets.FS, "", false, "", "", "test", 0, "")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/settings-defaults.js", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Errorf("Content-Type = %q, want application/javascript", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	want := "window.crit=window.crit||{};window.crit.settingsDefaults={};\n"
	if rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body.String(), want)
	}
}

func TestHandleSettingsDefaults_Configured(t *testing.T) {
	srv, err := NewServer(nil, webassets.FS, "", false, "", "", "test", 0, "")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.cfg = config.Config{
		Settings: map[string]any{
			"theme":            "dark",
			"diffMode":         "unified",
			"ignoreWhitespace": true,
			"shortcuts": map[string]string{
				"next_block": "ArrowDown",
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/settings-defaults.js", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	const prefix = "window.crit=window.crit||{};window.crit.settingsDefaults="
	if !strings.HasPrefix(body, prefix) || !strings.HasSuffix(body, ";\n") {
		t.Fatalf("unexpected script wrapper: %q", body)
	}
	jsonPayload := strings.TrimSuffix(strings.TrimPrefix(body, prefix), ";\n")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonPayload), &parsed); err != nil {
		t.Fatalf("unmarshal payload %q: %v", jsonPayload, err)
	}
	if parsed["theme"] != "dark" || parsed["diffMode"] != "unified" || parsed["ignoreWhitespace"] != true {
		t.Errorf("unexpected parsed defaults: %#v", parsed)
	}
	shortcuts, ok := parsed["shortcuts"].(map[string]any)
	if !ok || shortcuts["next_block"] != "ArrowDown" {
		t.Errorf("unexpected parsed shortcuts: %#v", parsed["shortcuts"])
	}
}

func TestHandleSettingsDefaults_MethodNotAllowed(t *testing.T) {
	srv, err := NewServer(nil, webassets.FS, "", false, "", "", "test", 0, "")
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/settings-defaults.js", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

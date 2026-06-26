package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestHandleFinish_BlockedWithoutPromptTrust(t *testing.T) {
	s, session := newTestServer(t)
	dir := session.RepoRoot
	os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{
		"prompts": { "on_finish_approved": "inline:Trusted custom approve text" }
	}`), 0644)

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestHandleFinish_CustomProjectPrompt(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	s, session := newTestServer(t)
	s.homeDir = home
	dir := session.RepoRoot
	s.projectDir = dir

	os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{
		"prompts": { "on_finish_approved": "inline:Proceed with the custom playbook." }
	}`), 0644)

	req := httptest.NewRequest("POST", "/api/project-prompts/trust", strings.NewReader(`{"mode":"always"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("trust status = %d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/api/finish", nil)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("finish status = %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["prompt"] != "Proceed with the custom playbook." {
		t.Fatalf("prompt = %v", resp["prompt"])
	}
}

func TestHandleConfig_ProjectPromptUntrusted(t *testing.T) {
	s, session := newTestServer(t)
	dir := session.RepoRoot
	os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{
		"prompts": { "on_finish_unresolved": "inline:Do the thing" }
	}`), 0644)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["project_prompts_untrusted"] != true {
		t.Fatalf("untrusted = %v", resp["project_prompts_untrusted"])
	}
}

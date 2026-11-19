package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/prompt"
	"github.com/tomasz-tomczyk/crit/internal/session"
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

func TestBuildPromptContext_StoryModeWhenSessionHasStory(t *testing.T) {
	s, sess := newTestServer(t)
	sess.Mode = "git"
	sess.SetStory(&session.Story{Version: 1})

	ctx := s.buildPromptContext(sess, false, nil)
	if ctx.Mode != "story" {
		t.Fatalf("mode = %q, want story", ctx.Mode)
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

func getPromptConfig(t *testing.T, s *Server, path string) (cfg struct {
	Problems []prompt.Problem `json:"prompt_problems"`
	Preview  string           `json:"project_prompt_preview"`
}) {
	t.Helper()
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestHandleConfig_RechecksCurrentPromptFiles(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	s, _ := newTestServer(t)
	s.homeDir = home
	path := filepath.Join(home, ".crit.config.json")
	if err := os.WriteFile(path, []byte(`{"prompts":{"on_finish_approved:files":"file:approval.md"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, endpoint := range []string{"/api/config", "/api/session?check_prompts=1"} {
		cfg := getPromptConfig(t, s, endpoint)
		if len(cfg.Problems) != 1 || cfg.Problems[0].Hook != "on_finish_approved:files" || cfg.Problems[0].Source != "global:approval.md" {
			t.Errorf("%s prompt problems = %+v, want missing approval.md", endpoint, cfg.Problems)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "approval.md"), []byte("Custom approval"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg := getPromptConfig(t, s, "/api/config"); cfg.Problems == nil || len(cfg.Problems) != 0 {
		t.Errorf("rechecked prompt problems = %+v, want an empty array", cfg.Problems)
	}
}

func TestHandleConfig_UntrustedPromptErrorAppearsInTrustPreview(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	s, sess := newTestServer(t)
	path := filepath.Join(sess.RepoRoot, ".crit.config.json")
	if err := os.WriteFile(path, []byte(`{"prompts":{"on_finish_approved":"inline:{{if false}}{{.old_field}}{{end}}"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := getPromptConfig(t, s, "/api/config")
	if len(cfg.Problems) != 0 || !strings.Contains(cfg.Preview, "project:inline") || !strings.Contains(cfg.Preview, "old_field") {
		t.Fatalf("untrusted prompt config = %+v, want project problem in preview only", cfg)
	}
	if err := prompt.SaveTrustChoice(sess.RepoRoot, prompt.TrustDefaults, ""); err != nil {
		t.Fatal(err)
	}
	if cfg := getPromptConfig(t, s, "/api/config"); len(cfg.Problems) != 0 || cfg.Preview != "" {
		t.Fatalf("defaults prompt config = %+v, want no project diagnostics", cfg)
	}
}

func TestHandleFinish_DataDependentErrorLeavesReviewOpen(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	s, sess := newTestServer(t)
	s.homeDir = home
	events := sess.Subscribe()
	defer sess.Unsubscribe(events)
	path := filepath.Join(home, ".crit.config.json")
	// The sample has a session key of "example". The real prompt fails only
	// for this session, so a startup check alone would miss the type error.
	if err := os.WriteFile(path, []byte(`{"prompts":{"on_finish_approved":"inline:{{if eq .session_key \"abcd1234ef01\"}}{{len .total_count}}{{end}}"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg := getPromptConfig(t, s, "/api/config"); len(cfg.Problems) != 0 {
		t.Fatalf("sample reported a real-data-only problem: %+v", cfg.Problems)
	}

	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/api/finish", nil))
	var problem prompt.Problem
	if w.Code != http.StatusUnprocessableEntity || json.Unmarshal(w.Body.Bytes(), &problem) != nil ||
		problem.Hook != "on_finish_approved" || problem.Source != "global:inline" || !strings.Contains(problem.Error, "len") {
		t.Fatalf("finish = %d %s, want structured prompt error", w.Code, w.Body.String())
	}
	select {
	case ev := <-events:
		t.Fatalf("failed finish emitted an event: %+v", ev)
	default:
	}

	if err := os.WriteFile(path, []byte(`{"prompts":{"on_finish_approved":"inline:Fixed approval prompt"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/api/finish", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Fixed approval prompt") {
		t.Fatalf("retry = %d %s, want successful finish with corrected prompt", w.Code, w.Body.String())
	}
	select {
	case ev := <-events:
		if ev.Type != "finish" {
			t.Fatalf("retry event = %+v, want finish", ev)
		}
	default:
		t.Fatal("retry emitted no finish event")
	}
}

func TestRenderProjectPromptPreview_DiscoveredOnly(t *testing.T) {
	s, session := newTestServer(t)
	dir := session.RepoRoot
	promptDir := filepath.Join(dir, ".crit", "prompts")
	if err := os.MkdirAll(promptDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "on_finish_approved.md"), []byte("Custom discovered approve text."), 0644); err != nil {
		t.Fatal(err)
	}

	preview := getPromptConfig(t, s, "/api/config").Preview
	if !strings.Contains(preview, "Custom discovered approve text.") {
		t.Fatalf("preview = %q, want discovered prompt text", preview)
	}
}

func TestRenderProjectPromptPreview_SkipsStockFallback(t *testing.T) {
	s, session := newTestServer(t)
	dir := session.RepoRoot
	promptDir := filepath.Join(dir, ".crit", "prompts")
	if err := os.MkdirAll(promptDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "on_finish_approved.md"), []byte("Only project approved."), 0644); err != nil {
		t.Fatal(err)
	}

	preview := getPromptConfig(t, s, "/api/config").Preview
	if strings.Contains(preview, "The review finished with") {
		t.Fatalf("preview should not include stock unresolved fallback: %q", preview)
	}
	if !strings.Contains(preview, "Only project approved.") {
		t.Fatalf("preview = %q, want project approved text", preview)
	}
}

func TestRenderProjectPromptPreview_ConfigPrompts(t *testing.T) {
	s, session := newTestServer(t)
	dir := session.RepoRoot
	os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{
		"prompts": {
			"on_finish_approved": "inline:Config approved prompt.",
			"on_finish_unresolved": "inline:Config unresolved prompt."
		}
	}`), 0644)

	preview := getPromptConfig(t, s, "/api/config").Preview
	if !strings.Contains(preview, "Config approved prompt.") {
		t.Fatalf("preview = %q, want approved config text", preview)
	}
	if !strings.Contains(preview, "Config unresolved prompt.") {
		t.Fatalf("preview = %q, want unresolved config text", preview)
	}
}

func TestRenderProjectPromptPreview_DiscoveredUnresolved(t *testing.T) {
	s, session := newTestServer(t)
	dir := session.RepoRoot
	promptDir := filepath.Join(dir, ".crit", "prompts")
	if err := os.MkdirAll(promptDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptDir, "on_finish_unresolved.md"), []byte("Discovered unresolved hook."), 0644); err != nil {
		t.Fatal(err)
	}

	preview := getPromptConfig(t, s, "/api/config").Preview
	if !strings.Contains(preview, "Discovered unresolved hook.") {
		t.Fatalf("preview = %q, want discovered unresolved text", preview)
	}
}

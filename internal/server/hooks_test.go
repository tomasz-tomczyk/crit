package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// writeFileGlobal writes ~/.crit.config.json into home with the given JSON body.
func writeFileGlobal(t *testing.T, home, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, ".crit.config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func markerCmd(t *testing.T) (cmd, marker string) {
	t.Helper()
	marker = filepath.Join(t.TempDir(), "marker.txt")
	// Quote the path defensively for sh -c.
	cmd = "printf %s \"$CRIT_SESSION_KEY\" > '" + marker + "'"
	return
}

// TestHandleFinish_RunsGlobalApprovedHook verifies that a globally-configured
// command hook runs synchronously during /api/finish and can read CRIT_* env
// vars. Global hooks do not require the project trust flow.
func TestHandleFinish_RunsGlobalApprovedHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh -c inline hook tested on unix")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	s, session := newTestServer(t)
	s.homeDir = home
	s.projectDir = session.RepoRoot

	cmd, marker := markerCmd(t)
	writeFileGlobal(t, home, `{"hooks":{"on_finish_approved":"inline:`+strings.ReplaceAll(cmd, `"`, `\"`)+`"}}`)

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("hook did not run (marker missing): %v\nstderr output above", err)
	}
	if string(got) != session.SessionKey {
		t.Fatalf("marker = %q want session key %q", string(got), session.SessionKey)
	}
}

// TestHandleFinish_GlobalUnresolvedHookFiresWhenCommentsOpen ensures the
// on_finish_unresolved hook fires (not on_finish_approved) when comments remain.
func TestHandleFinish_GlobalUnresolvedHookFiresWhenCommentsOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh -c inline hook tested on unix")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)

	s, session := newTestServer(t)
	s.homeDir = home
	dir := session.RepoRoot
	s.projectDir = dir

	// Add one unresolved comment so approved=false → on_finish_unresolved fires.
	addUnresolvedComment(t, s, session, dir)

	cmd, marker := markerCmd(t)
	writeFileGlobal(t, home, `{"hooks":{"on_finish_unresolved":"inline:`+strings.ReplaceAll(cmd, `"`, `\"`)+`"}}`)

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.ReadFile(marker); err != nil {
		t.Fatalf("unresolved hook did not run: %v", err)
	}

	// The approved hook should NOT have fired — reject a second marker.
	cmd2, marker2 := markerCmd(t)
	writeFileGlobal(t, home, `{"hooks":{"on_finish_unresolved":"inline:true","on_finish_approved":"inline:`+strings.ReplaceAll(cmd2, `"`, `\"`)+`"}}`)
	req = httptest.NewRequest("POST", "/api/finish", nil)
	// resolving the comment first would approve; here we just re-finish with the
	// same unresolved state — on_finish_unresolved fires again, approved never.
	w = httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if _, err := os.Stat(marker2); err == nil {
		t.Fatalf("approved hook should not have fired during unresolved finish")
	}
}

// TestHandleFinish_ProjectHookBlockedWithoutTrust verifies a checked-in project
// hook is gated by the finish trust flow until the user explicitly trusts it.
func TestHandleFinish_ProjectHookBlockedWithoutTrust(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh -c inline hook tested on unix")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	s, session := newTestServer(t)
	s.homeDir = home
	dir := session.RepoRoot
	s.projectDir = dir

	_, marker := markerCmd(t)
	os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"hooks":{"on_finish_approved":"inline:printf p > '`+marker+`'"}}`), 0o644)

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (project hook untrusted)", w.Code)
	}
}

// TestHandleFinish_ProjectHookRunsAfterTrust confirms that once the project is
// trusted, the project hook executes during finish.
func TestHandleFinish_ProjectHookRunsAfterTrust(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh -c inline hook tested on unix")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	s, session := newTestServer(t)
	s.homeDir = home
	dir := session.RepoRoot
	s.projectDir = dir

	_, marker := markerCmd(t)
	os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"hooks":{"on_finish_approved":"inline:printf p > '`+marker+`'"}}`), 0o644)

	// Trust the project.
	trustReq := httptest.NewRequest("POST", "/api/project-prompts/trust", strings.NewReader(`{"mode":"always"}`))
	trustReq.Header.Set("Content-Type", "application/json")
	tw := httptest.NewRecorder()
	s.ServeHTTP(tw, trustReq)
	if tw.Code != http.StatusOK {
		t.Fatalf("trust status = %d body=%s", tw.Code, tw.Body.String())
	}

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.ReadFile(marker); err != nil {
		t.Fatalf("project hook did not run after trust: %v", err)
	}
}

func TestHandleFinish_ProjectHookRunsWithPromptUntilChangeTrust(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh -c inline hook tested on unix")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	s, session := newTestServer(t)
	s.homeDir = home
	dir := session.RepoRoot
	s.projectDir = dir
	_, marker := markerCmd(t)
	configBody := `{"prompts":{"on_finish_approved":"inline:Approved."},"hooks":{"on_finish_approved":"inline:printf p > '` + marker + `'"}}`
	if err := os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(configBody), 0o644); err != nil {
		t.Fatal(err)
	}
	trustReq := httptest.NewRequest("POST", "/api/project-prompts/trust", strings.NewReader(`{"mode":"until_change"}`))
	trustReq.Header.Set("Content-Type", "application/json")
	tw := httptest.NewRecorder()
	s.ServeHTTP(tw, trustReq)
	if tw.Code != http.StatusOK {
		t.Fatalf("trust status = %d body=%s", tw.Code, tw.Body.String())
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/api/finish", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("finish status = %d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.ReadFile(marker); err != nil {
		t.Fatalf("project hook did not run with prompt+hook until_change trust: %v", err)
	}
}

// TestHandleFinish_DiscoveredFileHook runs a discovered .crit/hooks/*.sh script.
func TestHandleFinish_DiscoveredFileHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file shebang hook tested on unix")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	s, session := newTestServer(t)
	s.homeDir = home
	dir := session.RepoRoot
	s.projectDir = dir

	marker := filepath.Join(dir, "discovered-marker.txt")
	hooksDir := filepath.Join(dir, ".crit", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\nprintf %s \"$CRIT_MODE\" > \"" + marker + "\"\n"
	if err := os.WriteFile(filepath.Join(hooksDir, "on_finish_approved.sh"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	// Discovered .crit/hooks/*.sh triggers the project trust gate.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	trustReq := httptest.NewRequest("POST", "/api/project-prompts/trust", strings.NewReader(`{"mode":"always"}`))
	trustReq.Header.Set("Content-Type", "application/json")
	tw := httptest.NewRecorder()
	s.ServeHTTP(tw, trustReq)
	if tw.Code != http.StatusOK {
		t.Fatalf("trust status = %d body=%s", tw.Code, tw.Body.String())
	}

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("discovered hook did not run: %v", err)
	}
	if string(got) != "files" {
		t.Fatalf("marker = %q want mode files", string(got))
	}
}

// addUnresolvedComment adds a single unresolved comment to the first file so
// handleFinish reports approved=false.
func addUnresolvedComment(t *testing.T, s *Server, sess *Session, dir string) {
	t.Helper()
	for _, f := range sess.Files {
		f.Comments = append(f.Comments, Comment{
			ID:       "c_test1",
			Author:   "Tester",
			Body:     "please fix",
			Resolved: false,
		})
		break
	}
	if err := sess.SyncWriteFiles(); err != nil {
		t.Fatalf("SyncWriteFiles: %v", err)
	}
}

// TestHandleFinish_HookFailureStillReturns200 verifies a failing hook is logged
// but never blocks the finish response.
func TestHandleFinish_HookFailureStillReturns200(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh -c inline hook tested on unix")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	s, _ := newTestServer(t)
	s.homeDir = home

	writeFileGlobal(t, home, `{"hooks":{"on_finish_approved":"inline:exit 7"}}`)

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

// TestHandleConfig_ProjectHookSources lists hook config and discovered scripts
// in the trust payload shown before the user trusts a project.
func TestHandleConfig_ProjectHookSources(t *testing.T) {
	s, session := newTestServer(t)
	dir := session.RepoRoot
	hooksDir := filepath.Join(dir, ".crit", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "on_finish_approved.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".crit.config.json"), []byte(`{"hooks":{"on_finish_unresolved":"file:.crit/hooks/notify.sh"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["project_prompts_untrusted"] != true {
		t.Fatalf("untrusted = %v", resp["project_prompts_untrusted"])
	}
	raw, ok := resp["project_prompt_sources"].([]any)
	if !ok {
		t.Fatalf("sources = %T %v", resp["project_prompt_sources"], resp["project_prompt_sources"])
	}
	sources := make([]string, len(raw))
	for i, v := range raw {
		sources[i], _ = v.(string)
	}
	hasConfig := false
	hasFile := false
	hasDiscovered := false
	for _, src := range sources {
		switch src {
		case "project:.crit.config.json":
			hasConfig = true
		case "project:.crit/hooks/notify.sh":
			hasFile = true
		case "project:.crit/hooks/on_finish_approved.sh":
			hasDiscovered = true
		}
	}
	if !hasConfig || !hasFile || !hasDiscovered {
		t.Fatalf("sources = %v, want config + file + discovered hook paths", sources)
	}
}

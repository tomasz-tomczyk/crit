package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestHandleConfigShareTargetsNeverExposesTokens(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	if err := os.WriteFile(filepath.Join(home, ".crit.config.json"), []byte(`{"share_targets":[{"name":"Acme","url":"https://acme.example","auth":{"token":"secret","user_email":"a@example.com"}}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, _ := newTestServer(t)
	s.configConfigured = true
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "secret") {
		t.Fatal("target token leaked through /api/config")
	}
	var body struct {
		ShareTargets []struct {
			URL      string `json:"url"`
			LoggedIn bool   `json:"auth_logged_in"`
		} `json:"share_targets"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.ShareTargets) != 1 || body.ShareTargets[0].URL != "https://acme.example" || !body.ShareTargets[0].LoggedIn {
		t.Fatalf("targets=%+v", body.ShareTargets)
	}
}

func TestTargetAwareEndpointRejectsUnknownURL(t *testing.T) {
	s, _ := newTestServer(t)
	s.configConfigured = true
	s.cfg = config.Config{ShareTargets: []config.ShareTarget{{URL: "https://acme.example"}}}
	s.projectDir = ""
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/auth/orgs?target_url=https%3A%2F%2Fevil.example", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPullUsesBoundShareBaseURLPathPrefix(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	t.Cleanup(upstream.Close)
	base := upstream.URL + "/crit"

	s, sess := newShareTestServer(t, base, true)
	cfg := `{"share_targets":[{"name":"Prefixed","url":"` + base + `","default":true}]}`
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".crit.config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	s.configConfigured = true
	s.projectDir = sess.RepoRoot
	sess.SetSharedTarget(base+"/r/abc123", base, "delete-tok")

	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/share/pull", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if gotPath != "/crit/api/reviews/abc123/comments" {
		t.Fatalf("pull path=%q, want /crit/api/reviews/abc123/comments", gotPath)
	}
}

func TestPullUnauthorizedClearsTargetAuth(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "authentication required"})
	}))
	t.Cleanup(upstream.Close)

	s, sess := newShareTestServer(t, upstream.URL, true)
	home := os.Getenv("HOME")
	cfg := `{"share_targets":[{"name":"Acme","url":"` + upstream.URL + `","default":true,"auth":{"token":"secret-token"}}]}`
	if err := os.WriteFile(filepath.Join(home, ".crit.config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	s.configConfigured = true
	s.projectDir = sess.RepoRoot
	sess.SetSharedTarget(upstream.URL+"/r/abc123", upstream.URL, "delete-tok")

	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/share/pull", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	raw, err := os.ReadFile(filepath.Join(home, ".crit.config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-token") {
		t.Fatalf("target auth token still present after 401: %s", raw)
	}
}

func TestFreshShareConfigRuntimeOverride(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	if err := os.WriteFile(filepath.Join(home, ".crit.config.json"), []byte(`{"share_targets":[{"url":"https://a.example"},{"url":"https://b.example","default":true}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, sess := newTestServer(t)
	s.configConfigured = true
	s.projectDir = sess.RepoRoot
	empty := ""
	s.cfg.RuntimeShareURL = &empty
	cfg := s.freshShareConfig()
	targets, err := config.ResolveShareTargets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 0 {
		t.Fatalf("empty runtime override should disable targets, got %#v", targets)
	}

	override := "https://a.example"
	s.cfg.RuntimeShareURL = &override
	cfg = s.freshShareConfig()
	targets, err = config.ResolveShareTargets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].URL != "https://a.example" {
		t.Fatalf("runtime override targets=%#v", targets)
	}
}

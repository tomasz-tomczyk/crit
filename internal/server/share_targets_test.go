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

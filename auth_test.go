package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRequestDeviceCode_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/device/code" {
			t.Errorf("expected /api/device/code, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":               "dc_test123",
			"verification_uri_complete": "https://crit.md/auth/cli?code=sc_test456",
			"interval":                  5,
			"expires_in":                900,
		})
	}))
	defer srv.Close()

	code, err := requestDeviceCode(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code.DeviceCode != "dc_test123" {
		t.Errorf("device_code = %q, want dc_test123", code.DeviceCode)
	}
	if code.VerificationURIComplete != "https://crit.md/auth/cli?code=sc_test456" {
		t.Errorf("verification_uri_complete = %q", code.VerificationURIComplete)
	}
	if code.Interval != 5 {
		t.Errorf("interval = %d, want 5", code.Interval)
	}
	if code.ExpiresIn != 900 {
		t.Errorf("expires_in = %d, want 900", code.ExpiresIn)
	}
}

func TestRequestDeviceCode_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Login is not configured on this server.",
		})
	}))
	defer srv.Close()

	_, err := requestDeviceCode(srv.URL)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if got := err.Error(); got != "Login is not configured on this server." {
		t.Errorf("error = %q", got)
	}
}

func TestRequestDeviceCode_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := requestDeviceCode(srv.URL)
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestPollDeviceToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["device_code"] != "dc_test" {
			t.Errorf("device_code = %q, want dc_test", body["device_code"])
		}
		json.NewEncoder(w).Encode(map[string]string{
			"access_token": "crit_abc123",
			"token_type":   "bearer",
			"user_name":    "tomasz",
		})
	}))
	defer srv.Close()

	result, err := pollDeviceToken(srv.URL, "dc_test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.done {
		t.Error("expected done=true on success")
	}
	if result.token.AccessToken != "crit_abc123" {
		t.Errorf("access_token = %q", result.token.AccessToken)
	}
	if result.token.UserName != "tomasz" {
		t.Errorf("user_name = %q", result.token.UserName)
	}
}

func TestPollDeviceToken_Pending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "authorization_pending",
		})
	}))
	defer srv.Close()

	result, err := pollDeviceToken(srv.URL, "dc_test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.done {
		t.Error("expected done=false for pending")
	}
	if result.slowDown {
		t.Error("expected slowDown=false for pending")
	}
}

func TestPollDeviceToken_SlowDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "slow_down",
		})
	}))
	defer srv.Close()

	result, err := pollDeviceToken(srv.URL, "dc_test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.done {
		t.Error("expected done=false for slow_down")
	}
	if !result.slowDown {
		t.Error("expected slowDown=true")
	}
}

func TestPollDeviceToken_Expired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "expired_token",
		})
	}))
	defer srv.Close()

	_, err := pollDeviceToken(srv.URL, "dc_test")
	if err == nil {
		t.Fatal("expected error for expired_token")
	}
}

func TestPollDeviceToken_UnknownError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "server_error",
		})
	}))
	defer srv.Close()

	_, err := pollDeviceToken(srv.URL, "dc_test")
	if err == nil {
		t.Fatal("expected error for unknown error value")
	}
}

func TestPollResult_NextInterval(t *testing.T) {
	t.Run("normal keeps interval", func(t *testing.T) {
		r := pollResult{}
		if got := r.nextInterval(5); got != 5 {
			t.Errorf("nextInterval = %d, want 5", got)
		}
	})

	t.Run("slow_down adds 5", func(t *testing.T) {
		r := pollResult{slowDown: true}
		if got := r.nextInterval(5); got != 10 {
			t.Errorf("nextInterval = %d, want 10", got)
		}
	})

	t.Run("slow_down caps at 60", func(t *testing.T) {
		r := pollResult{slowDown: true}
		if got := r.nextInterval(58); got != 60 {
			t.Errorf("nextInterval = %d, want 60", got)
		}
	})
}

func TestHandlePollError(t *testing.T) {
	tests := []struct {
		name     string
		errStr   string
		wantErr  bool
		slowDown bool
	}{
		{"pending", "authorization_pending", false, false},
		{"slow_down", "slow_down", false, true},
		{"expired", "expired_token", true, false},
		{"unknown", "something_else", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := handlePollError(tt.errStr)
			if tt.wantErr && err == nil {
				t.Error("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if result.slowDown != tt.slowDown {
				t.Errorf("slowDown = %v, want %v", result.slowDown, tt.slowDown)
			}
		})
	}
}

func TestSaveGlobalConfig_PreservesUnknownKeys(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	// Write a config with existing keys.
	initial := `{"share_url": "https://example.com", "port": 3000}` + "\n"
	os.WriteFile(filepath.Join(home, ".crit.config.json"), []byte(initial), 0o600)

	if err := saveAuthSession(tokenResponse{AccessToken: "crit_tok"}); err != nil {
		t.Fatalf("saveAuthSession: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".crit.config.json"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing config: %v", err)
	}

	if _, ok := raw["share_url"]; !ok {
		t.Error("share_url should be preserved")
	}
	if _, ok := raw["port"]; !ok {
		t.Error("port should be preserved")
	}
	if _, ok := raw["auth_token"]; !ok {
		t.Error("auth_token should be set")
	}
}

func TestSaveGlobalConfig_FilePermissions(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	if err := saveAuthSession(tokenResponse{AccessToken: "crit_tok"}); err != nil {
		t.Fatalf("saveAuthSession: %v", err)
	}

	info, err := os.Stat(filepath.Join(home, ".crit.config.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	perm := info.Mode().Perm()
	if runtime.GOOS != "windows" && perm != 0o600 {
		t.Errorf("permissions = %o, want 0600", perm)
	}
}

func TestRevokeToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/auth/token" {
			t.Errorf("expected /api/auth/token, got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer crit_tok" {
			t.Errorf("Authorization = %q", auth)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if !revokeToken(srv.URL, "crit_tok") {
		t.Error("expected revokeToken to return true for 204")
	}
}

func TestRevokeToken_AlreadyGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if !revokeToken(srv.URL, "crit_tok") {
		t.Error("expected revokeToken to return true for 401")
	}
}

func TestRevokeToken_NetworkError(t *testing.T) {
	if revokeToken("http://127.0.0.1:1", "crit_tok") {
		t.Error("expected revokeToken to return false for network error")
	}
}

func TestFetchWhoami_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/auth/whoami" {
			t.Errorf("expected /api/auth/whoami, got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer crit_tok" {
			t.Errorf("Authorization = %q", auth)
		}
		json.NewEncoder(w).Encode(map[string]string{
			"name":  "Tomasz",
			"email": "tomasz@example.com",
		})
	}))
	defer srv.Close()

	who, err := fetchWhoami(context.Background(), srv.URL, "crit_tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if who.Name != "Tomasz" {
		t.Errorf("name = %q, want Tomasz", who.Name)
	}
	if who.Email != "tomasz@example.com" {
		t.Errorf("email = %q", who.Email)
	}
}

func TestFetchWhoami_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := fetchWhoami(context.Background(), srv.URL, "bad_token")
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestShowLoginHint_FirstTime(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	showLoginHint()

	// Verify flag was persisted
	data, err := os.ReadFile(filepath.Join(home, ".crit.config.json"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)
	if string(raw["login_hint_shown"]) != "true" {
		t.Errorf("login_hint_shown = %s, want true", raw["login_hint_shown"])
	}
}

func TestShowLoginHint_AlreadyShown(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	// Write config with hint already shown
	os.WriteFile(filepath.Join(home, ".crit.config.json"),
		[]byte(`{"login_hint_shown": true}`), 0o600)

	showLoginHint()

	// Verify the config was not rewritten (no new keys added)
	data, _ := os.ReadFile(filepath.Join(home, ".crit.config.json"))
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)
	if len(raw) != 1 {
		t.Errorf("expected 1 key in config, got %d", len(raw))
	}
}

func TestShowLoginHint_PreservesExistingConfig(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	// Write config with existing keys
	os.WriteFile(filepath.Join(home, ".crit.config.json"),
		[]byte(`{"share_url": "https://example.com"}`), 0o600)

	showLoginHint()

	data, _ := os.ReadFile(filepath.Join(home, ".crit.config.json"))
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)
	if _, ok := raw["share_url"]; !ok {
		t.Error("share_url should be preserved after showLoginHint")
	}
}

// TestRunAuth_Dispatch verifies the runAuth dispatch logic via subprocess
// (printAuthUsage calls os.Exit).
func TestRunAuth_Dispatch_NoArgs(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_AuthNoArgs", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for auth with no args")
	}
}

func TestHelperProcess_AuthNoArgs(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runAuth(nil)
}

func TestRunAuth_Dispatch_UnknownCommand(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_AuthUnknown", "--")
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for auth with unknown command")
	}
}

func TestHelperProcess_AuthUnknown(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runAuth([]string{"invalid-command"})
}

// TestRunAuthWhoami_NotLoggedIn verifies whoami output when no token is set.
func TestRunAuthWhoami_NotLoggedIn(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_AuthWhoamiNotLoggedIn", "--")
	home := t.TempDir()
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1", "HOME="+home)
	// Ensure CRIT_AUTH_TOKEN is not set
	var env []string
	for _, e := range cmd.Env {
		if !strings.HasPrefix(e, "CRIT_AUTH_TOKEN=") {
			env = append(env, e)
		}
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("whoami exited with error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "Not logged in") {
		t.Errorf("expected 'Not logged in' message, got: %s", out)
	}
}

func TestHelperProcess_AuthWhoamiNotLoggedIn(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runAuthWhoami(nil)
}

// TestRunAuthLogout_NotLoggedIn verifies logout output when no token is set.
func TestRunAuthLogout_NotLoggedIn(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_AuthLogoutNotLoggedIn", "--")
	home := t.TempDir()
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1", "HOME="+home)
	var env []string
	for _, e := range cmd.Env {
		if !strings.HasPrefix(e, "CRIT_AUTH_TOKEN=") {
			env = append(env, e)
		}
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("logout exited with error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "Not logged in") {
		t.Errorf("expected 'Not logged in' message, got: %s", out)
	}
}

func TestHelperProcess_AuthLogoutNotLoggedIn(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runAuthLogout(nil)
}

// TestRunAuthLogout_EnvToken verifies logout message when token is from env var.
func TestRunAuthLogout_EnvToken(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess_AuthLogoutEnvToken", "--")
	home := t.TempDir()
	cmd.Env = append(os.Environ(), "GO_TEST_HELPER=1", "HOME="+home, "CRIT_AUTH_TOKEN=crit_env_token")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("logout exited with error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "CRIT_AUTH_TOKEN") {
		t.Errorf("expected env var message, got: %s", out)
	}
}

func TestHelperProcess_AuthLogoutEnvToken(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "1" {
		return
	}
	runAuthLogout(nil)
}

func TestParseAuthLoginFlags(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		force bool
	}{
		{"no flags", nil, false},
		{"empty args", []string{}, false},
		{"force flag", []string{"--force"}, true},
		{"force with other args", []string{"--verbose", "--force"}, true},
		{"unknown flags ignored", []string{"--unknown"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := parseAuthLoginFlags(tt.args)
			if f.force != tt.force {
				t.Errorf("force = %v, want %v", f.force, tt.force)
			}
		})
	}
}

func TestRevokeToken_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if revokeToken(srv.URL, "crit_tok") {
		t.Error("expected revokeToken to return false for 500")
	}
}

func TestFetchWhoami_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := fetchWhoami(context.Background(), srv.URL, "crit_tok")
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestFetchWhoami_NetworkError(t *testing.T) {
	_, err := fetchWhoami(context.Background(), "http://127.0.0.1:1", "crit_tok")
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestFetchWhoami_NameOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"name": "Tomasz",
		})
	}))
	defer srv.Close()

	who, err := fetchWhoami(context.Background(), srv.URL, "crit_tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if who.Name != "Tomasz" {
		t.Errorf("name = %q, want Tomasz", who.Name)
	}
	if who.Email != "" {
		t.Errorf("email = %q, want empty", who.Email)
	}
}

func TestRequestDeviceCode_NotFoundNoBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		// No JSON body — just 404
	}))
	defer srv.Close()

	_, err := requestDeviceCode(srv.URL)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if got := err.Error(); got != "login is not available on this server" {
		t.Errorf("error = %q, want default message", got)
	}
}

func TestRequestDeviceCode_AcceptsHTTP200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":               "dc_200",
			"verification_uri_complete": "https://crit.md/auth/cli?code=sc_200",
			"interval":                  10,
			"expires_in":                600,
		})
	}))
	defer srv.Close()

	code, err := requestDeviceCode(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code.DeviceCode != "dc_200" {
		t.Errorf("device_code = %q, want dc_200", code.DeviceCode)
	}
}

func TestPollForToken_Integration(t *testing.T) {
	// Server returns authorization_pending for the first 2 requests,
	// then returns a valid token on the 3rd request.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "authorization_pending",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"access_token": "crit_integration_test",
			"user_name":    "testuser",
		})
	}))
	defer srv.Close()

	code := deviceCodeResponse{
		DeviceCode: "dc_integration",
		Interval:   1, // 1 second per poll to keep the test fast
		ExpiresIn:  30,
	}

	token, err := pollForToken(srv.URL, code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token.AccessToken != "crit_integration_test" {
		t.Errorf("access_token = %q, want crit_integration_test", token.AccessToken)
	}
	if token.UserName != "testuser" {
		t.Errorf("user_name = %q, want testuser", token.UserName)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server received %d requests, want 3", got)
	}
}

// TestSaveAuthSession_AtomicRewrite verifies that saveAuthSession writes the
// bearer token and all identity fields in a single atomic config write,
// guarding against the prod regression (#371) where stale identity from a
// previous login survived re-login because the bearer and identity were
// persisted in two separate writes.
func TestSaveAuthSession_AtomicRewrite(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	// Seed config with a full session from a previous user.
	initial := `{
		"auth_token": "old_token",
		"auth_user_id": "old-uuid",
		"auth_user_name": "Old User",
		"auth_user_email": "old@example.com"
	}`
	if err := os.WriteFile(filepath.Join(home, ".crit.config.json"), []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	// New login as a different user — every field should change.
	if err := saveAuthSession(tokenResponse{
		AccessToken: "new_token",
		UserID:      "new-uuid",
		UserName:    "New User",
		UserEmail:   "new@example.com",
	}); err != nil {
		t.Fatalf("saveAuthSession: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, ".crit.config.json"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing config: %v", err)
	}

	check := func(key, want string) {
		t.Helper()
		var got string
		json.Unmarshal(raw[key], &got)
		if got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	check("auth_token", "new_token")
	check("auth_user_id", "new-uuid")
	check("auth_user_name", "New User")
	check("auth_user_email", "new@example.com")
}

// TestSaveAuthSession_RemovesFieldsAbsentFromResponse verifies that an older
// crit-web that returns an empty user_id causes the existing key to be
// removed from disk in the same write — preventing stale identity from
// surviving a re-login when the new token response is partial.
func TestSaveAuthSession_RemovesFieldsAbsentFromResponse(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	initial := `{
		"auth_token": "old_token",
		"auth_user_id": "stale-uuid",
		"auth_user_name": "Stale User",
		"auth_user_email": "stale@example.com"
	}`
	if err := os.WriteFile(filepath.Join(home, ".crit.config.json"), []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	// Older server: returns token + name only.
	if err := saveAuthSession(tokenResponse{
		AccessToken: "fresh_token",
		UserName:    "Just Name",
	}); err != nil {
		t.Fatalf("saveAuthSession: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(home, ".crit.config.json"))
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)

	if _, ok := raw["auth_user_id"]; ok {
		t.Error("auth_user_id should be removed when token response omits it")
	}
	if _, ok := raw["auth_user_email"]; ok {
		t.Error("auth_user_email should be removed when token response omits it")
	}
	var token, name string
	json.Unmarshal(raw["auth_token"], &token)
	json.Unmarshal(raw["auth_user_name"], &name)
	if token != "fresh_token" {
		t.Errorf("auth_token = %q, want fresh_token", token)
	}
	if name != "Just Name" {
		t.Errorf("auth_user_name = %q, want Just Name", name)
	}
}

// TestPollDeviceToken_ParsesUserID verifies the new user_id field on the
// token response is decoded into tokenResponse.UserID.
func TestPollDeviceToken_ParsesUserID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"access_token": "crit_xyz",
			"token_type":   "bearer",
			"user_id":      "abc-123",
			"user_name":    "tomasz",
			"user_email":   "me@example.com",
		})
	}))
	defer srv.Close()

	result, err := pollDeviceToken(srv.URL, "dc_test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.token.UserID != "abc-123" {
		t.Errorf("user_id = %q, want abc-123", result.token.UserID)
	}
	if result.token.UserEmail != "me@example.com" {
		t.Errorf("user_email = %q, want me@example.com", result.token.UserEmail)
	}
}

// TestLazyBackfillAuthUserID_HitsExplicitServer verifies that when the caller
// passes a serverURL (e.g. resolved from --share-url), the whoami request goes
// to that server — not to the default https://crit.md and not to whatever
// CRIT_SHARE_URL points at. Regression for the bug where lazyBackfill always
// re-resolved with an empty override and so always queried the default server.
func TestLazyBackfillAuthUserID_HitsExplicitServer(t *testing.T) {
	setHome(t, t.TempDir())
	// Env var points elsewhere: must NOT be used when caller passes serverURL.
	t.Setenv("CRIT_SHARE_URL", "http://wrong-from-env.invalid")

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.URL.Path != "/api/auth/whoami" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok_explicit" {
			t.Errorf("Authorization = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]string{
			"id":   "user_123",
			"name": "Explicit User",
		})
	}))
	defer srv.Close()

	cfg := Config{
		AuthToken: "tok_explicit",
		// ShareURL deliberately points at a different bogus server to prove we
		// do NOT fall back to it when serverURL is supplied.
		ShareURL: "http://wrong-from-cfg.invalid",
	}
	lazyBackfillAuthUserID(&cfg, srv.URL)

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected 1 whoami hit on explicit server, got %d", got)
	}
	if cfg.AuthUserID != "user_123" {
		t.Errorf("AuthUserID = %q, want user_123", cfg.AuthUserID)
	}
	if cfg.AuthToken != "tok_explicit" {
		t.Errorf("AuthToken cleared unexpectedly: got %q", cfg.AuthToken)
	}
}

// TestLazyBackfillAuthUserID_DoesNotClearOnWrongServer401 verifies that the
// cached identity is NOT wiped when the caller correctly routes whoami to the
// server that owns the token. Previously, runShare with --share-url=<self>
// would call lazyBackfill which queried https://crit.md, got 401 for the
// selfhosted token, and clobbered the cached credentials. With the fix the
// caller's URL is honored so the right server gets the request.
func TestLazyBackfillAuthUserID_DoesNotClearOnWrongServer401(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	// "Wrong" server (analog of https://crit.md) that would 401 the token.
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer wrong.Close()

	// "Right" server (analog of selfhosted instance) that recognizes the token.
	right := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer self_tok" {
			t.Errorf("Authorization = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]string{"id": "self_user"})
	}))
	defer right.Close()

	// Seed the global config as though a prior `crit auth login` against the
	// selfhosted server happened. Note: AuthUserID is empty so backfill runs.
	if err := os.WriteFile(filepath.Join(home, ".crit.config.json"),
		[]byte(`{"auth_token":"self_tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Simulate `crit share --share-url <right>`: the cfg has the env-default
	// crit.md style URL, but the caller passes the resolved selfhosted URL.
	cfg := Config{AuthToken: "self_tok", ShareURL: wrong.URL}
	lazyBackfillAuthUserID(&cfg, right.URL)

	if cfg.AuthToken != "self_tok" {
		t.Errorf("AuthToken cleared on right-server success: got %q", cfg.AuthToken)
	}
	if cfg.AuthUserID != "self_user" {
		t.Errorf("AuthUserID = %q, want self_user", cfg.AuthUserID)
	}

	// Persisted config should still hold the token.
	data, _ := os.ReadFile(filepath.Join(home, ".crit.config.json"))
	if !strings.Contains(string(data), "self_tok") {
		t.Errorf("global config no longer contains auth_token: %s", data)
	}
}

// TestLazyBackfillAuthUserID_ClearsOnRightServer401 verifies that the existing
// "401 wipes cached identity" behaviour still fires — but only against the
// server the caller actually targets. The fix is "ask the right server", not
// "stop clearing tokens on 401".
func TestLazyBackfillAuthUserID_ClearsOnRightServer401(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if err := os.WriteFile(filepath.Join(home, ".crit.config.json"),
		[]byte(`{"auth_token":"revoked_tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Config{AuthToken: "revoked_tok"}
	lazyBackfillAuthUserID(&cfg, srv.URL)

	if cfg.AuthToken != "" {
		t.Errorf("AuthToken should be cleared after 401, got %q", cfg.AuthToken)
	}
	if cfg.AuthUserID != "" {
		t.Errorf("AuthUserID should be cleared after 401, got %q", cfg.AuthUserID)
	}

	data, _ := os.ReadFile(filepath.Join(home, ".crit.config.json"))
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	if v, ok := raw["auth_token"]; ok && v != "" {
		t.Errorf("persisted auth_token not cleared: %v", v)
	}
}

// TestLazyBackfillAuthUserID_FallbackWhenServerEmpty verifies the back-compat
// path: if a caller passes "" the function still resolves via env/config/default.
// CRIT_SHARE_URL takes precedence over the default. This documents the existing
// precedence rules so a future refactor can't silently drop them.
func TestLazyBackfillAuthUserID_FallbackWhenServerEmpty(t *testing.T) {
	setHome(t, t.TempDir())

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		json.NewEncoder(w).Encode(map[string]string{"id": "u1"})
	}))
	defer srv.Close()

	// Env override should win when serverURL == "".
	t.Setenv("CRIT_SHARE_URL", srv.URL)
	cfg := Config{AuthToken: "tok"}
	lazyBackfillAuthUserID(&cfg, "")

	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected env URL to be used as fallback, hits=%d", got)
	}
	if cfg.AuthUserID != "u1" {
		t.Errorf("AuthUserID = %q, want u1", cfg.AuthUserID)
	}
}

// TestLazyBackfillAuthUserID_Skips verifies the early-return guards: nil cfg,
// empty token, and already-cached user id all skip the network call.
func TestLazyBackfillAuthUserID_Skips(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer srv.Close()

	t.Run("nil cfg", func(t *testing.T) {
		lazyBackfillAuthUserID(nil, srv.URL)
	})
	t.Run("empty token", func(t *testing.T) {
		cfg := Config{}
		lazyBackfillAuthUserID(&cfg, srv.URL)
	})
	t.Run("already cached", func(t *testing.T) {
		cfg := Config{AuthToken: "tok", AuthUserID: "u"}
		lazyBackfillAuthUserID(&cfg, srv.URL)
	})

	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("expected 0 server hits across skip cases, got %d", got)
	}
}

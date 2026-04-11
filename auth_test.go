package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestSaveAndRemoveAuthToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Save a token
	if err := saveAuthToken("crit_test_token"); err != nil {
		t.Fatalf("saveAuthToken: %v", err)
	}

	// Verify it was written
	data, err := os.ReadFile(filepath.Join(home, ".crit.config.json"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing config: %v", err)
	}
	var token string
	json.Unmarshal(raw["auth_token"], &token)
	if token != "crit_test_token" {
		t.Errorf("auth_token = %q, want crit_test_token", token)
	}

	// Remove the token
	if err := removeAuthToken(); err != nil {
		t.Fatalf("removeAuthToken: %v", err)
	}

	data, err = os.ReadFile(filepath.Join(home, ".crit.config.json"))
	if err != nil {
		t.Fatalf("reading config after remove: %v", err)
	}
	raw = make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing config after remove: %v", err)
	}
	if _, ok := raw["auth_token"]; ok {
		t.Error("auth_token should be removed after removeAuthToken")
	}
}

func TestSaveGlobalConfig_PreservesUnknownKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Write a config with an existing key
	initial := `{"share_url": "https://example.com", "port": 3000}` + "\n"
	os.WriteFile(filepath.Join(home, ".crit.config.json"), []byte(initial), 0o600)

	// Save auth_token
	if err := saveAuthToken("crit_tok"); err != nil {
		t.Fatalf("saveAuthToken: %v", err)
	}

	// Verify both old keys and new key are present
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
	t.Setenv("HOME", home)

	if err := saveAuthToken("crit_tok"); err != nil {
		t.Fatalf("saveAuthToken: %v", err)
	}

	info, err := os.Stat(filepath.Join(home, ".crit.config.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
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

	name, email, err := fetchWhoami(srv.URL, "crit_tok")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "Tomasz" {
		t.Errorf("name = %q, want Tomasz", name)
	}
	if email != "tomasz@example.com" {
		t.Errorf("email = %q", email)
	}
}

func TestFetchWhoami_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, _, err := fetchWhoami(srv.URL, "bad_token")
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestShowLoginHint_FirstTime(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

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
	t.Setenv("HOME", home)

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
	t.Setenv("HOME", home)

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

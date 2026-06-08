package main

import "testing"

func TestServer_ClearAuthState(t *testing.T) {
	s := &Server{
		authToken: "secret-token",
		cfg: Config{
			AuthToken:     "secret-token",
			AuthUserID:    "user-123",
			AuthUserName:  "Alice",
			AuthUserEmail: "alice@example.com",
		},
	}

	if !s.authLoggedIn() {
		t.Fatal("precondition: expected authLoggedIn=true")
	}

	s.clearAuthState()

	if s.authLoggedIn() {
		t.Error("authLoggedIn still true after clearAuthState")
	}
	if got := s.authUserID(); got != "" {
		t.Errorf("authUserID = %q, want empty", got)
	}
	if got := s.authUserName(); got != "" {
		t.Errorf("authUserName = %q, want empty", got)
	}
	if got := s.authUserEmail(); got != "" {
		t.Errorf("authUserEmail = %q, want empty", got)
	}
	if s.cfg.AuthToken != "" {
		t.Errorf("cfg.AuthToken = %q, want empty", s.cfg.AuthToken)
	}
}

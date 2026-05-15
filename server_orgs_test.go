package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleAuthOrgs(t *testing.T) {
	tests := []struct {
		name           string
		shareURL       string
		authToken      string
		upstreamStatus int
		upstreamBody   string
		wantStatus     int
		wantBody       string
	}{
		{
			name:           "proxies upstream response",
			shareURL:       "UPSTREAM", // replaced with httptest URL
			authToken:      "valid-token",
			upstreamStatus: http.StatusOK,
			upstreamBody:   `[{"id":"org1","name":"Acme"}]`,
			wantStatus:     http.StatusOK,
			wantBody:       `[{"id":"org1","name":"Acme"}]`,
		},
		{
			name:       "empty array when share_url not set",
			shareURL:   "",
			authToken:  "some-token",
			wantStatus: http.StatusOK,
			wantBody:   `[]`,
		},
		{
			name:       "empty array when not authenticated",
			shareURL:   "UPSTREAM",
			authToken:  "",
			wantStatus: http.StatusOK,
			wantBody:   `[]`,
		},
		{
			name:           "empty array when upstream returns error",
			shareURL:       "UPSTREAM",
			authToken:      "valid-token",
			upstreamStatus: http.StatusInternalServerError,
			upstreamBody:   `{"error":"boom"}`,
			wantStatus:     http.StatusOK,
			wantBody:       `[]`,
		},
		{
			name:           "empty array when upstream returns 401",
			shareURL:       "UPSTREAM",
			authToken:      "expired-token",
			upstreamStatus: http.StatusUnauthorized,
			upstreamBody:   `{"error":"unauthorized"}`,
			wantStatus:     http.StatusOK,
			wantBody:       `[]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotBearer string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotBearer = r.Header.Get("Authorization")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.upstreamStatus)
				w.Write([]byte(tt.upstreamBody))
			}))
			defer upstream.Close()

			s, _ := newTestServer(t)
			if tt.shareURL == "UPSTREAM" {
				s.shareURL = upstream.URL
			} else {
				s.shareURL = tt.shareURL
			}
			s.authMu.Lock()
			s.authToken = tt.authToken
			s.authMu.Unlock()

			req := httptest.NewRequest(http.MethodGet, "/api/auth/orgs", nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			// Compare as JSON to ignore whitespace differences
			var got, want any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal response: %v (body: %s)", err, w.Body.String())
			}
			if err := json.Unmarshal([]byte(tt.wantBody), &want); err != nil {
				t.Fatalf("unmarshal want: %v", err)
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("body = %s, want %s", gotJSON, wantJSON)
			}

			// Verify bearer token was forwarded on successful proxy
			if tt.authToken != "" && tt.shareURL == "UPSTREAM" {
				if want := "Bearer " + tt.authToken; gotBearer != want {
					t.Errorf("Authorization = %q, want %q", gotBearer, want)
				}
			}
		})
	}
}

func TestHandleAuthOrgs_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/orgs", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

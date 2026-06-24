package session

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestRunReview_SessionByID_ConnectsToAliveDaemon(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	key := "839f3b4cd5d6"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "browser_clients": true})
		case "/api/session":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"mode": "git"})
		case "/api/review-cycle":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"approved":     false,
				"next_command": "crit --session " + key,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	port, _ := strconv.Atoi(ts.URL[strings.LastIndex(ts.URL, ":")+1:])

	if err := daemon.WriteSessionFile(key, daemon.SessionEntry{
		PID:  os.Getpid(),
		Port: port,
		CWD:  t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}

	orig := ResolveServerConfigFn
	t.Cleanup(func() { ResolveServerConfigFn = orig })
	ResolveServerConfigFn = func(_ []string) (*CLIReviewConfig, error) {
		return &CLIReviewConfig{SessionID: key, NoOpen: true}, nil
	}

	stderr := captureStderr(t, func() {
		if err := RunReview(nil); err != nil {
			t.Errorf("RunReview: %v", err)
		}
	})
	if !strings.Contains(stderr, "Connected to crit daemon") {
		t.Fatalf("stderr = %q, want connected message", stderr)
	}
	if !strings.Contains(stderr, "session "+key) {
		t.Fatalf("stderr = %q, want session id", stderr)
	}
}

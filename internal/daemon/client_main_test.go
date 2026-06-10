package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// TestRunReviewClientRaw_WaitsForReadiness verifies that runReviewClientRaw
// polls /api/session until the daemon is ready (non-503) before hitting
// /api/review-cycle. Regression test for the plan-hook auto-approve bug where
// review-cycle was called immediately after daemon start, got 503, and
// allowed through on error.
func TestRunReviewClientRaw_WaitsForReadiness(t *testing.T) {
	var sessionCalls atomic.Int32
	var reviewCycleCalled atomic.Bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/session":
			n := sessionCalls.Add(1)
			if n <= 2 {
				// First two calls return 503 (still initializing)
				w.WriteHeader(http.StatusServiceUnavailable)
				json.NewEncoder(w).Encode(map[string]string{"status": "loading"})
				return
			}
			// Third call: ready
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

		case "/api/review-cycle":
			reviewCycleCalled.Store(true)
			// Verify session was polled past the 503 phase
			if sessionCalls.Load() < 3 {
				t.Errorf("review-cycle called after only %d session polls, expected >=3", sessionCalls.Load())
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"approved": true, "prompt": ""})

		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	// Extract port from test server URL
	port := 0
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	if port == 0 {
		fmt.Sscanf(ts.URL, "http://localhost:%d", &port)
	}
	if port == 0 {
		t.Fatalf("could not parse port from test server URL: %s", ts.URL)
	}

	entry := SessionEntry{Port: port}
	approved, _ := RunReviewClientRaw(entry, "")

	if !reviewCycleCalled.Load() {
		t.Error("review-cycle was never called")
	}
	if !approved {
		t.Error("expected approved=true")
	}
	if n := sessionCalls.Load(); n < 3 {
		t.Errorf("expected at least 3 session polls (2x503 + 1x200), got %d", n)
	}
}

// TestRunReviewClientRaw_NoReadinessDelay verifies that when the daemon is
// already ready, runReviewClientRaw proceeds immediately without extra delay.
func TestRunReviewClientRaw_NoReadinessDelay(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/session":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case "/api/review-cycle":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"approved": false, "prompt": "fix this"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	port := 0
	fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
	if port == 0 {
		fmt.Sscanf(ts.URL, "http://localhost:%d", &port)
	}
	if port == 0 {
		t.Fatalf("could not parse port from test server URL: %s", ts.URL)
	}

	start := time.Now()
	approved, prompt := RunReviewClientRaw(SessionEntry{Port: port}, "")
	elapsed := time.Since(start)

	if approved {
		t.Error("expected approved=false")
	}
	if prompt != "fix this" {
		t.Errorf("expected prompt='fix this', got %q", prompt)
	}
	if elapsed > 2*time.Second {
		t.Errorf("took %v, expected near-instant when daemon is already ready", elapsed)
	}
}

// TestRunReviewClientRaw_DaemonShutdownDeniesNotApproves regression-tests the
// silent auto-approve bug: when the daemon shuts down mid-request the client
// must return approved=false (with an explanatory prompt), never approved=true.
// Covers two paths: the daemon answers /api/review-cycle with 503+shutdown
// payload, and the daemon's HTTP server force-closes the connection.
func TestRunReviewClientRaw_DaemonShutdownDeniesNotApproves(t *testing.T) {
	t.Run("503 shutdown response", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/session":
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			case "/api/review-cycle":
				w.WriteHeader(http.StatusServiceUnavailable)
				json.NewEncoder(w).Encode(map[string]any{
					"status":   "shutdown",
					"approved": false,
					"prompt":   "crit daemon shut down before review was finished.",
				})
			default:
				http.NotFound(w, r)
			}
		}))
		defer ts.Close()

		port := 0
		fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
		if port == 0 {
			fmt.Sscanf(ts.URL, "http://localhost:%d", &port)
		}

		approved, prompt := RunReviewClientRaw(SessionEntry{Port: port}, "")
		if approved {
			t.Fatal("expected approved=false on daemon shutdown, got true (silent auto-approve)")
		}
		if prompt == "" {
			t.Fatal("expected non-empty prompt explaining shutdown, got empty")
		}
	})

	t.Run("connection drop mid-request", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/session":
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			case "/api/review-cycle":
				// Simulate the HTTP server force-closing the connection
				// (what happens after httpServer.Shutdown's 2s timeout).
				// Use Errorf (not Fatal) inside the handler goroutine —
				// t.Fatal must be called from the test goroutine.
				hj, ok := w.(http.Hijacker)
				if !ok {
					t.Errorf("ResponseWriter does not support Hijacker")
					return
				}
				conn, _, err := hj.Hijack()
				if err != nil {
					t.Errorf("hijack failed: %v", err)
					return
				}
				conn.Close()
			default:
				http.NotFound(w, r)
			}
		}))
		defer ts.Close()

		port := 0
		fmt.Sscanf(ts.URL, "http://127.0.0.1:%d", &port)
		if port == 0 {
			fmt.Sscanf(ts.URL, "http://localhost:%d", &port)
		}

		approved, prompt := RunReviewClientRaw(SessionEntry{Port: port}, "")
		if approved {
			t.Fatal("expected approved=false on connection drop, got true (silent auto-approve)")
		}
		if prompt == "" {
			t.Fatal("expected non-empty prompt explaining the failure, got empty")
		}
	})
}

// TestWaitForDaemonReady_SurfacesDaemonLog verifies that when the daemon is
// unreachable (e.g. crashed during init), the error message surfaces the daemon
// log contents instead of the misleading "connection refused" network error.
func TestWaitForDaemonReady_SurfacesDaemonLog(t *testing.T) {
	t.Run("surfaces daemon log on connection error", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()

		dir := t.TempDir()
		testutil.SetHome(t, dir)

		sessDir := filepath.Join(dir, ".crit", "sessions")
		os.MkdirAll(sessDir, 0700)
		os.WriteFile(filepath.Join(sessDir, "testkey123.log"), []byte("Error: not in a git repository"), 0600)

		client := &http.Client{Timeout: 1 * time.Second}
		_, _, err = waitForDaemonReady(client, "", port, "testkey123")
		if err == nil {
			t.Fatal("expected error for unreachable daemon")
		}
		if !strings.Contains(err.Error(), "not in a git repository") {
			t.Errorf("expected daemon log message in error, got: %v", err)
		}
	})

	t.Run("falls back to network error when no log", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()

		client := &http.Client{Timeout: 1 * time.Second}
		_, _, err = waitForDaemonReady(client, "", port, "")
		if err == nil {
			t.Fatal("expected error for unreachable daemon")
		}
		if !strings.Contains(err.Error(), "could not reach daemon") {
			t.Errorf("expected 'could not reach daemon' fallback, got: %v", err)
		}
	})
}

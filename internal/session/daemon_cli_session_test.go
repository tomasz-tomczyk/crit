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

func TestConnectOrStartDaemon_AliveSession(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	key := "839f3b4cd5d6"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "browser_clients": true})
	}))
	defer ts.Close()
	port, _ := strconv.Atoi(ts.URL[strings.LastIndex(ts.URL, ":")+1:])
	if err := daemon.WriteSessionFile(key, daemon.SessionEntry{PID: os.Getpid(), Port: port}); err != nil {
		t.Fatal(err)
	}

	stderr := captureStderr(t, func() {
		entry, started, err := connectOrStartDaemon(key, nil, true, "")
		if err != nil {
			t.Fatalf("connectOrStartDaemon: %v", err)
		}
		if started {
			t.Fatal("expected existing session, not new daemon")
		}
		if entry.Port != port {
			t.Fatalf("port = %d, want %d", entry.Port, port)
		}
	})
	if !strings.Contains(stderr, "session "+key) {
		t.Fatalf("stderr = %q, want session id", stderr)
	}
}

func TestConnectOrStartDaemon_StartsDaemon(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	key := "839f3b4cd5d6"

	orig := startDaemonForConnect
	startDaemonForConnect = func(gotKey string, _ []string) (daemon.SessionEntry, error) {
		if gotKey != key {
			t.Fatalf("key = %q", gotKey)
		}
		return daemon.SessionEntry{PID: 42, Port: 3001}, nil
	}
	t.Cleanup(func() { startDaemonForConnect = orig })

	stderr := captureStderr(t, func() {
		entry, started, err := connectOrStartDaemon(key, nil, true, "")
		if err != nil {
			t.Fatalf("connectOrStartDaemon: %v", err)
		}
		if !started {
			t.Fatal("expected new daemon start")
		}
		if entry.Port != 3001 {
			t.Fatalf("port = %d", entry.Port)
		}
	})
	if !strings.Contains(stderr, "Started crit daemon") {
		t.Fatalf("stderr = %q", stderr)
	}
	if !strings.Contains(stderr, "session "+key) {
		t.Fatalf("stderr = %q", stderr)
	}
}

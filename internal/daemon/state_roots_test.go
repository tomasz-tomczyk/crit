package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLiveSessionsIn(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer ts.Close()
	port, _ := strconv.Atoi(ts.URL[strings.LastIndex(ts.URL, ":")+1:])

	dir := filepath.Join(t.TempDir(), "sessions")
	writeJSON(t, filepath.Join(dir, "aaaaaaaaaaaa.json"), SessionEntry{PID: os.Getpid(), Port: port, CWD: "/repo"})
	writeJSON(t, filepath.Join(dir, "bbbbbbbbbbbb.json"), SessionEntry{PID: 0, Port: port}) // dead
	if err := os.WriteFile(filepath.Join(dir, "cccccccccccc.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "aaaaaaaaaaaa.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	alive := LiveSessionsIn(dir)
	if len(alive) != 1 || alive[0].CWD != "/repo" {
		t.Fatalf("LiveSessionsIn = %+v, want only the live session", alive)
	}
	// Dead session files are left for the owner of that root to clean.
	if _, err := os.Stat(filepath.Join(dir, "bbbbbbbbbbbb.json")); err != nil {
		t.Errorf("dead session file removed: %v", err)
	}
	if got := LiveSessionsIn(filepath.Join(t.TempDir(), "missing")); got != nil {
		t.Errorf("missing dir = %+v, want nil", got)
	}
}

func TestAllReviewsDirsAndFindReviewDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps the legacy layout without XDG variables")
	}
	home := t.TempDir()
	testutil.SetHome(t, home) // seeds ~/.crit
	xdg := filepath.Join(home, ".local", "state", "crit")
	if err := os.MkdirAll(xdg, 0o755); err != nil {
		t.Fatal(err)
	}

	dirs, err := AllReviewsDirs()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(xdg, "reviews"), filepath.Join(home, ".crit", "reviews")}
	if len(dirs) != 2 || dirs[0] != want[0] || dirs[1] != want[1] {
		t.Fatalf("AllReviewsDirs = %q, want %q", dirs, want)
	}

	if _, ok := FindReviewDir("aaaaaaaaaaaa"); ok {
		t.Fatal("found a review that does not exist")
	}
	legacyReview := filepath.Join(home, ".crit", "reviews", "aaaaaaaaaaaa")
	writeJSON(t, filepath.Join(legacyReview, "review.json"), map[string]string{})
	if got, ok := FindReviewDir("aaaaaaaaaaaa"); !ok || got != legacyReview {
		t.Fatalf("FindReviewDir = %q, %v; want the legacy copy", got, ok)
	}
	// The root in use wins when both have the review.
	xdgReview := filepath.Join(xdg, "reviews", "aaaaaaaaaaaa")
	writeJSON(t, filepath.Join(xdgReview, "review.json"), map[string]string{})
	if got, ok := FindReviewDir("aaaaaaaaaaaa"); !ok || got != xdgReview {
		t.Fatalf("FindReviewDir = %q, %v; want the XDG copy", got, ok)
	}
}

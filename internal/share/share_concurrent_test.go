package share

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestConcurrentShareLock verifies parallel `crit share` invocations for the
// same review identity create only one hosted review (single POST).
func TestConcurrentShareLock(t *testing.T) {
	binary := critBinaryForTest(t)

	var postCount atomic.Int32
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/reviews" {
			n := postCount.Add(1)
			time.Sleep(100 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"url":          fmt.Sprintf("http://stub/r/token-%d", n),
				"delete_token": fmt.Sprintf("delete-%d", n),
			})
			return
		}
		if r.Method == http.MethodPut {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"url":          "http://stub/r/token-1",
				"review_round": 1,
				"changed":      true,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer stub.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dummy.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, err := runCritShareCmd(t, binary, dir, stub.URL, dir)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("crit share failed: %v", err)
		}
	}

	if got := postCount.Load(); got != 1 {
		t.Fatalf("expected 1 POST to crit-web, got %d", got)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".crit", "review.json"))
	if err != nil {
		t.Fatalf("reading review.json: %v", err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("parsing review.json: %v", err)
	}
	if cj.DeleteToken != "delete-1" {
		t.Errorf("delete_token = %q, want delete-1", cj.DeleteToken)
	}
	if cj.ShareURL != "http://stub/r/token-1" {
		t.Errorf("share_url = %q, want http://stub/r/token-1", cj.ShareURL)
	}
	if _, err := os.Stat(filepath.Join(dir, ".crit", "share.lock")); err != nil {
		t.Errorf("share.lock missing: %v", err)
	}
}

func critBinaryForTest(t *testing.T) string {
	t.Helper()
	if b := os.Getenv("CRIT_BINARY"); b != "" {
		return b
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(wd, "..", "..", "crit"),
		filepath.Join(wd, "crit"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(wd, "..", "..", "crit")
}

func runCritShareCmd(t *testing.T, binary, dir, stubURL, outputDir string) (string, error) {
	t.Helper()
	cmd := exec.Command(binary, "share", "--share-url", stubURL, "--output", outputDir, "dummy.md")
	cmd.Dir = dir
	cmd.Env = envWithout("CRIT_AUTH_TOKEN=", "HOME=", "CRIT_SHARE_URL=")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func envWithout(prefixes ...string) []string {
	var env []string
outer:
	for _, e := range os.Environ() {
		for _, p := range prefixes {
			if strings.HasPrefix(e, p) {
				continue outer
			}
		}
		env = append(env, e)
	}
	return env
}

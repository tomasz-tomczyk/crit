package share

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

// TestConcurrentShareLock verifies parallel share attempts for the same review
// identity create only one hosted review (single POST).
func TestConcurrentShareLock(t *testing.T) {
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
	critPath := filepath.Join(dir, ".crit")
	files := []ShareFile{{Path: "dummy.md", Content: "# test\n"}}
	sharePaths := []string{"dummy.md"}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			errs <- session.WithShareLock(critPath, func() error {
				return runShareUnderLock(critPath, files, sharePaths, stub.URL, "", "", "", "", false)
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("share under lock failed: %v", err)
		}
	}

	if got := postCount.Load(); got != 1 {
		t.Fatalf("expected 1 POST to crit-web, got %d", got)
	}

	data, err := os.ReadFile(filepath.Join(critPath, "review.json"))
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
	if _, err := os.Stat(session.ReviewPathsFor(critPath).ShareLock); err != nil {
		t.Errorf("share.lock missing: %v", err)
	}
}

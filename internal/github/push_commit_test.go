package github

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
)

// TestRunPushPostReview_CommitID checks that --commit reaches the
// create-review request as commit_id, and that without it the key is absent.
func TestRunPushPostReview_CommitID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-gh shim is a POSIX shell script; not portable to Windows")
	}
	sha := strings.Repeat("b", 40)
	tests := []struct {
		name      string
		commitSHA string
	}{
		{name: "pinned", commitSHA: sha},
		{name: "not pinned", commitSHA: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			payloadFile := filepath.Join(dir, "payload.json")
			// The first gh call creates the review (stdin = payload); the
			// second lists its comments.
			script := `#!/bin/sh
if [ ! -f "` + payloadFile + `" ]; then
  cat > "` + payloadFile + `"
  echo '{"id": 5}'
else
  cat >/dev/null
  echo '[]'
fi
`
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

			b := pushBuckets{Postable: []scopedComment{{
				Path:    "a.go",
				Comment: session.Comment{ID: "c1", StartLine: 3, EndLine: 3, Body: "fix"},
			}}}
			ctx := pushContext{prNumber: 7, event: "APPROVE", commitSHA: tt.commitSHA}
			posted, failed, _, _ := runPushPostReview(ctx, b, nil)
			if failed || posted != 1 {
				t.Fatalf("runPushPostReview = (%d, failed=%v), want (1, false)", posted, failed)
			}
			data, err := os.ReadFile(payloadFile)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatalf("unmarshal payload %q: %v", data, err)
			}
			got, ok := payload["commit_id"]
			if tt.commitSHA == "" {
				if ok {
					t.Fatalf("payload has commit_id %v without --commit", got)
				}
				return
			}
			if got != tt.commitSHA {
				t.Fatalf("commit_id = %v, want %q", got, tt.commitSHA)
			}
		})
	}
}

package comment

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// crit comment prints the new comment's id so an agent can refer to it in a
// later comment (`c_…` renders as a link to that comment). Each add helper
// must return the id it stored.
func TestAddCommentAtPathReturnsStoredID(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		add    func(critPath string) (string, error)
		stored func(cj session.CritJSON) string
	}{
		{
			name:   "line comment",
			prefix: "c_",
			add: func(p string) (string, error) {
				return addCommentToCritJSONAtPath("main.go", 3, 4, "body", "agent", "", p, session.InheritedScope{})
			},
			stored: func(cj session.CritJSON) string { return cj.Files["main.go"].Comments[0].ID },
		},
		{
			name:   "file comment",
			prefix: "c_",
			add: func(p string) (string, error) {
				return addFileCommentToCritJSONAtPath("main.go", "body", "agent", "", p, session.InheritedScope{})
			},
			stored: func(cj session.CritJSON) string { return cj.Files["main.go"].Comments[0].ID },
		},
		{
			name:   "review comment",
			prefix: "r_",
			add: func(p string) (string, error) {
				return addReviewCommentToCritJSONAtPath("body", "agent", "", p, session.InheritedScope{})
			},
			stored: func(cj session.CritJSON) string { return cj.ReviewComments[0].ID },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			critPath := filepath.Join(t.TempDir(), "review.json")
			id, err := tt.add(critPath)
			if err != nil {
				t.Fatal(err)
			}
			if len(id) <= len(tt.prefix) || id[:len(tt.prefix)] != tt.prefix {
				t.Fatalf("id = %q, want prefix %q", id, tt.prefix)
			}
			cj, err := loadCritJSON(critPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := tt.stored(cj); got != id {
				t.Errorf("returned id %q, stored id %q", id, got)
			}
		})
	}
}

// The crit-cli skills tell agents to read the id from this output, so pin
// the format end to end through RunComment.
func TestRunCommentPrintsNewID(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want *regexp.Regexp
	}{
		{"line", []string{"main.go:3", "body"}, regexp.MustCompile(`^Added comment (c_[0-9a-f]+) on main\.go:3\n$`)},
		{"file", []string{"main.go", "body"}, regexp.MustCompile(`^Added file comment (c_[0-9a-f]+) on main\.go\n$`)},
		{"review", []string{"body"}, regexp.MustCompile(`^Added review comment (r_[0-9a-f]+)\n$`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			testutil.SetHome(t, t.TempDir())
			t.Chdir(dir)
			if err := os.WriteFile("main.go", []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			out := captureStdout(t, func() error {
				return RunComment(append([]string{"--output", dir, "--author", "agent"}, tt.args...))
			})
			m := tt.want.FindStringSubmatch(out)
			if m == nil {
				t.Fatalf("output %q does not match %s", out, tt.want)
			}
			raw, err := os.ReadFile(findReviewFile(t, dir))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), `"id": "`+m[1]+`"`) {
				t.Errorf("printed id %s is not in the review file", m[1])
			}
		})
	}
}

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := fn()
	_ = w.Close()
	os.Stdout = old
	if runErr != nil {
		t.Fatal(runErr)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// findReviewFile returns the single review.json written under dir.
func findReviewFile(t *testing.T, dir string) string {
	t.Helper()
	var found []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "review.json" {
			found = append(found, p)
		}
		return nil
	})
	if len(found) != 1 {
		t.Fatalf("want one review.json under %s, found %v", dir, found)
	}
	return found[0]
}

func TestAddCommentAtPathRejectsTraversalWithoutID(t *testing.T) {
	critPath := filepath.Join(t.TempDir(), "review.json")
	if id, err := addCommentToCritJSONAtPath("../x.go", 1, 1, "b", "", "", critPath, session.InheritedScope{}); err == nil || id != "" {
		t.Errorf("line: id=%q err=%v, want empty id and an error", id, err)
	}
	if id, err := addFileCommentToCritJSONAtPath("/etc/passwd", "b", "", "", critPath, session.InheritedScope{}); err == nil || id != "" {
		t.Errorf("file: id=%q err=%v, want empty id and an error", id, err)
	}
}

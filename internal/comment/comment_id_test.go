package comment

import (
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/session"
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

func TestAddCommentAtPathRejectsTraversalWithoutID(t *testing.T) {
	critPath := filepath.Join(t.TempDir(), "review.json")
	if id, err := addCommentToCritJSONAtPath("../x.go", 1, 1, "b", "", "", critPath, session.InheritedScope{}); err == nil || id != "" {
		t.Errorf("line: id=%q err=%v, want empty id and an error", id, err)
	}
	if id, err := addFileCommentToCritJSONAtPath("/etc/passwd", "b", "", "", critPath, session.InheritedScope{}); err == nil || id != "" {
		t.Errorf("file: id=%q err=%v, want empty id and an error", id, err)
	}
}

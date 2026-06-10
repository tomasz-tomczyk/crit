package comment

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLooksLikeLineSpec(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"42", true},
		{"10-25", true},
		{"abc", false},
		{"10-abc", false},
	}
	for _, c := range cases {
		if got := looksLikeLineSpec(c.in); got != c.want {
			t.Errorf("looksLikeLineSpec(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestPlural(t *testing.T) {
	if plural(1) != "" {
		t.Errorf("plural(1) = %q, want empty", plural(1))
	}
	if plural(2) != "s" {
		t.Errorf("plural(2) = %q, want s", plural(2))
	}
}

func TestFileExistsOnDiskOrSession_OnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exists.go")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileExistsOnDiskOrSession(path, dir) {
		t.Error("expected true for file on disk")
	}
}

func TestFileExistsOnDiskOrSession_InReviewFile(t *testing.T) {
	dir := t.TempDir()
	cj := CritJSON{Files: map[string]CritJSONFile{
		"session-only.go": {Status: "modified"},
	}}
	if err := saveCritJSON(filepath.Join(dir, ".crit"), cj); err != nil {
		t.Fatal(err)
	}
	if !fileExistsOnDiskOrSession("session-only.go", dir) {
		t.Error("expected true for path in review file")
	}
}

func TestFileExistsOnDiskOrSession_Missing(t *testing.T) {
	dir := t.TempDir()
	if fileExistsOnDiskOrSession("missing.go", dir) {
		t.Error("expected false for missing path")
	}
}

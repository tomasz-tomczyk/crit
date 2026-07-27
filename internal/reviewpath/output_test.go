package reviewpath

import (
	"path/filepath"
	"testing"
)

func TestFromOutputDir(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "reviews")
	got, err := FromOutputDir(nested)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(nested, ".crit")
	if got != want {
		t.Fatalf("review path = %q, want %q", got, want)
	}
}

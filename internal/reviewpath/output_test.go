package reviewpath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdentity(t *testing.T) {
	root := t.TempDir()
	got, err := Identity(root, "deadbeef1234")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "reviews", "deadbeef1234")
	if got != want {
		t.Fatalf("Identity = %q, want %q", got, want)
	}
}

func TestReviewsDir(t *testing.T) {
	root := t.TempDir()
	got, err := ReviewsDir(root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "reviews")
	if got != want {
		t.Fatalf("ReviewsDir = %q, want %q", got, want)
	}
}

func TestHasLegacyIdentity(t *testing.T) {
	root := t.TempDir()
	if HasLegacyIdentity(root) {
		t.Fatal("expected no legacy identity in empty root")
	}
	if err := os.MkdirAll(filepath.Join(root, ".crit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !HasLegacyIdentity(root) {
		t.Fatal("expected legacy .crit folder to be detected")
	}
}

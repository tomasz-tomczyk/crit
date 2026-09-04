package reviewpath

import (
	"errors"
	"path/filepath"
	"strings"
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

func TestReviewsDirAbsError(t *testing.T) {
	orig := absPath
	t.Cleanup(func() { absPath = orig })
	absPath = func(string) (string, error) {
		return "", errors.New("abs failed")
	}
	if _, err := ReviewsDir("x"); err == nil || !strings.Contains(err.Error(), "abs failed") {
		t.Fatalf("ReviewsDir error = %v", err)
	}
	if _, err := Identity("x", "key"); err == nil || !strings.Contains(err.Error(), "abs failed") {
		t.Fatalf("Identity error = %v", err)
	}
}

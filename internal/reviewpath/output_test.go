package reviewpath

import (
	"errors"
	"os"
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

func TestHasLegacyIdentity_JSONFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".crit.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasLegacyIdentity(root) {
		t.Fatal("expected legacy .crit.json to be detected")
	}
}

func TestLegacyIdentityPath(t *testing.T) {
	root := t.TempDir()
	got, err := LegacyIdentityPath(root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, ".crit")
	if got != want {
		t.Fatalf("LegacyIdentityPath = %q, want %q", got, want)
	}
}

func TestIdentityUnderDataRoot(t *testing.T) {
	t.Run("keys under reviews when the root is clean", func(t *testing.T) {
		root := t.TempDir()
		got, err := IdentityUnderDataRoot(root, "deadbeef1234")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(root, "reviews", "deadbeef1234")
		if got != want {
			t.Fatalf("IdentityUnderDataRoot = %q, want %q", got, want)
		}
	})

	t.Run("honors a legacy .crit folder", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".crit"), 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := IdentityUnderDataRoot(root, "deadbeef1234")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(root, ".crit")
		if got != want {
			t.Fatalf("IdentityUnderDataRoot = %q, want legacy path %q", got, want)
		}
	})

	t.Run("honors a legacy .crit.json flat file", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".crit.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := IdentityUnderDataRoot(root, "deadbeef1234")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(root, ".crit")
		if got != want {
			t.Fatalf("IdentityUnderDataRoot = %q, want legacy path %q", got, want)
		}
	})
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
	if _, err := LegacyIdentityPath("x"); err == nil || !strings.Contains(err.Error(), "abs failed") {
		t.Fatalf("LegacyIdentityPath error = %v", err)
	}
	if HasLegacyIdentity("x") {
		t.Fatal("HasLegacyIdentity should be false when Abs fails")
	}
}

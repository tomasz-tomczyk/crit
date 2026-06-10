package vcs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasJJDirFrom(t *testing.T) {
	dir := t.TempDir()
	if hasJJDirFromTest(dir) {
		t.Error("expected false")
	}
	if err := os.Mkdir(filepath.Join(dir, ".jj"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !hasJJDirFromTest(dir) {
		t.Error("expected true with .jj present")
	}
}

func TestHasSLDirFrom(t *testing.T) {
	dir := t.TempDir()
	if hasSLDirFromTest(dir) {
		t.Error("expected false")
	}
	if err := os.Mkdir(filepath.Join(dir, ".sl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !hasSLDirFromTest(dir) {
		t.Error("expected true with .sl present")
	}
}

func TestHasGitSLDir_UnderCwd(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "sl"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if !hasGitSLDirTest() {
		t.Error("expected true when .git/sl exists under cwd")
	}
}

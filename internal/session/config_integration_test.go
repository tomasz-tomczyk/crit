package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func TestNewSessionFromGitWithIgnore(t *testing.T) {
	dir := testutil.InitTestRepo(t)

	vcs.ResetDefaultBranchOnceForTest()

	testutil.Git(t, dir, "checkout", "-b", "feature")
	testutil.WriteFile(t, filepath.Join(dir, "main.go"), "package main\n")
	testutil.WriteFile(t, filepath.Join(dir, "service.pb.go"), "package main\n// generated\n")
	testutil.WriteFile(t, filepath.Join(dir, "vendor", "lib.go"), "package vendor\n")
	testutil.WriteFile(t, filepath.Join(dir, "README.md"), "# Updated\n")
	testutil.Git(t, dir, "add", ".")
	testutil.Git(t, dir, "commit", "-m", "add files")

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	patterns := []string{"*.pb.go", "vendor/"}
	vc := vcs.DetectVCS("")
	if vc == nil {
		t.Fatal("expected git vcs")
	}
	sess, err := NewGitSession(vc, patterns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	paths := make(map[string]bool)
	for _, f := range sess.Files {
		paths[f.Path] = true
	}
	if paths["service.pb.go"] {
		t.Error("service.pb.go should be ignored")
	}
	if paths["vendor/lib.go"] {
		t.Error("vendor/lib.go should be ignored")
	}
	if !paths["main.go"] {
		t.Error("main.go should be present")
	}
	if !paths["README.md"] {
		t.Error("README.md should be present")
	}

	if len(sess.IgnorePatterns) != 2 {
		t.Errorf("session.IgnorePatterns = %v, want 2 entries", sess.IgnorePatterns)
	}
}

func TestNewSessionFromFilesWithIgnore(t *testing.T) {
	dir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(dir, "main.go"), "package main\n")
	testutil.WriteFile(t, filepath.Join(dir, "generated", "types.go"), "package gen\n")
	testutil.WriteFile(t, filepath.Join(dir, "app.min.js"), "// minified\n")
	testutil.WriteFile(t, filepath.Join(dir, "readme.txt"), "hello\n")

	patterns := []string{"generated/"}
	sess, err := NewSessionFromFiles([]string{dir}, patterns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, f := range sess.Files {
		rel, _ := filepath.Rel(dir, f.AbsPath)
		if strings.HasPrefix(rel, "generated/") || strings.HasPrefix(rel, "generated\\") {
			t.Errorf("file %s should have been ignored by generated/ pattern", f.Path)
		}
	}
}

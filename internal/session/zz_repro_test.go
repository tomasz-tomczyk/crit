package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// TestReproLazyThreshold replicates TestNewSessionFromGitLazyThreshold and
// dumps everything that could explain the "expected 120 files, got 121"
// failure seen on GitHub Actions.
func TestReproLazyThreshold(t *testing.T) {
	dir := initTestRepo(t)
	vcs.SetDefaultBranchOverride("")
	defer func() { vcs.SetDefaultBranchOverride("") }()

	gitT(t, dir, "checkout", "-b", "feature-many-files")
	for i := 0; i < 120; i++ {
		name := fmt.Sprintf("file%03d.go", i)
		writeFile(t, filepath.Join(dir, name), fmt.Sprintf("package main\n// file %d\n", i))
	}
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-m", "add 120 files")

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	run := func(args ...string) string {
		out, err := exec.Command("git", args...).Output()
		if err != nil {
			return fmt.Sprintf("<err: %v>", err)
		}
		return strings.TrimSpace(string(out))
	}

	t.Logf("git version: %s", run("--version"))
	t.Logf("cwd: %s", dir)
	t.Logf("env GIT_: %s", strings.Join(filterEnv("GIT_"), " | "))
	t.Logf("env HOME=%s TMPDIR=%s CI=%s", os.Getenv("HOME"), os.Getenv("TMPDIR"), os.Getenv("CI"))
	t.Logf("ls-files --others: %q", run("ls-files", "--others", "--exclude-standard"))
	t.Logf("ls-files --others (no exclude): %q", run("ls-files", "--others"))
	t.Logf("status --porcelain: %q", run("status", "--porcelain"))
	mb, err := vcs.MergeBase(vcs.DefaultBaseRef())
	t.Logf("mergeBase=%q err=%v", mb, err)
	diff := run("diff", mb, "--name-status")
	t.Logf("diff lines: %d", len(strings.Split(diff, "\n")))
	if !strings.Contains(diff, "README") && strings.Count(diff, "\n") > 120 {
		t.Logf("DIFF CONTAINS EXTRA: %q", diff)
	}

	s, err := NewSessionFromGit(nil)
	if err != nil {
		t.Fatalf("NewSessionFromGit failed: %v", err)
	}
	re := regexp.MustCompile(`^file\d{3}\.go$`)
	for _, f := range s.Files {
		if !re.MatchString(f.Path) {
			t.Logf("UNEXPECTED FILE: %q (status=%q)", f.Path, f.Status)
		}
	}
	t.Logf("total files: %d (expected 120)", len(s.Files))
	if len(s.Files) != 120 {
		t.Fatalf("expected 120, got %d", len(s.Files))
	}
}

func filterEnv(prefix string) []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	return out
}

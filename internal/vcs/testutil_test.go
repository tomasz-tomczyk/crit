package vcs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	return testutil.InitTestRepo(t)
}

// gitT runs git in dir via testutil.Git, which strips the inherited GIT_*
// and HOME vars (git hooks set GIT_DIR / GIT_INDEX_FILE) and turns off
// auto-maintenance.
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return testutil.Git(t, dir, args...)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

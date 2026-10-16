package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestBinaryDoesNotImportTestutil guards against test helpers leaking into
// the shipped binary. testutil's init() unsets every GIT_* env var, so a
// production import silently drops the user's GIT_DIR, GIT_SSH_COMMAND, etc.
func TestBinaryDoesNotImportTestutil(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	out, err := exec.Command(goBin, "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	for _, pkg := range strings.Fields(string(out)) {
		if strings.HasSuffix(pkg, "/internal/testutil") {
			t.Fatalf("cmd/crit depends on %s; move the import into a _test.go file", pkg)
		}
	}
}

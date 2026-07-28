package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/server"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// TestPreflightCheck_CleanRepo verifies that running crit in a clean
// repo with no changes returns the user-facing message instead of letting
// the daemon spawn and crash with a misleading "could not reach daemon" error.
func TestPreflightCheck_CleanRepo(t *testing.T) {
	dir := testutil.InitTestRepo(t)
	resetDefaultBranchOnce()

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	sc := &server.DaemonCLIConfig{}
	msg := server.PreflightCheck(sc)
	if msg == "" {
		t.Fatal("expected a no-changes message, got empty string")
	}
	if !strings.Contains(msg, "No changed files found.") {
		t.Errorf("missing headline; got:\n%s", msg)
	}
	if !strings.Contains(msg, "crit <file") {
		t.Errorf("missing file-args hint; got:\n%s", msg)
	}
	if !strings.Contains(msg, "review changed files") {
		t.Errorf("missing default-mode explanation; got:\n%s", msg)
	}
	if strings.Contains(msg, "plan") {
		t.Errorf("message should not mention plan mode (internal subcommand); got:\n%s", msg)
	}
	// Must not mention daemons, ports, or networking.
	for _, banned := range []string{"daemon", "port", "connection", "127.0.0.1"} {
		if strings.Contains(strings.ToLower(msg), banned) {
			t.Errorf("message mentions %q; should be free of networking/daemon noise:\n%s", banned, msg)
		}
	}
}

// TestPreflightCheck_WithChanges verifies the preflight is silent
// (returns "") when the repo has changes, so the daemon proceeds normally.
func TestPreflightCheck_WithChanges(t *testing.T) {
	dir := testutil.InitTestRepo(t)
	resetDefaultBranchOnce()

	testutil.Git(t, dir, "checkout", "-b", "feature")
	testutil.WriteFile(t, dir+"/README.md", "# Modified\n")

	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	sc := &server.DaemonCLIConfig{}
	if msg := server.PreflightCheck(sc); msg != "" {
		t.Errorf("expected empty message when repo has changes, got:\n%s", msg)
	}
}

// TestPreflightCheck_NotARepo verifies that preflightCheck returns a
// user-facing error when run outside a VCS repo. Issue #593.
func TestPreflightCheck_NotARepo(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	sc := &server.DaemonCLIConfig{}
	msg := server.PreflightCheck(sc)
	if msg == "" {
		t.Fatal("expected a not-in-repo message, got empty string")
	}
	if !strings.Contains(msg, "Not in a version-controlled repository") {
		t.Errorf("missing headline; got:\n%s", msg)
	}
	if !strings.Contains(msg, "crit <file") {
		t.Errorf("missing file-args hint; got:\n%s", msg)
	}
	for _, banned := range []string{"daemon", "port", "connection", "127.0.0.1"} {
		if strings.Contains(strings.ToLower(msg), banned) {
			t.Errorf("message mentions %q; should be free of networking/daemon noise:\n%s", banned, msg)
		}
	}
}

// TestResolveServeReviewPath_PlanModeColocatesWithReviewFile verifies that
// plan-mode daemons compute a review path under the plan dir, so attachment
// upload and share-payload inlining target the same folder. Pre-fix, plan
// mode fell through to the centralized ~/.crit/reviews/<key> path while
// session.critJSONPath() returned <planDir>/.crit — the split caused pasted
// images to render as [image: <alt>] placeholders on crit-web.
func TestResolveServeReviewPath(t *testing.T) {
	t.Run("outputDir wins as data root", func(t *testing.T) {
		dir := t.TempDir()
		got, err := resolveServeReviewPath(dir, "/some/plan/dir", "deadbeef")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(dir, "reviews", "deadbeef")
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("outputDir warns on legacy identity", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".crit"), 0o755); err != nil {
			t.Fatal(err)
		}
		stderr := captureStderr(t, func() {
			got, err := resolveServeReviewPath(dir, "", "deadbeef1234")
			if err != nil {
				t.Fatalf("resolveServeReviewPath: %v", err)
			}
			want := filepath.Join(dir, "reviews", "deadbeef1234")
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
		if !strings.Contains(stderr, "legacy .crit review") {
			t.Fatalf("stderr = %q, want legacy warning", stderr)
		}
	})

	t.Run("planDir used when outputDir empty", func(t *testing.T) {
		planDir := t.TempDir()
		got, err := resolveServeReviewPath("", planDir, "deadbeef")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(planDir, ".crit")
		if got != want {
			t.Errorf("plan-mode review path: got %q, want %q (must co-locate with review.json so attachments/ inlining can find them)", got, want)
		}
	})

	t.Run("centralized path when neither outputDir nor planDir set", func(t *testing.T) {
		got, err := resolveServeReviewPath("", "", "deadbeef123")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "deadbeef123") {
			t.Errorf("centralized path should embed session key; got %q", got)
		}
	})

	t.Run("centralized path errors are preserved", func(t *testing.T) {
		orig := reviewFilePath
		t.Cleanup(func() { reviewFilePath = orig })
		reviewFilePath = func(string) (string, error) {
			return "", errors.New("home unavailable")
		}

		if _, err := resolveServeReviewPath("", "", "deadbeef123"); err == nil ||
			!strings.Contains(err.Error(), "home unavailable") {
			t.Fatalf("expected centralized path error, got %v", err)
		}
	})

	t.Run("absolute plan path errors are preserved", func(t *testing.T) {
		orig := serveAbsPath
		t.Cleanup(func() { serveAbsPath = orig })
		serveAbsPath = func(string) (string, error) {
			return "", errors.New("working directory unavailable")
		}

		if _, err := resolveServeReviewPath("", "plan", "deadbeef123"); err == nil ||
			!strings.Contains(err.Error(), "working directory unavailable") {
			t.Fatalf("expected plan path error, got %v", err)
		}
	})
}

func TestResolveServeReviewPathPlanBeatsConfiguredOutput(t *testing.T) {
	dir := t.TempDir()
	testutil.SetHome(t, t.TempDir())
	t.Chdir(dir)
	if err := os.WriteFile(
		filepath.Join(dir, ".crit.config.json"),
		[]byte(`{"output":"/tmp/configured"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	planDir := filepath.Join(dir, "plan")
	sc, err := server.ResolveDaemonCLIConfig([]string{"--plan-dir", planDir})
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolveServeReviewPath(sc.OutputDir, sc.PlanDir, "deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(planDir, ".crit")
	if got != want {
		t.Fatalf("review path = %q, want plan review path %q", got, want)
	}
}

func TestServeSessionKey_Override(t *testing.T) {
	sc := &server.DaemonCLIConfig{SessionKeyOverride: "839f3b4cd5d6"}
	if got := serveSessionKey(sc); got != "839f3b4cd5d6" {
		t.Errorf("serveSessionKey() = %q", got)
	}
}

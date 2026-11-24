package migrate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// legacyHome returns a home with a legacy install: a global config, a prompt,
// a hook, custom.css, a review, a plan and stats. XDG variables are cleared.
func legacyHome(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps the legacy layout without XDG variables")
	}
	home := t.TempDir()
	testutil.SetFreshHome(t, home)
	write(t, filepath.Join(home, ".crit.config.json"), `{"author":"me"}`)
	write(t, filepath.Join(home, ".crit", "prompts", "on_finish.md"), "prompt")
	write(t, filepath.Join(home, ".crit", "hooks", "on_finish_approved.sh"), "hook")
	write(t, filepath.Join(home, ".crit", "custom.css"), "css")
	write(t, filepath.Join(home, ".crit", "reviews", "aaaaaaaaaaaa", "review.json"), "{}")
	write(t, filepath.Join(home, ".crit", "plans", "my-plan", ".crit", "review.json"), "{}")
	write(t, filepath.Join(home, ".crit", "stats.json"), "{}")
	return home
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func noRunningReviews(t *testing.T) {
	t.Helper()
	orig := runningReviews
	runningReviews = func(string) []daemon.SessionEntry { return nil }
	t.Cleanup(func() { runningReviews = orig })
}

func TestRun_MovesEverythingAndRemovesLegacy(t *testing.T) {
	home := legacyHome(t)
	noRunningReviews(t)

	var out bytes.Buffer
	if err := run(&out, home, false); err != nil {
		t.Fatalf("run: %v\n%s", err, out.String())
	}

	cfg := filepath.Join(home, ".config", "crit")
	state := filepath.Join(home, ".local", "state", "crit")
	for path, want := range map[string]string{
		filepath.Join(cfg, "config.json"):                                `{"author":"me"}`,
		filepath.Join(cfg, "prompts", "on_finish.md"):                    "prompt",
		filepath.Join(cfg, "hooks", "on_finish_approved.sh"):             "hook",
		filepath.Join(cfg, "custom.css"):                                 "css",
		filepath.Join(state, "reviews", "aaaaaaaaaaaa", "review.json"):   "{}",
		filepath.Join(state, "plans", "my-plan", ".crit", "review.json"): "{}",
		filepath.Join(state, "stats.json"):                               "{}",
	} {
		if got := read(t, path); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	for _, gone := range []string{filepath.Join(home, ".crit"), filepath.Join(home, ".crit.config.json")} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Errorf("%s still exists (err=%v)", gone, err)
		}
	}

	// Running it again is a no-op.
	out.Reset()
	if err := run(&out, home, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Nothing to migrate") {
		t.Errorf("second run said %q", out.String())
	}
}

func TestRun_HonoursCustomXDGFolders(t *testing.T) {
	home := legacyHome(t)
	noRunningReviews(t)
	cfg, state := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_STATE_HOME", state)

	if err := run(&bytes.Buffer{}, home, false); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(cfg, "crit", "config.json")); got != `{"author":"me"}` {
		t.Errorf("config = %q", got)
	}
	if got := read(t, filepath.Join(state, "crit", "stats.json")); got != "{}" {
		t.Errorf("stats = %q", got)
	}
}

func TestRun_DryRunMovesNothing(t *testing.T) {
	home := legacyHome(t)
	noRunningReviews(t)

	var out bytes.Buffer
	if err := run(&out, home, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "~/.crit.config.json → ~/.config/crit/config.json") {
		t.Errorf("dry run output lacks the config move:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".crit", "stats.json")); err != nil {
		t.Errorf("dry run moved stats.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
		t.Errorf("dry run created the config folder (err=%v)", err)
	}
}

// One existing target stops the whole migration before anything moves.
func TestRun_ConflictMovesNothing(t *testing.T) {
	home := legacyHome(t)
	noRunningReviews(t)
	write(t, filepath.Join(home, ".config", "crit", "config.json"), `{"author":"new"}`)

	err := run(&bytes.Buffer{}, home, false)
	if err == nil || !strings.Contains(err.Error(), "~/.config/crit/config.json") {
		t.Fatalf("err = %v, want the conflicting path named", err)
	}
	if got := read(t, filepath.Join(home, ".config", "crit", "config.json")); got != `{"author":"new"}` {
		t.Errorf("existing config overwritten: %q", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".crit", "stats.json")); err != nil {
		t.Errorf("stats.json moved despite the conflict: %v", err)
	}
}

// An empty folder in the way holds nothing, so it is replaced.
func TestRun_EmptyTargetFolderIsReplaced(t *testing.T) {
	home := legacyHome(t)
	noRunningReviews(t)
	if err := os.MkdirAll(filepath.Join(home, ".local", "state", "crit", "reviews"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := run(&bytes.Buffer{}, home, false); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(home, ".local", "state", "crit", "reviews", "aaaaaaaaaaaa", "review.json")); got != "{}" {
		t.Errorf("review = %q", got)
	}
}

func TestRun_RefusesWhileReviewsRun(t *testing.T) {
	home := legacyHome(t)
	orig := runningReviews
	runningReviews = func(string) []daemon.SessionEntry { return []daemon.SessionEntry{{PID: 1}} }
	t.Cleanup(func() { runningReviews = orig })

	err := run(&bytes.Buffer{}, home, false)
	if err == nil || !strings.Contains(err.Error(), "crit stop --all") {
		t.Fatalf("err = %v, want a refusal naming crit stop --all", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".crit.config.json")); err != nil {
		t.Errorf("config moved while a review was running: %v", err)
	}
}

func TestBuildPlan_SkipsLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	t.Run("relative link inside ~/.crit", func(t *testing.T) {
		home := legacyHome(t)
		if err := os.Symlink("prompts", filepath.Join(home, ".crit", "prompts-link")); err != nil {
			t.Fatal(err)
		}
		p, err := BuildPlan(home)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Skipped) != 1 || p.Skipped[0].Path != filepath.Join(home, ".crit", "prompts-link") {
			t.Fatalf("skipped = %+v, want the relative link", p.Skipped)
		}
	})
	t.Run("~/.crit is a link", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip()
		}
		home := t.TempDir()
		testutil.SetFreshHome(t, home)
		dotfiles := filepath.Join(t.TempDir(), "crit")
		write(t, filepath.Join(dotfiles, "stats.json"), "{}")
		if err := os.Symlink(dotfiles, filepath.Join(home, ".crit")); err != nil {
			t.Fatal(err)
		}
		noRunningReviews(t)
		var out bytes.Buffer
		if err := run(&out, home, false); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Nothing was moved") || !strings.Contains(out.String(), "is a link to") {
			t.Errorf("output = %q, want the link left in place", out.String())
		}
		if got := read(t, filepath.Join(dotfiles, "stats.json")); got != "{}" {
			t.Errorf("dotfiles folder changed: %q", got)
		}
	})
}

// Across file systems a rename fails; the copy keeps contents, modes, links
// and modification times.
func TestCopyTree(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	write(t, filepath.Join(src, "reviews", "k", "review.json"), "{}")
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(src, "reviews", "k", "review.json"), old, old); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink("review.json", filepath.Join(src, "reviews", "k", "current")); err != nil {
			t.Fatal(err)
		}
	}
	dst := filepath.Join(t.TempDir(), "dst")
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dst, "reviews", "k", "review.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Errorf("mtime = %v, want %v", info.ModTime(), old)
	}
	if runtime.GOOS != "windows" {
		if link, err := os.Readlink(filepath.Join(dst, "reviews", "k", "current")); err != nil || link != "review.json" {
			t.Errorf("link = %q, %v", link, err)
		}
	}
}

func TestMaybeHint(t *testing.T) {
	home := legacyHome(t)
	stamp := filepath.Join(t.TempDir(), "hint")
	orig := hintStampPath
	hintStampPath = func() (string, error) { return stamp, nil }
	t.Cleanup(func() { hintStampPath = orig })

	now := time.Now()
	var out bytes.Buffer
	MaybeHint(&out, home, now)
	if !strings.Contains(out.String(), "crit migrate-xdg") {
		t.Fatalf("first hint = %q", out.String())
	}

	out.Reset()
	MaybeHint(&out, home, now.Add(24*time.Hour))
	if out.Len() != 0 {
		t.Errorf("hint repeated within a week: %q", out.String())
	}

	MaybeHint(&out, home, now.Add(8*24*time.Hour))
	if !strings.Contains(out.String(), "crit migrate-xdg") {
		t.Errorf("hint missing after a week: %q", out.String())
	}

	// Once migrated there is nothing to hint about.
	noRunningReviews(t)
	if err := run(&bytes.Buffer{}, home, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stamp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("hint stamp left behind (err=%v)", err)
	}
	out.Reset()
	MaybeHint(&out, home, now.Add(30*24*time.Hour))
	if out.Len() != 0 {
		t.Errorf("hint after migrating: %q", out.String())
	}
}

func TestMaybeHint_FreshInstallStaysQuiet(t *testing.T) {
	home := t.TempDir()
	testutil.SetFreshHome(t, home)
	orig := hintStampPath
	hintStampPath = func() (string, error) { return filepath.Join(t.TempDir(), "hint"), nil }
	t.Cleanup(func() { hintStampPath = orig })

	var out bytes.Buffer
	MaybeHint(&out, home, time.Now())
	if out.Len() != 0 {
		t.Errorf("hint on a fresh install: %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".crit")); !os.IsNotExist(err) {
		t.Errorf("hint created ~/.crit (err=%v)", err)
	}
}

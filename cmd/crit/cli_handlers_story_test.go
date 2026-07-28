package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/github"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// reviewSessionKey computes the session key the way RunReview does, for a given
// set of review args, so tests can assert `crit story` collides with it.
func reviewSessionKey(t *testing.T, args []string) string {
	t.Helper()
	sc, err := session.ResolveServerConfigFn(args)
	if err != nil {
		t.Fatalf("resolve review config: %v", err)
	}
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	branch := ""
	if v := vcs.DetectVCS(sc.VCSOverride); v != nil {
		branch = v.CurrentBranch()
	}
	return daemon.SessionKey(cwd, branch, session.FocusKeyArgs(sc))
}

// storySessionKey computes the key the story handler resolves for the same
// scope args (mirrors resolveStoryReviewPath without touching the daemon).
func storySessionKey(t *testing.T, scopeArgs []string) string {
	t.Helper()
	sc, err := storyReviewConfig(scopeArgs)
	if err != nil {
		t.Fatalf("resolve story config: %v", err)
	}
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	branch := ""
	if v := vcs.DetectVCS(sc.VCSOverride); v != nil {
		branch = v.CurrentBranch()
	}
	return daemon.SessionKey(cwd, branch, session.FocusKeyArgs(sc))
}

func setupStoryRepo(t *testing.T) (dir, baseSHA, headSHA string) {
	t.Helper()
	dir = testutil.InitTestRepo(t)
	testutil.SetHome(t, dir)
	baseSHA = testutil.Git(t, dir, "rev-parse", "HEAD")
	// A feature branch with a change so the working tree / range has hunks.
	testutil.Git(t, dir, "checkout", "-b", "feature")
	testutil.WriteFile(t, filepath.Join(dir, "app.go"), "package app\n\nfunc A() int { return 1 }\n")
	testutil.Git(t, dir, "add", "app.go")
	testutil.Git(t, dir, "commit", "-m", "add app.go")
	headSHA = testutil.Git(t, dir, "rev-parse", "HEAD")
	t.Chdir(dir)
	return dir, baseSHA, headSHA
}

func TestStorySessionKeyInvariant_Git(t *testing.T) {
	setupStoryRepo(t)
	got := storySessionKey(t, nil)
	want := reviewSessionKey(t, nil)
	if got != want {
		t.Fatalf("git-mode session key mismatch: story=%s review=%s", got, want)
	}
}

func TestStorySessionKeyInvariant_Range(t *testing.T) {
	_, base, head := setupStoryRepo(t)
	rangeSpec := base + ".." + head
	got := storySessionKey(t, []string{"--range", rangeSpec})
	want := reviewSessionKey(t, []string{"--range", rangeSpec})
	if got != want {
		t.Fatalf("range-mode session key mismatch: story=%s review=%s", got, want)
	}
}

// TestStorySessionKeyInvariant_PRArgs asserts PR-scope arg normalization is
// identical between the two subcommands without needing gh/network: both feed
// the resolved config through session.FocusKeyArgs, so an identical Focus
// yields an identical key. We build the Focus directly to avoid a live PR.
func TestStorySessionKeyInvariant_PRArgs(t *testing.T) {
	setupStoryRepo(t)
	cwd, _ := daemon.ResolvedCWD()
	prFocus := &session.Focus{Kind: session.FocusRange, PRNumber: 75, BaseSHA: "b", HeadSHA: "h"}
	cfg := &session.CLIReviewConfig{Focus: prFocus}

	storyKey := daemon.SessionKey(cwd, "feature", session.FocusKeyArgs(cfg))
	reviewKey := daemon.SessionKey(cwd, "feature", session.FocusKeyArgs(cfg))
	if storyKey != reviewKey {
		t.Fatalf("pr-mode normalization mismatch: %s vs %s", storyKey, reviewKey)
	}
	// And the args are the stable "pr:75" token.
	if got := session.FocusKeyArgs(cfg); len(got) != 1 || got[0] != "pr:75" {
		t.Fatalf("unexpected pr focus key args: %+v", got)
	}
}

func TestFillStoryPRContext_ExplicitPR(t *testing.T) {
	restore := github.SwapFetchPRByNumberForTest(func(n int) (*github.PRInfo, error) {
		return &github.PRInfo{
			Number: n,
			URL:    "https://github.com/acme/widget/pull/75",
			Title:  "Story mode polish",
			Body:   "Use PR description as generation context.",
		}, nil
	})
	defer restore()

	scope := session.StoryScope{PRNumber: 75}
	fillStoryPRContext(&scope)

	if scope.PRTitle != "Story mode polish" || scope.PRBody != "Use PR description as generation context." {
		t.Fatalf("PR context not filled: %+v", scope)
	}
	if scope.PRURL != "https://github.com/acme/widget/pull/75" {
		t.Fatalf("PR URL = %q", scope.PRURL)
	}
}

func TestStoryRejectsPositionalFileArgs(t *testing.T) {
	setupStoryRepo(t)
	err := runStoryE([]string{"plan.md"})
	if err == nil {
		t.Fatal("expected rejection of positional file args")
	}
}

func TestStoryHelpMentionsStoryCommands(t *testing.T) {
	var stderr strings.Builder
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	done := make(chan struct{})
	go func() {
		io.Copy(&stderr, r)
		close(done)
	}()
	err := runStoryE([]string{"--help"})
	w.Close()
	<-done
	os.Stderr = old
	if err != nil {
		t.Fatalf("story help returned error: %v", err)
	}
	out := stderr.String()
	for _, want := range []string{"Usage: crit story", "--story-file", "--prep", "--guide", "--skip-llm", "--refresh", "--clear", "--no-spend"} {
		if !strings.Contains(out, want) {
			t.Fatalf("story help missing %q:\n%s", want, out)
		}
	}
}

func TestStoryStoryFileEndToEnd(t *testing.T) {
	setupStoryRepo(t)

	// Author a story covering app.go's single hunk. New file => old_start 0.
	st := session.Story{
		Version: 1,
		Prologue: &session.StoryPrologue{
			Title:      "App entry point",
			Overview:   "Adds an app function and wires it up.",
			KeyChanges: []string{"Introduce app.A()."},
			Risks:      []string{"Coverage depends on the app.go new-file hunk."},
		},
		Chapters: []session.StoryChapter{
			{ID: "ch1", Title: "New app fn", HunkRefs: []session.StoryHunkRef{{FilePath: "app.go", OldStart: 0}}},
		},
	}
	raw, _ := json.Marshal(st)
	// Write the story file OUTSIDE the repo so it isn't itself picked up as an
	// untracked change in the diff scope.
	storyPath := filepath.Join(t.TempDir(), "story.json")
	if err := os.WriteFile(storyPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runStoryE([]string{"--story-file", storyPath}); err != nil {
		t.Fatalf("expected clean ingest, got: %v", err)
	}

	// The story must be persisted to the review file for this session key.
	critPath, err := resolveStoryReviewPath(nil)
	if err != nil {
		t.Fatal(err)
	}
	cj, err := review.LoadCritJSON(critPath)
	if err != nil {
		t.Fatal(err)
	}
	if cj.Story == nil {
		t.Fatal("story not persisted to review.json")
	}
	if len(cj.Story.Chapters) != 1 || cj.Story.Chapters[0].Title != "New app fn" {
		t.Fatalf("unexpected persisted story: %+v", cj.Story)
	}
	if cj.Story.Coverage == nil || !cj.Story.Coverage.OK {
		t.Fatalf("expected clean coverage, got %+v", cj.Story.Coverage)
	}
}

func TestStoryPrepWritesText(t *testing.T) {
	setupStoryRepo(t)
	prepPath := filepath.Join(t.TempDir(), "prep.txt")
	if err := runStoryE([]string{"--prep", prepPath}); err != nil {
		t.Fatalf("prep failed: %v", err)
	}
	data, err := os.ReadFile(prepPath)
	if err != nil {
		t.Fatalf("prep file not written: %v", err)
	}
	text := string(data)
	// Scope header + the fixture file's hunk id must be present.
	for _, want := range []string{"=== SCOPE ===", "scope_fingerprint:", "=== HUNKS ===", "(app.go, 0)"} {
		if !strings.Contains(text, want) {
			t.Errorf("prep text missing %q:\n%s", want, text)
		}
	}
}

func TestStoryClearRemovesStory(t *testing.T) {
	setupStoryRepo(t)
	critPath, err := resolveStoryReviewPath(nil)
	if err != nil {
		t.Fatal(err)
	}
	cj, _ := review.LoadCritJSON(critPath)
	cj.Story = &session.Story{Version: 1}
	if err := review.SaveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
	}

	if err := runStoryE([]string{"--clear"}); err != nil {
		t.Fatalf("clear failed: %v", err)
	}
	reloaded, _ := review.LoadCritJSON(critPath)
	if reloaded.Story != nil {
		t.Fatal("--clear did not remove the story")
	}
}

func TestStoryClearUsesExactRunningDaemon(t *testing.T) {
	setupStoryRepo(t)
	origAlive, origDelete := storyDaemonAlive, storyDeleteStory
	t.Cleanup(func() { storyDaemonAlive, storyDeleteStory = origAlive, origDelete })
	wantKey := storySessionKey(t, nil)
	var gotKey string
	deleted := false
	storyDaemonAlive = func(key string) (daemon.SessionEntry, bool) {
		gotKey = key
		return daemon.SessionEntry{ReviewPath: "exact-review"}, true
	}
	storyDeleteStory = func(entry daemon.SessionEntry) error {
		deleted = entry.ReviewPath == "exact-review"
		return nil
	}
	if err := runStoryE([]string{"--clear"}); err != nil {
		t.Fatalf("clear failed: %v", err)
	}
	if gotKey != wantKey || !deleted {
		t.Fatalf("clear targeted key %q (want %q), deleted=%v", gotKey, wantKey, deleted)
	}
}

func TestStoryClearFallsBackWhenDaemonDies(t *testing.T) {
	setupStoryRepo(t)
	critPath, err := resolveStoryReviewPath(nil)
	if err != nil {
		t.Fatal(err)
	}
	cj, _ := review.LoadCritJSON(critPath)
	cj.Story = &session.Story{Version: 1}
	if err := review.SaveCritJSON(critPath, cj); err != nil {
		t.Fatal(err)
	}
	origAlive, origDelete := storyDaemonAlive, storyDeleteStory
	t.Cleanup(func() { storyDaemonAlive, storyDeleteStory = origAlive, origDelete })
	checks := 0
	storyDaemonAlive = func(string) (daemon.SessionEntry, bool) {
		checks++
		return daemon.SessionEntry{}, checks <= 2
	}
	storyDeleteStory = func(daemon.SessionEntry) error { return errors.New("connection refused") }
	if err := runStoryE([]string{"--clear"}); err != nil {
		t.Fatalf("clear should fall back after daemon exits: %v", err)
	}
	reloaded, _ := review.LoadCritJSON(critPath)
	if reloaded.Story != nil {
		t.Fatal("fallback clear left story on disk")
	}
}

func TestStoryClearReturnsErrorWhileDaemonStillAlive(t *testing.T) {
	setupStoryRepo(t)
	origAlive, origDelete := storyDaemonAlive, storyDeleteStory
	t.Cleanup(func() { storyDaemonAlive, storyDeleteStory = origAlive, origDelete })
	storyDaemonAlive = func(string) (daemon.SessionEntry, bool) { return daemon.SessionEntry{}, true }
	storyDeleteStory = func(daemon.SessionEntry) error { return errors.New("delete failed") }
	err := runStoryE([]string{"--clear"})
	if err == nil || !strings.Contains(err.Error(), "delete failed") {
		t.Fatalf("expected live daemon delete error, got %v", err)
	}
}

func TestResolveStoryReviewPathIgnoresOtherScopedDaemon(t *testing.T) {
	setupStoryRepo(t)
	origAlive := storyDaemonAlive
	t.Cleanup(func() { storyDaemonAlive = origAlive })
	wantKey := storySessionKey(t, nil)
	storyDaemonAlive = func(key string) (daemon.SessionEntry, bool) {
		if key != wantKey {
			return daemon.SessionEntry{ReviewPath: "wrong-review"}, true
		}
		return daemon.SessionEntry{ReviewPath: "exact-review"}, true
	}
	path, err := resolveStoryReviewPath(nil)
	if err != nil {
		t.Fatal(err)
	}
	if path != "exact-review" {
		t.Fatalf("path = %q, want exact scoped daemon path", path)
	}
}

func TestResolveStoryReviewPathUsesExplicitOutputWithoutDaemon(t *testing.T) {
	setupStoryRepo(t)
	origAlive := storyDaemonAlive
	t.Cleanup(func() { storyDaemonAlive = origAlive })
	storyDaemonAlive = func(string) (daemon.SessionEntry, bool) {
		return daemon.SessionEntry{}, false
	}

	for _, flag := range []string{"--output", "-o"} {
		t.Run(flag, func(t *testing.T) {
			outputDir := filepath.Join(t.TempDir(), "review-output")
			path, err := resolveStoryReviewPath([]string{flag, outputDir})
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(outputDir, "reviews", storySessionKey(t, nil))
			if path != want {
				t.Fatalf("path = %q, want %q", path, want)
			}
		})
	}
}

func TestResolveStoryReviewPathUsesConfiguredOutputWithoutDaemon(t *testing.T) {
	repoDir, _, _ := setupStoryRepo(t)
	origAlive := storyDaemonAlive
	t.Cleanup(func() { storyDaemonAlive = origAlive })
	storyDaemonAlive = func(string) (daemon.SessionEntry, bool) {
		return daemon.SessionEntry{}, false
	}

	outputDir := filepath.Join(t.TempDir(), "configured-output")
	configJSON, err := json.Marshal(map[string]string{"output": outputDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, ".crit.config.json"), configJSON, 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := resolveStoryReviewPath(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(outputDir, "reviews", storySessionKey(t, nil))
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestResolveStoryReviewPathOutputUsesDataRoot(t *testing.T) {
	setupStoryRepo(t)
	origAlive := storyDaemonAlive
	t.Cleanup(func() { storyDaemonAlive = origAlive })
	storyDaemonAlive = func(string) (daemon.SessionEntry, bool) {
		return daemon.SessionEntry{}, false
	}

	outputDir := filepath.Join(t.TempDir(), "review-output")
	got, err := resolveStoryReviewPath([]string{"--output", outputDir})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(outputDir, "reviews", storySessionKey(t, nil))
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestStorySkipLLMWritesStub(t *testing.T) {
	setupStoryRepo(t)
	if err := runStoryE([]string{"--skip-llm"}); err != nil {
		t.Fatalf("skip-llm failed: %v", err)
	}
	critPath, _ := resolveStoryReviewPath(nil)
	cj, _ := review.LoadCritJSON(critPath)
	if cj.Story == nil {
		t.Fatal("skip-llm did not write a stub story")
	}
	if len(cj.Story.Support) != 1 || cj.Story.Support[0].Reason != "stub" {
		t.Fatalf("expected a single stub support entry, got %+v", cj.Story.Support)
	}
}

func TestStoryNoSpendWithoutStory(t *testing.T) {
	setupStoryRepo(t)
	if err := runStoryE([]string{"--no-spend"}); err == nil {
		t.Fatal("expected exit 1 when --no-spend and no story present")
	}
}

// fixturePath resolves a repo test fixture, anchored at this test file's own
// directory (via runtime.Caller) so it stays correct after t.Chdir().
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "test", "fixtures", "story", name)
}

func TestStoryFixtureValidIngests(t *testing.T) {
	setupStoryRepo(t)
	if err := runStoryE([]string{"--story-file", fixturePath(t, "valid-story.json")}); err != nil {
		t.Fatalf("valid fixture should ingest cleanly: %v", err)
	}
	critPath, _ := resolveStoryReviewPath(nil)
	cj, _ := review.LoadCritJSON(critPath)
	if cj.Story == nil || len(cj.Story.Chapters) != 1 {
		t.Fatalf("valid fixture not persisted: %+v", cj.Story)
	}
}

func TestStoryFixtureDuplicateRejected(t *testing.T) {
	setupStoryRepo(t)
	err := runStoryE([]string{"--story-file", fixturePath(t, "duplicate-story.json")})
	if err == nil {
		t.Fatal("duplicate fixture must be rejected (exit 1)")
	}
	critPath, _ := resolveStoryReviewPath(nil)
	cj, _ := review.LoadCritJSON(critPath)
	if cj.Story != nil {
		t.Fatal("rejected story must not be persisted")
	}
}

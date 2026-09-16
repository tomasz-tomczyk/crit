package session

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/picker"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// seedReview writes a review folder under ~/.crit/reviews/<key> as a finished
// session would have left it.
func seedReview(t *testing.T, key string, cj CritJSON) string {
	t.Helper()
	dir, err := daemon.ReviewFilePath(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCritJSON(dir, cj); err != nil {
		t.Fatal(err)
	}
	return dir
}

func stubResumeReview(t *testing.T) *[]string {
	t.Helper()
	var got []string
	orig := runReviewForResume
	runReviewForResume = func(args []string) error {
		got = args
		return nil
	}
	t.Cleanup(func() { runReviewForResume = orig })
	return &got
}

func forceTerminal(t *testing.T, isTTY bool) {
	t.Helper()
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return isTTY }
	t.Cleanup(func() { stdinIsTerminal = orig })
}

func TestListResumableReviews_NewestFirst(t *testing.T) {
	testutil.SetHome(t, t.TempDir())

	seedReview(t, "aaaaaaaaaaaa", CritJSON{Branch: "old", UpdatedAt: "2026-01-01T00:00:00Z"})
	seedReview(t, "bbbbbbbbbbbb", CritJSON{Branch: "newest", UpdatedAt: "2026-03-01T00:00:00Z"})
	seedReview(t, "cccccccccccc", CritJSON{Branch: "middle", UpdatedAt: "2026-02-01T00:00:00Z"})

	reviews, err := listResumableReviews()
	if err != nil {
		t.Fatalf("listResumableReviews: %v", err)
	}
	var branches []string
	for _, review := range reviews {
		branches = append(branches, review.branch)
	}
	want := []string{"newest", "middle", "old"}
	if strings.Join(branches, ",") != strings.Join(want, ",") {
		t.Errorf("branches = %v, want %v", branches, want)
	}
}

func TestListResumableReviews_SkipsUnreadableFolders(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	seedReview(t, "aaaaaaaaaaaa", CritJSON{Branch: "good"})

	reviewsDir, err := daemon.ReviewsDir()
	if err != nil {
		t.Fatal(err)
	}
	// An orphaned snapshots folder, a folder whose name is not a session key,
	// a corrupt review, and a stray file all have to be ignored.
	mustMkdir(t, filepath.Join(reviewsDir, "dddddddddddd"))
	writeFile(t, filepath.Join(reviewsDir, "dddddddddddd", "snapshots.json"), "{}")
	mustMkdir(t, filepath.Join(reviewsDir, "not-a-session-key"))
	mustMkdir(t, filepath.Join(reviewsDir, "eeeeeeeeeeee"))
	writeFile(t, filepath.Join(reviewsDir, "eeeeeeeeeeee", "review.json"), "not json")
	writeFile(t, filepath.Join(reviewsDir, "legacy.json"), "{}")

	reviews, err := listResumableReviews()
	if err != nil {
		t.Fatalf("listResumableReviews: %v", err)
	}
	if len(reviews) != 1 || reviews[0].key != "aaaaaaaaaaaa" {
		t.Fatalf("reviews = %+v, want only the readable review", reviews)
	}
}

func TestListResumableReviews_MissingDirectoryIsNotAnError(t *testing.T) {
	testutil.SetHome(t, t.TempDir())

	reviews, err := listResumableReviews()
	if err != nil {
		t.Fatalf("listResumableReviews: %v", err)
	}
	if len(reviews) != 0 {
		t.Errorf("reviews = %+v, want none", reviews)
	}
}

func TestListResumableReviews_FallsBackToFileMtime(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	seedReview(t, "aaaaaaaaaaaa", CritJSON{Branch: "no-timestamp"})

	reviews, err := listResumableReviews()
	if err != nil {
		t.Fatalf("listResumableReviews: %v", err)
	}
	if len(reviews) != 1 {
		t.Fatalf("reviews = %+v, want one", reviews)
	}
	if reviews[0].updatedAt.IsZero() {
		t.Error("a review without updated_at should fall back to the file mtime")
	}
}

func TestResumableReview_Label(t *testing.T) {
	tests := map[string]struct {
		review resumableReview
		want   string
	}{
		"branch":    {resumableReview{key: "aaaaaaaaaaaa", branch: "feature"}, "feature"},
		"files":     {resumableReview{branch: "feature", cliArgs: []string{"a.md", "b.md"}}, "a.md b.md"},
		"pr focus":  {resumableReview{branch: "feature", cliArgs: []string{"pr:42"}}, "pr:42"},
		"live":      {resumableReview{reviewType: "live", origin: "http://localhost:3000"}, "live: http://localhost:3000"},
		"no branch": {resumableReview{key: "aaaaaaaaaaaa"}, "aaaaaaaaaaaa"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := tc.review.label(); got != tc.want {
				t.Errorf("label() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResumableReview_Detail(t *testing.T) {
	review := resumableReview{
		cwd:        filepath.Join(t.TempDir(), "app"),
		updatedAt:  time.Now().Add(-2 * time.Hour),
		unresolved: 1,
		running:    true,
	}
	detail := review.detail()
	for _, want := range []string{"2h ago", "1 open comment", "running"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail() = %q, want it to mention %q", detail, want)
		}
	}
}

func TestResumableReview_DetailWithoutRecordedCWD(t *testing.T) {
	review := resumableReview{branch: "feature", updatedAt: time.Now()}
	if !strings.Contains(review.detail(), "current directory") {
		t.Errorf("detail() = %q, want it to say where an unrecorded review resumes", review.detail())
	}
	if note := review.unavailable(); note != "" {
		t.Errorf("unavailable() = %q, want a review without a recorded cwd to stay selectable", note)
	}
}

func TestResumableReview_UnavailableWhenDirectoryIsGone(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "deleted")
	review := resumableReview{cwd: gone}
	if note := review.unavailable(); note == "" {
		t.Error("a review whose directory no longer exists must not be selectable")
	}

	present := resumableReview{cwd: t.TempDir()}
	if note := present.unavailable(); note != "" {
		t.Errorf("unavailable() = %q, want an existing directory to be selectable", note)
	}
}

func TestParseResumeArgs(t *testing.T) {
	listOnly, id, passthrough, err := parseResumeArgs([]string{"--list", "839f3b4cd5d6", "--no-open"})
	if err != nil {
		t.Fatalf("parseResumeArgs: %v", err)
	}
	if !listOnly {
		t.Error("--list not recognized")
	}
	if id != "839f3b4cd5d6" {
		t.Errorf("id = %q, want the session key", id)
	}
	if len(passthrough) != 1 || passthrough[0] != "--no-open" {
		t.Errorf("passthrough = %v, want unrecognized flags forwarded", passthrough)
	}
}

func TestParseResumeArgs_RejectsTwoSessionIDs(t *testing.T) {
	if _, _, _, err := parseResumeArgs([]string{"839f3b4cd5d6", "aaaaaaaaaaaa"}); err == nil {
		t.Fatal("expected an error for two session IDs")
	}
}

func TestRunResume_SessionIDSkipsThePicker(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	got := stubResumeReview(t)
	forceTerminal(t, true)

	if err := RunResume([]string{"839f3b4cd5d6", "--no-open"}); err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	want := "--no-open --session 839f3b4cd5d6"
	if strings.Join(*got, " ") != want {
		t.Errorf("review args = %v, want %q", *got, want)
	}
}

func TestRunResume_PickedReviewIsResumed(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	seedReview(t, "aaaaaaaaaaaa", CritJSON{Branch: "older", UpdatedAt: "2026-01-01T00:00:00Z"})
	seedReview(t, "bbbbbbbbbbbb", CritJSON{Branch: "newer", UpdatedAt: "2026-02-01T00:00:00Z"})

	got := stubResumeReview(t)
	forceTerminal(t, true)
	stubSelect(t, func(_ *os.File, _ io.Writer, _ string, items []picker.Item) (int, error) {
		if len(items) != 2 {
			t.Fatalf("picker got %d items, want 2", len(items))
		}
		if items[0].Label != "newer" {
			t.Errorf("first item = %q, want the newest review", items[0].Label)
		}
		return 1, nil
	})

	if err := RunResume(nil); err != nil {
		t.Fatalf("RunResume: %v", err)
	}
	if strings.Join(*got, " ") != "--session aaaaaaaaaaaa" {
		t.Errorf("review args = %v, want the chosen session", *got)
	}
}

func TestRunResume_CancelledPickerDoesNothing(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	seedReview(t, "aaaaaaaaaaaa", CritJSON{Branch: "feature"})

	got := stubResumeReview(t)
	forceTerminal(t, true)
	stubSelect(t, func(*os.File, io.Writer, string, []picker.Item) (int, error) {
		return 0, picker.ErrCancelled
	})

	if err := RunResume(nil); err != nil {
		t.Fatalf("RunResume after cancel = %v, want nil", err)
	}
	if *got != nil {
		t.Errorf("review args = %v, want no review started", *got)
	}
}

func TestRunResume_WithoutTerminalPrintsListAndFails(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	seedReview(t, "aaaaaaaaaaaa", CritJSON{Branch: "feature"})
	forceTerminal(t, false)

	var err error
	stderr := captureStderr(t, func() { err = RunResume(nil) })
	if err == nil {
		t.Fatal("expected an error when there is no terminal for the picker")
	}
	if !strings.Contains(stderr, "aaaaaaaaaaaa") || !strings.Contains(stderr, "feature") {
		t.Errorf("stderr = %q, want the review list", stderr)
	}
}

func TestRunResume_NoReviews(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	forceTerminal(t, true)
	stubSelect(t, func(*os.File, io.Writer, string, []picker.Item) (int, error) {
		t.Fatal("picker must not open when there is nothing to resume")
		return 0, nil
	})

	if err := RunResume(nil); err != nil {
		t.Fatalf("RunResume: %v", err)
	}
}

func TestPrintResumableReviews(t *testing.T) {
	var out bytes.Buffer
	printResumableReviews(&out, []resumableReview{
		{key: "aaaaaaaaaaaa", branch: "feature", cwd: t.TempDir(), unresolved: 2, updatedAt: time.Now()},
		{key: "bbbbbbbbbbbb", branch: "gone", cwd: filepath.Join(t.TempDir(), "deleted")},
	})

	text := out.String()
	for _, want := range []string{"aaaaaaaaaaaa", "feature", "2 open comments", "directory is gone", "crit resume <id>"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}

func TestDisplayPath(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	if got := displayPath(filepath.Join(home, "code", "app")); got != filepath.Join("~", "code", "app") {
		t.Errorf("displayPath() = %q, want a ~-relative path", got)
	}
	if got := displayPath(""); got != "current directory" {
		t.Errorf("displayPath(\"\") = %q", got)
	}
	outside := filepath.Join(t.TempDir(), "elsewhere")
	if got := displayPath(outside); got != outside {
		t.Errorf("displayPath() = %q, want the path unchanged outside home", got)
	}
}

func TestHumanizeAge(t *testing.T) {
	tests := map[time.Duration]string{
		30 * time.Second:     "just now",
		5 * time.Minute:      "5m ago",
		3 * time.Hour:        "3h ago",
		50 * time.Hour:       "2d ago",
		400 * 24 * time.Hour: "400d ago",
	}
	for d, want := range tests {
		if got := humanizeAge(d); got != want {
			t.Errorf("humanizeAge(%s) = %q, want %q", d, got, want)
		}
	}
}

// The recorded cwd is the only way resume can find a review's working tree, so
// the write path has to set it and must never blank it out.
func TestBuildCritJSON_RecordsCWD(t *testing.T) {
	dir := t.TempDir()
	cj := buildCritJSON(writeFilesSnapshot{critPath: filepath.Join(dir, "review"), cwd: "/work/app"})
	if cj.CWD != "/work/app" {
		t.Errorf("CWD = %q, want the daemon's directory", cj.CWD)
	}
}

func TestBuildCritJSON_EmptyCWDKeepsRecordedDirectory(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	critPath := seedReview(t, "aaaaaaaaaaaa", CritJSON{CWD: "/work/app", Branch: "feature"})

	cj := buildCritJSON(writeFilesSnapshot{critPath: critPath, branch: "feature"})
	if cj.CWD != "/work/app" {
		t.Errorf("CWD = %q, want the directory already on disk to survive a snapshot without one", cj.CWD)
	}
}

func stubSelect(t *testing.T, fn func(*os.File, io.Writer, string, []picker.Item) (int, error)) {
	t.Helper()
	orig := selectResumeTarget
	selectResumeTarget = fn
	t.Cleanup(func() { selectResumeTarget = orig })
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// Guard against the resume list quietly swallowing a real read failure.
func TestListResumableReviews_SurfacesReadError(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	reviewsDir, err := daemon.ReviewsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(reviewsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	// A file where the reviews directory should be makes ReadDir fail with
	// something other than "not exist".
	writeFile(t, reviewsDir, "")

	if _, err := listResumableReviews(); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want a surfaced read failure", err)
	}
}

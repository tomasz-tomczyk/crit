package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/daemon"
	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

// writeReviewFixture writes a CritJSON with the given branch into
// ~/.crit/reviews/<name>.json under HOME and returns the full path.
func writeReviewFixture(t *testing.T, name, branch string) string {
	t.Helper()
	dir, err := daemon.ReviewsDir()
	if err != nil {
		t.Fatalf("daemon.ReviewsDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, name+".json")
	cj := CritJSON{Branch: branch, Files: map[string]CritJSONFile{}}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// writeFolderReviewFixture writes a folder-form review at <daemon.ReviewsDir>/<name>
// with the given branch. Returns the folder identity path.
func writeFolderReviewFixture(t *testing.T, name, branch string) string {
	t.Helper()
	dir, err := daemon.ReviewsDir()
	if err != nil {
		t.Fatalf("daemon.ReviewsDir: %v", err)
	}
	folder := filepath.Join(dir, name)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cj := CritJSON{Branch: branch, Files: map[string]CritJSONFile{}}
	data, err := json.Marshal(cj)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(folder, "review.json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return folder
}

func TestFindReviewFileByBranch_FolderForm(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	want := writeFolderReviewFixture(t, "k1", "feature-x")
	writeFolderReviewFixture(t, "k2", "other")

	got, err := findReviewFileByBranch("feature-x", "")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFindReviewFileByBranch_OrphanFolderSkipped(t *testing.T) {
	// Folder with no review.json (snapshots-only orphan) must be ignored.
	testutil.SetHome(t, t.TempDir())
	dir, _ := daemon.ReviewsDir()
	if err := os.MkdirAll(filepath.Join(dir, "orphan"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "orphan", "snapshots.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := writeFolderReviewFixture(t, "k1", "feature-x")

	got, err := findReviewFileByBranch("feature-x", "")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q (orphan should be skipped)", got, want)
	}
}

// MIGRATION-REMOVAL: legacy flat-file review files must still be discoverable
// until the migration shim is deleted.
func TestFindReviewFileByBranch_MigrationFallback(t *testing.T) {
	testutil.SetHome(t, t.TempDir())
	want := writeReviewFixture(t, "k1", "feature-x")

	got, err := findReviewFileByBranch("feature-x", "")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFindReviewFileByBranch(t *testing.T) {
	t.Run("single match returns path", func(t *testing.T) {
		testutil.SetHome(t, t.TempDir())
		want := writeReviewFixture(t, "k1", "feature-x")
		writeReviewFixture(t, "k2", "other-branch")

		got, err := findReviewFileByBranch("feature-x", "")
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("no match returns sentinel", func(t *testing.T) {
		testutil.SetHome(t, t.TempDir())
		writeReviewFixture(t, "k1", "other-branch")

		_, err := findReviewFileByBranch("feature-x", "")
		if !errors.Is(err, errReviewFileNotFoundForBranch) {
			t.Errorf("err = %v, want errReviewFileNotFoundForBranch", err)
		}
	})

	t.Run("multiple matches return ambiguous sentinel", func(t *testing.T) {
		testutil.SetHome(t, t.TempDir())
		writeReviewFixture(t, "k1", "feature-x")
		writeReviewFixture(t, "k2", "feature-x")

		_, err := findReviewFileByBranch("feature-x", "")
		if !errors.Is(err, errReviewFileAmbiguousForBranch) {
			t.Errorf("err = %v, want errReviewFileAmbiguousForBranch", err)
		}
	})

	t.Run("excludePath is skipped", func(t *testing.T) {
		testutil.SetHome(t, t.TempDir())
		exclude := writeReviewFixture(t, "k1", "feature-x")
		want := writeReviewFixture(t, "k2", "feature-x")

		// With exclude, k2 is the only remaining match.
		got, err := findReviewFileByBranch("feature-x", exclude)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("reviews dir missing returns not-found sentinel", func(t *testing.T) {
		testutil.SetHome(t, t.TempDir())
		_, err := findReviewFileByBranch("feature-x", "")
		if !errors.Is(err, errReviewFileNotFoundForBranch) {
			t.Errorf("err = %v, want errReviewFileNotFoundForBranch", err)
		}
	})

	t.Run("empty branch errors", func(t *testing.T) {
		testutil.SetHome(t, t.TempDir())
		_, err := findReviewFileByBranch("", "")
		if err == nil {
			t.Error("err = nil, want non-nil for empty branch")
		}
	})
}

// captureStderr runs fn while capturing os.Stderr writes; returns captured bytes.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = prev }()

	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()
	_ = w.Close()
	<-done
	return buf.String()
}

func TestResolveReviewPathWithArgs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	t.Run("no args delegates to ResolveReviewPath", func(t *testing.T) {
		withArgs, err := ResolveReviewPathWithArgs("", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		without, err := ResolveReviewPath("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if withArgs != without {
			t.Errorf("expected same path, got %q vs %q", withArgs, without)
		}
	})

	t.Run("file args produce different path than no args", func(t *testing.T) {
		noArgs, err := ResolveReviewPathWithArgs("", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		withArgs, err := ResolveReviewPathWithArgs("", []string{"file.md"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if noArgs == withArgs {
			t.Error("expected different paths for no-args vs file-args")
		}
	})

	t.Run("outputDir is data root with keyed review", func(t *testing.T) {
		dir := filepath.Join(tmp, "out")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path, err := ResolveReviewPathWithArgs(dir, []string{"file.md"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		cwd, err := daemon.ResolvedCWD()
		if err != nil {
			t.Fatal(err)
		}
		key := daemon.SessionKey(cwd, "", []string{"file.md"})
		want := filepath.Join(dir, "reviews", key)
		if path != want {
			t.Errorf("got %q, want %q", path, want)
		}
	})
}

func TestResolveReviewPathOutputHomeCritMatchesDefault(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	cwd := t.TempDir()
	t.Chdir(cwd)
	resolvedCWD, err := daemon.ResolvedCWD()
	if err != nil {
		t.Fatal(err)
	}
	key := daemon.SessionKey(resolvedCWD, "", nil)
	defaultPath, err := daemon.ReviewFilePath(key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ResolveReviewPath(filepath.Join(home, ".crit"))
	if err != nil {
		t.Fatal(err)
	}
	if got != defaultPath {
		t.Fatalf("got %q, want default path %q", got, defaultPath)
	}
}

func TestResolveCommandReviewPathExplicitRelativeOutputUsesCurrentDirectory(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	got, err := ResolveCommandReviewPath("out", filepath.Join(root, "configured"))
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := daemon.ResolvedCWD()
	if err != nil {
		t.Fatal(err)
	}
	key := daemon.SessionKey(cwd, "", nil)
	want := filepath.Join(nested, "out", "reviews", key)
	if got != want {
		t.Fatalf("review path = %q, want explicit CWD-relative path %q", got, want)
	}
}

func TestResolveCommandReviewPathPrecedence(t *testing.T) {
	cwd := t.TempDir()
	testutil.SetHome(t, t.TempDir())
	t.Chdir(cwd)
	resolvedCWD, err := daemon.ResolvedCWD()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("configured output before centralized", func(t *testing.T) {
		configuredOutput := t.TempDir()
		got, err := ResolveCommandReviewPath("", configuredOutput)
		if err != nil {
			t.Fatal(err)
		}
		key := daemon.SessionKey(resolvedCWD, "", nil)
		want := filepath.Join(configuredOutput, "reviews", key)
		if got != want {
			t.Fatalf("review path = %q, want configured path %q", got, want)
		}
	})

	t.Run("file args retained for centralized fallback", func(t *testing.T) {
		args := []string{"plan.md"}
		got, err := ResolveCommandReviewPathWithArgs("", "", args)
		if err != nil {
			t.Fatal(err)
		}
		want, err := ResolveReviewPathWithArgs("", args)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("review path = %q, want centralized path %q", got, want)
		}
	})

	t.Run("active daemon before configured output", func(t *testing.T) {
		health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"status":"ok"}`)
		}))
		t.Cleanup(health.Close)
		parsed, err := url.Parse(health.URL)
		if err != nil {
			t.Fatal(err)
		}
		port, err := strconv.Atoi(parsed.Port())
		if err != nil {
			t.Fatal(err)
		}
		daemonPath := filepath.Join(t.TempDir(), ".crit")
		const key = "review-command-path"
		if err := daemon.WriteSessionFile(key, daemon.SessionEntry{
			PID:        os.Getpid(),
			Port:       port,
			CWD:        resolvedCWD,
			ReviewPath: daemonPath,
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { daemon.RemoveSessionFile(key) })

		got, err := ResolveCommandReviewPath("", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if got != daemonPath {
			t.Fatalf("review path = %q, want daemon path %q", got, daemonPath)
		}
	})
}

func TestClearReviewPath(t *testing.T) {
	reviewPath := filepath.Join(t.TempDir(), ".crit")
	if err := os.MkdirAll(reviewPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reviewPath, "review.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ClearReviewPath(reviewPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reviewPath); !os.IsNotExist(err) {
		t.Fatalf("review path still exists or stat failed unexpectedly: %v", err)
	}
}

func withFetchPRHeadInfo(t *testing.T, fn func(int) (*PRHeadInfo, error)) {
	t.Helper()
	prev := FetchPRHeadInfoFn
	FetchPRHeadInfoFn = fn
	t.Cleanup(func() { FetchPRHeadInfoFn = prev })
}

func TestRedirectReviewPathForPR(t *testing.T) {
	type stub func(int) (*PRHeadInfo, error)

	tests := []struct {
		name        string
		fetch       stub
		fixtures    map[string]string // name -> branch
		cwdBranch   string
		wantOK      bool
		wantBaseOf  string // basename of returned path (when ok)
		wantStderr  string // substring (empty = no requirement)
		notInStderr string // must NOT contain
	}{
		{
			name:      "cwd matches PR HeadRefName -> no redirect",
			fetch:     func(int) (*PRHeadInfo, error) { return &PRHeadInfo{HeadRefName: "feature-x"}, nil },
			fixtures:  map[string]string{"k1": "feature-x"},
			cwdBranch: "feature-x",
			wantOK:    false,
		},
		{
			name:       "cwd differs, unique alt -> redirect",
			fetch:      func(int) (*PRHeadInfo, error) { return &PRHeadInfo{HeadRefName: "feature-x"}, nil },
			fixtures:   map[string]string{"k1": "feature-x"},
			cwdBranch:  "other",
			wantOK:     true,
			wantBaseOf: "k1.json",
		},
		{
			name:      "cwd differs, no alt -> no redirect",
			fetch:     func(int) (*PRHeadInfo, error) { return &PRHeadInfo{HeadRefName: "feature-x"}, nil },
			fixtures:  map[string]string{"k1": "other-branch"},
			cwdBranch: "other",
			wantOK:    false,
		},
		{
			name:       "cwd differs, multiple alt files -> Note + no redirect",
			fetch:      func(int) (*PRHeadInfo, error) { return &PRHeadInfo{HeadRefName: "feature-x"}, nil },
			fixtures:   map[string]string{"k1": "feature-x", "k2": "feature-x"},
			cwdBranch:  "other",
			wantOK:     false,
			wantStderr: "multiple review files match",
		},
		{
			name:      "fetch error -> no panic, no redirect",
			fetch:     func(int) (*PRHeadInfo, error) { return nil, errors.New("offline") },
			fixtures:  map[string]string{"k1": "feature-x"},
			cwdBranch: "other",
			wantOK:    false,
		},
		{
			name:      "empty HeadRefName -> no redirect",
			fetch:     func(int) (*PRHeadInfo, error) { return &PRHeadInfo{HeadRefName: ""}, nil },
			fixtures:  map[string]string{"k1": "feature-x"},
			cwdBranch: "other",
			wantOK:    false,
		},
		{
			name:       "empty cwdBranch + unique alt -> redirect (Fix 1)",
			fetch:      func(int) (*PRHeadInfo, error) { return &PRHeadInfo{HeadRefName: "feature-x"}, nil },
			fixtures:   map[string]string{"k1": "feature-x"},
			cwdBranch:  "",
			wantOK:     true,
			wantBaseOf: "k1.json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetHome(t, t.TempDir())
			for name, branch := range tc.fixtures {
				writeReviewFixture(t, name, branch)
			}
			withFetchPRHeadInfo(t, tc.fetch)

			var gotPath string
			var gotOK bool
			stderr := captureStderr(t, func() {
				gotPath, _, gotOK = RedirectReviewPathForPR(123, tc.cwdBranch, "")
			})

			if gotOK != tc.wantOK {
				t.Errorf("ok = %v, want %v (stderr=%q)", gotOK, tc.wantOK, stderr)
			}
			if tc.wantOK && filepath.Base(gotPath) != tc.wantBaseOf {
				t.Errorf("path basename = %q, want %q", filepath.Base(gotPath), tc.wantBaseOf)
			}
			if tc.wantStderr != "" && !strings.Contains(stderr, tc.wantStderr) {
				t.Errorf("stderr = %q, want substring %q", stderr, tc.wantStderr)
			}
		})
	}
}

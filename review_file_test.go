package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeReviewFixture writes a CritJSON with the given branch into
// ~/.crit/reviews/<name>.json under HOME and returns the full path.
func writeReviewFixture(t *testing.T, name, branch string) string {
	t.Helper()
	dir, err := reviewsDir()
	if err != nil {
		t.Fatalf("reviewsDir: %v", err)
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

// writeFolderReviewFixture writes a folder-form review at <reviewsDir>/<name>
// with the given branch. Returns the folder identity path.
func writeFolderReviewFixture(t *testing.T, name, branch string) string {
	t.Helper()
	dir, err := reviewsDir()
	if err != nil {
		t.Fatalf("reviewsDir: %v", err)
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
	setHome(t, t.TempDir())
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
	setHome(t, t.TempDir())
	dir, _ := reviewsDir()
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
	setHome(t, t.TempDir())
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
		setHome(t, t.TempDir())
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
		setHome(t, t.TempDir())
		writeReviewFixture(t, "k1", "other-branch")

		_, err := findReviewFileByBranch("feature-x", "")
		if !errors.Is(err, errReviewFileNotFoundForBranch) {
			t.Errorf("err = %v, want errReviewFileNotFoundForBranch", err)
		}
	})

	t.Run("multiple matches return ambiguous sentinel", func(t *testing.T) {
		setHome(t, t.TempDir())
		writeReviewFixture(t, "k1", "feature-x")
		writeReviewFixture(t, "k2", "feature-x")

		_, err := findReviewFileByBranch("feature-x", "")
		if !errors.Is(err, errReviewFileAmbiguousForBranch) {
			t.Errorf("err = %v, want errReviewFileAmbiguousForBranch", err)
		}
	})

	t.Run("excludePath is skipped", func(t *testing.T) {
		setHome(t, t.TempDir())
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
		setHome(t, t.TempDir())
		_, err := findReviewFileByBranch("feature-x", "")
		if !errors.Is(err, errReviewFileNotFoundForBranch) {
			t.Errorf("err = %v, want errReviewFileNotFoundForBranch", err)
		}
	})

	t.Run("empty branch errors", func(t *testing.T) {
		setHome(t, t.TempDir())
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

func TestRedirectReviewPathForPR(t *testing.T) {
	type stub func(int) (*PRInfo, error)

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
			fetch:     func(int) (*PRInfo, error) { return &PRInfo{HeadRefName: "feature-x"}, nil },
			fixtures:  map[string]string{"k1": "feature-x"},
			cwdBranch: "feature-x",
			wantOK:    false,
		},
		{
			name:       "cwd differs, unique alt -> redirect",
			fetch:      func(int) (*PRInfo, error) { return &PRInfo{HeadRefName: "feature-x"}, nil },
			fixtures:   map[string]string{"k1": "feature-x"},
			cwdBranch:  "other",
			wantOK:     true,
			wantBaseOf: "k1.json",
		},
		{
			name:      "cwd differs, no alt -> no redirect",
			fetch:     func(int) (*PRInfo, error) { return &PRInfo{HeadRefName: "feature-x"}, nil },
			fixtures:  map[string]string{"k1": "other-branch"},
			cwdBranch: "other",
			wantOK:    false,
		},
		{
			name:       "cwd differs, multiple alt files -> Note + no redirect",
			fetch:      func(int) (*PRInfo, error) { return &PRInfo{HeadRefName: "feature-x"}, nil },
			fixtures:   map[string]string{"k1": "feature-x", "k2": "feature-x"},
			cwdBranch:  "other",
			wantOK:     false,
			wantStderr: "multiple review files match",
		},
		{
			name:      "fetch error -> no panic, no redirect",
			fetch:     func(int) (*PRInfo, error) { return nil, errors.New("offline") },
			fixtures:  map[string]string{"k1": "feature-x"},
			cwdBranch: "other",
			wantOK:    false,
		},
		{
			name:      "empty HeadRefName -> no redirect",
			fetch:     func(int) (*PRInfo, error) { return &PRInfo{HeadRefName: ""}, nil },
			fixtures:  map[string]string{"k1": "feature-x"},
			cwdBranch: "other",
			wantOK:    false,
		},
		{
			name:       "empty cwdBranch + unique alt -> redirect (Fix 1)",
			fetch:      func(int) (*PRInfo, error) { return &PRInfo{HeadRefName: "feature-x"}, nil },
			fixtures:   map[string]string{"k1": "feature-x"},
			cwdBranch:  "",
			wantOK:     true,
			wantBaseOf: "k1.json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setHome(t, t.TempDir())
			for name, branch := range tc.fixtures {
				writeReviewFixture(t, name, branch)
			}
			withFetchPRByNumber(t, tc.fetch)

			var gotPath string
			var gotOK bool
			stderr := captureStderr(t, func() {
				gotPath, _, gotOK = redirectReviewPathForPR(123, tc.cwdBranch, "")
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

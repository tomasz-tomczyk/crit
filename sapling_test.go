package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Compile-time interface compliance check.
var _ VCS = &SaplingVCS{}

func TestSaplingVCS_Name(t *testing.T) {
	s := &SaplingVCS{}
	if got := s.Name(); got != "sl" {
		t.Errorf("Name() = %q, want %q", got, "sl")
	}
}

func TestSaplingVCS_HasStagingArea(t *testing.T) {
	s := &SaplingVCS{}
	if s.HasStagingArea() {
		t.Error("HasStagingArea() = true, want false")
	}
}

func TestSaplingVCS_SkipDirNames(t *testing.T) {
	s := &SaplingVCS{}
	dirs := s.SkipDirNames()
	want := map[string]bool{".sl": true, ".git": true}
	if len(dirs) != len(want) {
		t.Fatalf("SkipDirNames() = %v, want keys of %v", dirs, want)
	}
	for _, d := range dirs {
		if !want[d] {
			t.Errorf("unexpected dir name %q in SkipDirNames()", d)
		}
	}
}

func TestSaplingVCS_ChangedFilesScoped_Staged(t *testing.T) {
	s := &SaplingVCS{}
	got, err := s.ChangedFilesScoped("staged", "")
	if err != nil {
		t.Fatalf("ChangedFilesScoped(staged) error: %v", err)
	}
	if got != nil {
		t.Errorf("ChangedFilesScoped(staged) = %v, want nil", got)
	}
}

func TestSaplingVCS_ChangedFilesScoped_Unstaged(t *testing.T) {
	s := &SaplingVCS{}
	got, err := s.ChangedFilesScoped("unstaged", "")
	if err != nil {
		t.Fatalf("ChangedFilesScoped(unstaged) error: %v", err)
	}
	if got != nil {
		t.Errorf("ChangedFilesScoped(unstaged) = %v, want nil", got)
	}
}

func TestSaplingVCS_DefaultBranchOverride(t *testing.T) {
	s := &SaplingVCS{}
	s.SetDefaultBranchOverride("develop")
	if got := s.GetDefaultBranchOverride(); got != "develop" {
		t.Errorf("GetDefaultBranchOverride() = %q, want %q", got, "develop")
	}
	if got := s.DefaultBranch(); got != "develop" {
		t.Errorf("DefaultBranch() = %q after override, want %q", got, "develop")
	}
}

func TestParseSaplingCommitLog(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []CommitInfo
	}{
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
		{
			name: "single commit",
			input: "abc123def456abc123def456abc123def456abcdef\n" +
				"abc123d\n" +
				"Fix the widget\n" +
				"alice\n" +
				"2024-03-15 10:30 +0000\n" +
				"---\n",
			want: []CommitInfo{
				{
					SHA:      "abc123def456abc123def456abc123def456abcdef",
					ShortSHA: "abc123d",
					Message:  "Fix the widget",
					Author:   "alice",
					Date:     "2024-03-15 10:30 +0000",
				},
			},
		},
		{
			name: "multiple commits",
			input: "aaaa\naa\nFirst commit\nalice\n2024-01-01 00:00 +0000\n---\n" +
				"bbbb\nbb\nSecond commit\nbob\n2024-01-02 00:00 +0000\n---\n",
			want: []CommitInfo{
				{SHA: "aaaa", ShortSHA: "aa", Message: "First commit", Author: "alice", Date: "2024-01-01 00:00 +0000"},
				{SHA: "bbbb", ShortSHA: "bb", Message: "Second commit", Author: "bob", Date: "2024-01-02 00:00 +0000"},
			},
		},
		{
			name:  "incomplete block is skipped",
			input: "abc\nshort\n---\n",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSaplingCommitLog(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d commits, want %d\ngot:  %+v\nwant: %+v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("commit[%d]: got %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseRemoteBookmarks(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
		{
			name:  "single bookmark",
			input: "main   abc123def456",
			want:  []string{"main"},
		},
		{
			name:  "multiple bookmarks",
			input: "main   abc123\nrelease   def456\ndev   789abc",
			want:  []string{"main", "release", "dev"},
		},
		{
			name:  "empty lines",
			input: "main   abc123\n\n\nrelease   def456\n",
			want:  []string{"main", "release"},
		},
		{
			name:  "whitespace only lines",
			input: "main   abc123\n   \nrelease   def456",
			want:  []string{"main", "release"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRemoteBookmarks(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d bookmarks, want %d\ngot:  %v\nwant: %v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("bookmark[%d]: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDetectVCS_SaplingOverride(t *testing.T) {
	for _, override := range []string{"sl", "sapling"} {
		v := DetectVCS(override)
		if v == nil {
			// Falls back to git if sl not in PATH — that's fine
			t.Skipf("DetectVCS(%q) returned nil (sl likely not in PATH)", override)
		}
		// If sl is in PATH, should return SaplingVCS; otherwise GitVCS fallback
		if _, hasSL := exec.LookPath("sl"); hasSL == nil {
			if _, ok := v.(*SaplingVCS); !ok {
				t.Errorf("DetectVCS(%q) returned %T, want *SaplingVCS", override, v)
			}
		}
	}
}

func TestHasSLDirFrom_DetectsDotSL(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "nested", "repo")
	if err := os.MkdirAll(filepath.Join(root, ".sl"), 0o755); err != nil {
		t.Fatalf("mkdir .sl: %v", err)
	}
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	if !hasSLDirFrom(child) {
		t.Fatal("expected hasSLDirFrom to detect .sl metadata")
	}
}

func TestHasSLDirFrom_DoesNotDetectDotGitSL(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "nested", "repo")
	if err := os.MkdirAll(filepath.Join(root, ".git", "sl"), 0o755); err != nil {
		t.Fatalf("mkdir .git/sl: %v", err)
	}
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	// .git/sl should NOT trigger hasSLDirFrom — it's handled separately
	// as a hint, not as auto-detection
	if hasSLDirFrom(child) {
		t.Fatal("hasSLDirFrom should not detect .git/sl as Sapling metadata")
	}
}

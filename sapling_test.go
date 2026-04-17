package main

import "testing"

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

func TestDetectVCS_SaplingOverride(t *testing.T) {
	for _, override := range []string{"sl", "sapling"} {
		v := DetectVCS(override)
		if v == nil {
			t.Fatalf("DetectVCS(%q) = nil, want *SaplingVCS", override)
		}
		if _, ok := v.(*SaplingVCS); !ok {
			t.Errorf("DetectVCS(%q) returned %T, want *SaplingVCS", override, v)
		}
	}
}

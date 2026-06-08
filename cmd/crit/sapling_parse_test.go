package main

import (
	"testing"
)

func TestParseSaplingStatus(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []FileChange
	}{
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
		{
			name:  "single modified file",
			input: "M foo.go",
			want:  []FileChange{{Path: "foo.go", Status: "modified"}},
		},
		{
			name:  "single added file",
			input: "A new_file.go",
			want:  []FileChange{{Path: "new_file.go", Status: "added"}},
		},
		{
			name:  "single removed file",
			input: "R old_file.go",
			want:  []FileChange{{Path: "old_file.go", Status: "deleted"}},
		},
		{
			name:  "single untracked file",
			input: "? scratch.txt",
			want:  []FileChange{{Path: "scratch.txt", Status: "untracked"}},
		},
		{
			name:  "missing file treated as deleted",
			input: "! gone.go",
			want:  []FileChange{{Path: "gone.go", Status: "deleted"}},
		},
		{
			name:  "ignored file skipped",
			input: "I build/output.o",
			want:  nil,
		},
		{
			name:  "clean file skipped",
			input: "C lib/util.go",
			want:  nil,
		},
		{
			name:  "multiple files mixed statuses",
			input: "M src/main.go\nA src/new.go\nR src/old.go\n? notes.txt\n! vanished.go",
			want: []FileChange{
				{Path: "src/main.go", Status: "modified"},
				{Path: "src/new.go", Status: "added"},
				{Path: "src/old.go", Status: "deleted"},
				{Path: "notes.txt", Status: "untracked"},
				{Path: "vanished.go", Status: "deleted"},
			},
		},
		{
			name:  "ignored and clean files filtered from mixed input",
			input: "M keep.go\nI skip.o\nC clean.go\nA also_keep.go",
			want: []FileChange{
				{Path: "keep.go", Status: "modified"},
				{Path: "also_keep.go", Status: "added"},
			},
		},
		{
			name:  "path with spaces",
			input: "M path with spaces/file name.go",
			want:  []FileChange{{Path: "path with spaces/file name.go", Status: "modified"}},
		},
		{
			name:  "windows line endings",
			input: "M foo.go\r\nA bar.go\r\n",
			want: []FileChange{
				{Path: "foo.go", Status: "modified"},
				{Path: "bar.go", Status: "added"},
			},
		},
		{
			name:  "whitespace only input",
			input: "   \n  \n",
			want:  nil,
		},
		{
			name:  "malformed line too short",
			input: "M",
			want:  nil,
		},
		{
			name:  "malformed line no space separator",
			input: "MXfoo.go",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSaplingStatus(tt.input)
			assertFileChangesEqual(t, got, tt.want)
		})
	}
}

func TestParseSaplingDiffStat(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]NumstatEntry
	}{
		{
			name:  "empty input",
			input: "",
			want:  map[string]NumstatEntry{},
		},
		{
			name:  "typical mixed output",
			input: " src/main.go | 10 ++++------\n src/util.go | 3 +++\n 2 files changed, 7 insertions(+), 6 deletions(-)",
			want: map[string]NumstatEntry{
				"src/main.go": {Additions: 4, Deletions: 6},
				"src/util.go": {Additions: 3, Deletions: 0},
			},
		},
		{
			name:  "file with only additions",
			input: " new.go | 5 +++++\n 1 file changed, 5 insertions(+)",
			want: map[string]NumstatEntry{
				"new.go": {Additions: 5, Deletions: 0},
			},
		},
		{
			name:  "file with only deletions",
			input: " old.go | 3 ---\n 1 file changed, 3 deletions(-)",
			want: map[string]NumstatEntry{
				"old.go": {Additions: 0, Deletions: 3},
			},
		},
		{
			name:  "summary line is skipped",
			input: " 5 files changed, 20 insertions(+), 10 deletions(-)",
			want:  map[string]NumstatEntry{},
		},
		{
			name:  "path with spaces",
			input: " path with spaces/file.go | 2 +-\n 1 file changed, 1 insertion(+), 1 deletion(-)",
			want: map[string]NumstatEntry{
				"path with spaces/file.go": {Additions: 1, Deletions: 1},
			},
		},
		{
			name:  "windows line endings",
			input: " a.go | 3 +++\r\n b.go | 2 --\r\n 2 files changed, 3 insertions(+), 2 deletions(-)\r\n",
			want: map[string]NumstatEntry{
				"a.go": {Additions: 3, Deletions: 0},
				"b.go": {Additions: 0, Deletions: 2},
			},
		},
		{
			name:  "no summary line",
			input: " config.yaml | 4 ++--",
			want: map[string]NumstatEntry{
				"config.yaml": {Additions: 2, Deletions: 2},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSaplingDiffStat(tt.input)
			assertNumstatEqual(t, got, tt.want)
		})
	}
}

func assertFileChangesEqual(t *testing.T, got, want []FileChange) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d changes, want %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("change[%d]: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func assertNumstatEqual(t *testing.T, got, want map[string]NumstatEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for path, wantEntry := range want {
		gotEntry, ok := got[path]
		if !ok {
			t.Errorf("missing entry for %q", path)
			continue
		}
		if gotEntry != wantEntry {
			t.Errorf("entry[%q]: got %+v, want %+v", path, gotEntry, wantEntry)
		}
	}
}

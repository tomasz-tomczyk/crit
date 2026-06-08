package main

import "testing"

// Pure-parser edge cases for parseJJDiffSummary that complement the contributor's
// jj_parse_test.go without overlapping its existing cases.
func TestParseJJDiffSummary_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []FileChange
	}{
		{
			name:  "nested rename braces",
			input: "R src/a/{b/old.go => c/new.go}",
			want:  []FileChange{{Path: "src/a/c/new.go", Status: "renamed"}},
		},
		{
			name:  "rename with trailing newline",
			input: "R {old.txt => new.txt}\n",
			want:  []FileChange{{Path: "new.txt", Status: "renamed"}},
		},
		{
			name:  "copy status ignored",
			input: "C src/copy.go",
			want:  nil,
		},
		{
			name: "copy mixed with modified",
			// `C` is not in the status map so it's dropped; `M` still parses.
			input: "C src/copy.go\nM src/real.go",
			want:  []FileChange{{Path: "src/real.go", Status: "modified"}},
		},
		{
			name:  "path with spaces inside braces",
			input: "R src/{old name.txt => new name.txt}",
			want:  []FileChange{{Path: "src/new name.txt", Status: "renamed"}},
		},
		{
			name:  "crlf only",
			input: "M a.go\r\nA b.go\r\n",
			want: []FileChange{
				{Path: "a.go", Status: "modified"},
				{Path: "b.go", Status: "added"},
			},
		},
		{
			// Snapshot of current behavior: parser doesn't strip BOMs, so a
			// BOM-prefixed status line fails the leading-status-char check and
			// is dropped. If BOM stripping is added later, update this case.
			name:  "leading bom is treated as junk and skipped",
			input: "\uFEFFM a.go",
			want:  nil,
		},
		{
			name:  "malformed missing space after status",
			input: "Mfile.go",
			want:  nil,
		},
		{
			name:  "malformed too short",
			input: "M",
			want:  nil,
		},
		{
			name:  "unknown status letter",
			input: "X foo.go",
			want:  nil,
		},
		{
			name:  "rename without arrow keeps trimmed braces",
			input: "R {nope.txt}",
			want:  []FileChange{{Path: "nope.txt", Status: "renamed"}},
		},
		{
			name:  "blank lines between entries",
			input: "M a.go\n\nM b.go\n",
			want: []FileChange{
				{Path: "a.go", Status: "modified"},
				{Path: "b.go", Status: "modified"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseJJDiffSummary(tt.input)
			assertFileChangesEqual(t, got, tt.want)
		})
	}
}

func TestParseJJDiffStat_Empty(t *testing.T) {
	got := parseSaplingDiffStat("")
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

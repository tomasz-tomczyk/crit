package main

import "testing"

func TestParseJJDiffSummary(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []FileChange
	}{
		{name: "empty", input: "", want: nil},
		{name: "modified", input: "M src/main.go", want: []FileChange{{Path: "src/main.go", Status: "modified"}}},
		{name: "added", input: "A new file.txt", want: []FileChange{{Path: "new file.txt", Status: "added"}}},
		{name: "deleted", input: "D old.go", want: []FileChange{{Path: "old.go", Status: "deleted"}}},
		{name: "rename whole path", input: "R {old.txt => new.txt}", want: []FileChange{{Path: "new.txt", Status: "renamed"}}},
		{name: "rename shared prefix", input: "R src/{old.go => new.go}", want: []FileChange{{Path: "src/new.go", Status: "renamed"}}},
		{
			name:  "mixed",
			input: "M app.go\r\nA docs/plan.md\nR {a.txt => b.txt}\nD gone.txt",
			want: []FileChange{
				{Path: "app.go", Status: "modified"},
				{Path: "docs/plan.md", Status: "added"},
				{Path: "b.txt", Status: "renamed"},
				{Path: "gone.txt", Status: "deleted"},
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

func TestParseJJDiffStat(t *testing.T) {
	got := parseSaplingDiffStat("app.go  | 2 +-\nnew.txt | 1 +\n2 files changed, 2 insertions(+), 1 deletion(-)")
	want := map[string]NumstatEntry{
		"app.go":  {Additions: 1, Deletions: 1},
		"new.txt": {Additions: 1, Deletions: 0},
	}
	assertNumstatEqual(t, got, want)
}

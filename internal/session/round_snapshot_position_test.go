package session

import "testing"

// TestRoundSnapshotPosition asserts that captureRoundSnapshot records each
// file's index in s.Files as Position.
func TestRoundSnapshotPosition(t *testing.T) {
	s := &Session{
		Mode:        "files",
		ReviewRound: 1,
		Files: []*FileEntry{
			{Path: "a.md", Status: "modified", Content: "a"},
			{Path: "b.md", Status: "modified", Content: "b"},
			{Path: "c.md", Status: "modified", Content: "c"},
		},
	}
	s.captureRoundSnapshot(1)

	cases := []struct {
		path string
		want int
	}{
		{"a.md", 0},
		{"b.md", 1},
		{"c.md", 2},
	}
	for _, tc := range cases {
		got, ok := s.roundSnapshotForFile(tc.path, 1)
		if !ok {
			t.Fatalf("snapshot missing for %s", tc.path)
		}
		if got.Position != tc.want {
			t.Errorf("%s: Position = %d, want %d", tc.path, got.Position, tc.want)
		}
	}
}

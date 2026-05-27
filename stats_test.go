package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadStats_MissingFile(t *testing.T) {
	setHome(t, t.TempDir())
	sf, err := loadStats()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sf.Totals.Sessions != 0 {
		t.Errorf("expected 0 sessions, got %d", sf.Totals.Sessions)
	}
	if len(sf.Sessions) != 0 {
		t.Errorf("expected empty sessions slice, got %d", len(sf.Sessions))
	}
}

func TestAppendSession(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	rec := sessionRecord{
		StartedAt: "2026-05-27T10:00:00Z",
		EndedAt:   "2026-05-27T10:15:00Z",
		Duration:  900,
		Files:     12,
		Comments:  5,
		Branch:    "feat/auth",
		Mode:      "git",
		Rounds:    2,
		ReviewKey: "abc123",
	}

	if err := appendSession(rec); err != nil {
		t.Fatalf("first append: %v", err)
	}

	sf, err := loadStats()
	if err != nil {
		t.Fatalf("load after first append: %v", err)
	}
	if sf.Totals.Sessions != 1 {
		t.Errorf("expected 1 session, got %d", sf.Totals.Sessions)
	}
	if sf.Totals.Duration != 900 {
		t.Errorf("expected 900s duration, got %d", sf.Totals.Duration)
	}
	if sf.Totals.Files != 12 {
		t.Errorf("expected 12 files, got %d", sf.Totals.Files)
	}
	if sf.Totals.Comments != 5 {
		t.Errorf("expected 5 comments, got %d", sf.Totals.Comments)
	}
	if len(sf.Sessions) != 1 {
		t.Fatalf("expected 1 session record, got %d", len(sf.Sessions))
	}
	if sf.Sessions[0].Branch != "feat/auth" {
		t.Errorf("expected branch feat/auth, got %s", sf.Sessions[0].Branch)
	}

	// Second append accumulates.
	rec2 := sessionRecord{
		StartedAt: "2026-05-28T14:00:00Z",
		EndedAt:   "2026-05-28T14:30:00Z",
		Duration:  1800,
		Files:     8,
		Comments:  3,
		Mode:      "files",
		Rounds:    1,
	}
	if err := appendSession(rec2); err != nil {
		t.Fatalf("second append: %v", err)
	}

	sf, err = loadStats()
	if err != nil {
		t.Fatalf("load after second append: %v", err)
	}
	if sf.Totals.Sessions != 2 {
		t.Errorf("expected 2 sessions, got %d", sf.Totals.Sessions)
	}
	if sf.Totals.Duration != 2700 {
		t.Errorf("expected 2700s total duration, got %d", sf.Totals.Duration)
	}
	if sf.Totals.Files != 20 {
		t.Errorf("expected 20 total files, got %d", sf.Totals.Files)
	}
	if sf.Totals.Comments != 8 {
		t.Errorf("expected 8 total comments, got %d", sf.Totals.Comments)
	}
}

func TestLoadStats_CorruptFile(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	dir := filepath.Join(home, ".crit")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "stats.json"), []byte("{invalid"), 0o644)

	_, err := loadStats()
	if err == nil {
		t.Fatal("expected error for corrupt file")
	}
}

func TestFormatTotalsLine(t *testing.T) {
	tests := []struct {
		name   string
		totals statsTotals
		want   string
	}{
		{
			name:   "singular",
			totals: statsTotals{Sessions: 1, Duration: 45, Files: 1, Comments: 1},
			want:   "1 session · 45s · 1 file · 1 comment",
		},
		{
			name:   "plural with hours",
			totals: statsTotals{Sessions: 47, Duration: 28380, Files: 312, Comments: 89},
			want:   "47 sessions · 7h 53m · 312 files · 89 comments",
		},
		{
			name:   "minutes only",
			totals: statsTotals{Sessions: 3, Duration: 300, Files: 10, Comments: 2},
			want:   "3 sessions · 5m · 10 files · 2 comments",
		},
		{
			name:   "exact hours",
			totals: statsTotals{Sessions: 5, Duration: 7200, Files: 50, Comments: 10},
			want:   "5 sessions · 2h · 50 files · 10 comments",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTotalsLine(tt.totals)
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds int
		want    string
	}{
		{0, "0s"},
		{30, "30s"},
		{59, "59s"},
		{60, "1m"},
		{90, "1m"},
		{3600, "1h"},
		{3660, "1h 1m"},
		{28380, "7h 53m"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.seconds)
		if got != tt.want {
			t.Errorf("formatDuration(%d) = %q, want %q", tt.seconds, got, tt.want)
		}
	}
}

func TestBusiestDay(t *testing.T) {
	sessions := []sessionRecord{
		{StartedAt: "2026-05-25T10:00:00Z"}, // Monday
		{StartedAt: "2026-05-25T14:00:00Z"}, // Monday
		{StartedAt: "2026-05-26T10:00:00Z"}, // Tuesday
		{StartedAt: "2026-05-27T10:00:00Z"}, // Wednesday
	}
	got := busiestDay(sessions)
	if got != "Monday" {
		t.Errorf("expected Monday, got %s", got)
	}
}

func TestBusiestDay_Empty(t *testing.T) {
	got := busiestDay(nil)
	if got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

func TestLongestSession(t *testing.T) {
	sessions := []sessionRecord{
		{Duration: 300, Branch: "fix/bug"},
		{Duration: 4320, Branch: "feat/auth"},
		{Duration: 600, Branch: "main"},
	}
	branch, dur := longestSession(sessions)
	if branch != "feat/auth" {
		t.Errorf("expected feat/auth, got %s", branch)
	}
	if dur != 4320 {
		t.Errorf("expected 4320, got %d", dur)
	}
}

func TestLongestSession_NoBranch(t *testing.T) {
	sessions := []sessionRecord{
		{Duration: 300, Mode: "files"},
		{Duration: 600, Mode: "files"},
	}
	branch, dur := longestSession(sessions)
	if branch != "files mode" {
		t.Errorf("expected 'files mode', got %s", branch)
	}
	if dur != 600 {
		t.Errorf("expected 600, got %d", dur)
	}
}

func TestComputeInsights_TooFewSessions(t *testing.T) {
	insights := computeInsights([]sessionRecord{{StartedAt: "2026-05-27T10:00:00Z"}})
	if len(insights) != 0 {
		t.Errorf("expected no insights for single session, got %v", insights)
	}
}

func TestComputeInsights_MultipleSessions(t *testing.T) {
	sessions := []sessionRecord{
		{StartedAt: "2026-05-25T10:00:00Z", Duration: 300, Branch: "fix/bug"},
		{StartedAt: "2026-05-25T14:00:00Z", Duration: 4320, Branch: "feat/auth"},
		{StartedAt: "2026-05-27T10:00:00Z", Duration: 600, Branch: "main"},
	}
	insights := computeInsights(sessions)
	if len(insights) != 2 {
		t.Fatalf("expected 2 insights, got %d: %v", len(insights), insights)
	}
	if insights[0] != "You review most on Mondays." {
		t.Errorf("unexpected day insight: %s", insights[0])
	}
	if insights[1] != "Longest session: 1h 12m on feat/auth." {
		t.Errorf("unexpected longest insight: %s", insights[1])
	}
}

func TestSessionActivity(t *testing.T) {
	sess := &Session{
		Files: []*FileEntry{
			{
				Comments: []Comment{
					{Author: "alice"},
					{Author: "bob"},
					{Author: "alice"},
				},
			},
			{
				Comments: []Comment{
					{Author: "alice"},
				},
			},
		},
		reviewComments: []Comment{
			{Author: "alice"},
			{Author: "bob"},
		},
	}

	files, comments := sessionActivity(sess, "alice")
	if files != 2 {
		t.Errorf("expected 2 files, got %d", files)
	}
	if comments != 4 {
		t.Errorf("expected 4 alice comments, got %d", comments)
	}

	_, bobComments := sessionActivity(sess, "bob")
	if bobComments != 2 {
		t.Errorf("expected 2 bob comments, got %d", bobComments)
	}

	_, charlieComments := sessionActivity(sess, "charlie")
	if charlieComments != 0 {
		t.Errorf("expected 0 charlie comments, got %d", charlieComments)
	}
}

func TestRecordSessionStats_SkipsEmptySessions(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	sess := &Session{Mode: "git"}
	recordSessionStats(sess, "alice", time.Now().Add(-5*time.Minute))

	// File should not exist — session had no files and no comments.
	path := filepath.Join(home, ".crit", "stats.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no stats file for empty session, but file exists")
	}
}

func TestRecordSessionStats_WritesForActivity(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	start := time.Now().Add(-10 * time.Minute)
	sess := &Session{
		Mode:           "git",
		Branch:         "feat/stats",
		ReviewFilePath: filepath.Join(home, ".crit", "reviews", "abc123"),
		Files: []*FileEntry{
			{Path: "main.go"},
			{Path: "stats.go"},
		},
		reviewComments: []Comment{
			{Author: "alice"},
		},
	}

	recordSessionStats(sess, "alice", start)

	sf, err := loadStats()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if sf.Totals.Sessions != 1 {
		t.Fatalf("expected 1 session, got %d", sf.Totals.Sessions)
	}
	if sf.Totals.Files != 2 {
		t.Errorf("expected 2 files, got %d", sf.Totals.Files)
	}
	if sf.Totals.Comments != 1 {
		t.Errorf("expected 1 comment, got %d", sf.Totals.Comments)
	}
	if sf.Sessions[0].Branch != "feat/stats" {
		t.Errorf("expected branch feat/stats, got %s", sf.Sessions[0].Branch)
	}
	if sf.Sessions[0].ReviewKey != "abc123" {
		t.Errorf("expected review key abc123, got %s", sf.Sessions[0].ReviewKey)
	}
	if sf.Sessions[0].Duration < 590 {
		t.Errorf("expected ~600s duration, got %d", sf.Sessions[0].Duration)
	}
}

func TestStatsJSON_EmptyFile(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	sf, err := loadStats()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(sf)
	if err != nil {
		t.Fatal(err)
	}
	var parsed statsFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Totals.Sessions != 0 {
		t.Errorf("expected 0 sessions in JSON, got %d", parsed.Totals.Sessions)
	}
}

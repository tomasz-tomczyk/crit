package session

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
)

type statsFile struct {
	Totals   statsTotals     `json:"totals"`
	Sessions []sessionRecord `json:"sessions"`
}

type statsTotals struct {
	Sessions int `json:"sessions"`
	Duration int `json:"duration_seconds"`
	Files    int `json:"files_reviewed"`
	Comments int `json:"comments_submitted"`
}

type sessionRecord struct {
	StartedAt string   `json:"started_at"`
	EndedAt   string   `json:"ended_at"`
	Duration  int      `json:"duration_seconds"`
	Files     int      `json:"files_reviewed"`
	Comments  int      `json:"comments_submitted"`
	Branch    string   `json:"branch,omitempty"`
	Mode      string   `json:"mode"`
	Rounds    int      `json:"rounds"`
	ReviewKey string   `json:"review_key,omitempty"`
	Args      []string `json:"args,omitempty"`
}

func statsFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".crit", "stats.json"), nil
}

func loadStats() (statsFile, error) {
	path, err := statsFilePath()
	if err != nil {
		return statsFile{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return statsFile{}, nil
		}
		return statsFile{}, fmt.Errorf("reading stats file: %w", err)
	}
	var sf statsFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return statsFile{}, fmt.Errorf("parsing stats file: %w", err)
	}
	return sf, nil
}

// appendSession is not safe against concurrent writers (two daemons finishing
// in the same millisecond window could lose an update). Acceptable: the window
// is tiny and stats are best-effort.
func appendSession(rec sessionRecord) error {
	sf, err := loadStats()
	if err != nil {
		return err
	}

	sf.Sessions = append(sf.Sessions, rec)
	sf.Totals.Sessions++
	sf.Totals.Duration += rec.Duration
	sf.Totals.Files += rec.Files
	sf.Totals.Comments += rec.Comments

	path, err := statsFilePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating stats directory: %w", err)
	}

	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding stats: %w", err)
	}

	return AtomicWriteFile(path, data, 0o644)
}

// sessionActivity returns file count and user comment count under a single lock.
func sessionActivity(sess *Session, author string) (files, comments int) {
	sess.mu.RLock()
	defer sess.mu.RUnlock()

	files = len(sess.Files)
	for _, c := range sess.reviewComments {
		if c.Author == author {
			comments++
		}
	}
	for _, f := range sess.Files {
		for _, c := range f.Comments {
			if c.Author == author {
				comments++
			}
		}
	}
	return
}

// recordSessionStats builds a sessionRecord from the current session state and
// appends it to ~/.crit/stats.json. Called from handleFinish (on approve) and
// during daemon shutdown. Skips sessions with no meaningful activity.
func recordSessionStats(sess *Session, author string, startedAt time.Time) {
	now := time.Now().UTC()
	duration := int(math.Round(now.Sub(startedAt).Seconds()))

	files, comments := sessionActivity(sess, author)

	if comments == 0 && files == 0 {
		return
	}

	mode := sess.Mode
	if sess.ReviewType != "" {
		mode = sess.ReviewType
	}

	rec := sessionRecord{
		StartedAt: startedAt.UTC().Format(time.RFC3339),
		EndedAt:   now.Format(time.RFC3339),
		Duration:  duration,
		Files:     files,
		Comments:  comments,
		Branch:    sess.Branch,
		Mode:      mode,
		Rounds:    sess.GetReviewRound(),
		Args:      sess.CLIArgs,
	}

	if sess.ReviewFilePath != "" {
		rec.ReviewKey = filepath.Base(sess.ReviewFilePath)
	}

	if err := appendSession(rec); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to record session stats: %v\n", err)
	}
}

// --- CLI output ---

// RunStats prints review session statistics.
func RunStats(args []string) {
	jsonOutput := false
	for _, a := range args {
		if a == "--json" {
			jsonOutput = true
		}
	}

	sf, err := loadStats()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(sf, "", "  ")
		fmt.Println(string(data))
		return
	}

	if sf.Totals.Sessions == 0 {
		fmt.Println("No review sessions recorded yet.")
		return
	}

	fmt.Println(formatTotalsLine(sf.Totals))

	if insights := computeInsights(sf.Sessions); len(insights) > 0 {
		fmt.Println()
		for _, ins := range insights {
			fmt.Println(ins)
		}
	}
}

func formatTotalsLine(t statsTotals) string {
	return fmt.Sprintf("%d %s · %s · %d %s · %d %s",
		t.Sessions, pluralize(t.Sessions, "session", "sessions"),
		formatDuration(t.Duration),
		t.Files, pluralize(t.Files, "file", "files"),
		t.Comments, pluralize(t.Comments, "comment", "comments"),
	)
}

func formatDuration(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	if hours == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func computeInsights(sessions []sessionRecord) []string {
	if len(sessions) < 2 {
		return nil
	}

	var insights []string

	if day := busiestDay(sessions); day != "" {
		insights = append(insights, fmt.Sprintf("You review most on %ss.", day))
	}

	if branch, dur := longestSession(sessions); branch != "" {
		insights = append(insights, fmt.Sprintf("Longest session: %s on %s.", formatDuration(dur), branch))
	}

	return insights
}

func busiestDay(sessions []sessionRecord) string {
	counts := make(map[time.Weekday]int)
	for _, s := range sessions {
		t, err := time.Parse(time.RFC3339, s.StartedAt)
		if err != nil {
			continue
		}
		counts[t.Weekday()]++
	}
	if len(counts) == 0 {
		return ""
	}

	var best time.Weekday
	bestCount := 0
	// Deterministic iteration order.
	for d := time.Sunday; d <= time.Saturday; d++ {
		if counts[d] > bestCount {
			bestCount = counts[d]
			best = d
		}
	}
	return best.String()
}

func longestSession(sessions []sessionRecord) (string, int) {
	var bestBranch string
	var bestDur int
	for _, s := range sessions {
		if s.Duration > bestDur {
			bestDur = s.Duration
			bestBranch = s.Branch
		}
	}
	if bestBranch == "" {
		// All sessions had empty branch — try to find any with mode info.
		for _, s := range sessions {
			if s.Duration >= bestDur && s.Mode != "" {
				bestBranch = s.Mode + " mode"
				bestDur = s.Duration
			}
		}
	}
	if bestBranch == "" {
		return "", 0
	}
	return bestBranch, bestDur
}

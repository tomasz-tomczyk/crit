package main

import (
	"strings"
)

// saplingStatusMap maps Sapling/Mercurial status characters to crit status strings.
var saplingStatusMap = map[byte]string{
	'M': "modified",
	'A': "added",
	'R': "deleted",
	'?': "untracked",
	'!': "deleted",
}

// parseSaplingStatus parses `sl status` output (Mercurial format).
// Each line is: <status-char> <space> <path>
// Maps: M->"modified", A->"added", R->"deleted", ?->"untracked", !->"deleted"
// Skips: I (ignored), C (clean), and any unrecognized status characters.
func parseSaplingStatus(output string) []FileChange {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil
	}

	lines := strings.Split(trimmed, "\n")
	var changes []FileChange
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if len(line) < 3 || line[1] != ' ' {
			continue
		}
		status, ok := saplingStatusMap[line[0]]
		if !ok {
			continue
		}
		path := line[2:]
		changes = append(changes, FileChange{Path: path, Status: status})
	}
	return changes
}

// parseSaplingDiffStat parses `sl diff --stat` output.
// Each file line has the format: " path | N +++--"
// The summary line ("N files changed, ...") is skipped.
// The function counts '+' and '-' characters in the visual bar
// to determine additions and deletions.
func parseSaplingDiffStat(output string) map[string]NumstatEntry {
	result := make(map[string]NumstatEntry)
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return result
	}

	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimRight(line, "\r")
		if isSummaryLine(line) {
			continue
		}
		path, entry, ok := parseDiffStatLine(line)
		if !ok {
			continue
		}
		result[path] = entry
	}
	return result
}

// isSummaryLine returns true if the line is a diff-stat summary
// (e.g., " 3 files changed, 10 insertions(+), 2 deletions(-)").
func isSummaryLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.Contains(trimmed, "files changed") ||
		strings.Contains(trimmed, "file changed")
}

// parseDiffStatLine parses a single diff --stat file line.
// Format: " path | N +++--"
// Returns the path, a NumstatEntry, and whether parsing succeeded.
func parseDiffStatLine(line string) (string, NumstatEntry, bool) {
	pipeIdx := strings.LastIndex(line, "|")
	if pipeIdx < 0 {
		return "", NumstatEntry{}, false
	}
	path := strings.TrimSpace(line[:pipeIdx])
	if path == "" {
		return "", NumstatEntry{}, false
	}

	right := line[pipeIdx+1:]
	adds, dels := countPlusMinus(right)
	return path, NumstatEntry{Additions: adds, Deletions: dels}, true
}

// countPlusMinus counts '+' and '-' characters in a diff-stat bar segment.
func countPlusMinus(s string) (int, int) {
	var adds, dels int
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '+':
			adds++
		case '-':
			dels++
		}
	}
	return adds, dels
}

package main

import "strings"

// DiffEntry represents a single line in the diff output.
type DiffEntry struct {
	Type    string `json:"type"`               // "unchanged", "added", or "removed"
	OldLine int    `json:"old_line,omitempty"` // 1-based line number in old content (0 if added)
	NewLine int    `json:"new_line,omitempty"` // 1-based line number in new content (0 if removed)
	Text    string `json:"text"`
}

// ComputeLineDiff computes a line-level diff between oldContent and newContent
// using the LCS (Longest Common Subsequence) algorithm. Each line is classified
// as "unchanged", "added", or "removed".
func ComputeLineDiff(oldContent, newContent string) []DiffEntry {
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)

	m, n := len(oldLines), len(newLines)

	// Build LCS table
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if oldLines[i-1] == newLines[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to build diff
	var result []DiffEntry
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			result = append([]DiffEntry{{Type: "unchanged", OldLine: i, NewLine: j, Text: newLines[j-1]}}, result...)
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			result = append([]DiffEntry{{Type: "added", NewLine: j, Text: newLines[j-1]}}, result...)
			j--
		} else {
			result = append([]DiffEntry{{Type: "removed", OldLine: i, Text: oldLines[i-1]}}, result...)
			i--
		}
	}
	return result
}

// MapOldLineToNew builds a mapping from old line numbers to new line numbers
// using the diff entries. For unchanged lines it maps directly. For removed
// lines it maps to the nearest subsequent new line in the new document.
// Returns a map[int]int where key=old line, value=new line.
func MapOldLineToNew(entries []DiffEntry) map[int]int {
	m := make(map[int]int)
	// First pass: map all unchanged lines directly
	for _, e := range entries {
		if e.Type == "unchanged" {
			m[e.OldLine] = e.NewLine
		}
	}
	// Second pass (reverse): for removed lines, find the next new line after them.
	// Walking backwards, we track the next new line we'll encounter going forward.
	nextNewLine := 0
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.NewLine > 0 {
			nextNewLine = e.NewLine
		}
		if e.Type == "removed" {
			if _, ok := m[e.OldLine]; !ok {
				m[e.OldLine] = nextNewLine
			}
		}
	}
	// If nextNewLine is still 0 for some removed lines (all content was removed
	// with no new lines at all), find the last new line as fallback.
	if nextNewLine == 0 {
		lastNewLine := 0
		for _, e := range entries {
			if e.NewLine > 0 {
				lastNewLine = e.NewLine
			}
		}
		if lastNewLine > 0 {
			for _, e := range entries {
				if e.Type == "removed" && m[e.OldLine] == 0 {
					m[e.OldLine] = lastNewLine
				}
			}
		}
	}
	return m
}

// splitLines splits content into lines, returning an empty slice for empty input.
func splitLines(content string) []string {
	if content == "" {
		return []string{}
	}
	return strings.Split(content, "\n")
}

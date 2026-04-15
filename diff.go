package main

import (
	"fmt"
	"strings"
)

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
			switch {
			case oldLines[i-1] == newLines[j-1]:
				dp[i][j] = dp[i-1][j-1] + 1
			case dp[i-1][j] >= dp[i][j-1]:
				dp[i][j] = dp[i-1][j]
			default:
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to build diff (collect in reverse, then flip)
	var reversed []DiffEntry
	i, j := m, n
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && oldLines[i-1] == newLines[j-1]:
			reversed = append(reversed, DiffEntry{Type: "unchanged", OldLine: i, NewLine: j, Text: newLines[j-1]})
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			reversed = append(reversed, DiffEntry{Type: "added", NewLine: j, Text: newLines[j-1]})
			j--
		default:
			reversed = append(reversed, DiffEntry{Type: "removed", OldLine: i, Text: oldLines[i-1]})
			i--
		}
	}
	// Reverse to get forward order
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	return reversed
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

// groupChangedIndices groups changed entry indices into ranges separated by gaps
// larger than 2*contextLines of unchanged lines.
func groupChangedIndices(changedIndices []int, contextLines int) [][2]int {
	var groups [][2]int
	groupStart := changedIndices[0]
	groupEnd := changedIndices[0]
	for _, ci := range changedIndices[1:] {
		if ci-groupEnd > 2*contextLines {
			groups = append(groups, [2]int{groupStart, groupEnd})
			groupStart = ci
		}
		groupEnd = ci
	}
	groups = append(groups, [2]int{groupStart, groupEnd})
	return groups
}

func entryToDiffLine(e DiffEntry) (DiffLine, int, int) {
	switch e.Type {
	case "unchanged":
		return DiffLine{Type: "context", Content: e.Text, OldNum: e.OldLine, NewNum: e.NewLine}, 1, 1
	case "removed":
		return DiffLine{Type: "del", Content: e.Text, OldNum: e.OldLine}, 1, 0
	case "added":
		return DiffLine{Type: "add", Content: e.Text, NewNum: e.NewLine}, 0, 1
	default:
		return DiffLine{}, 0, 0
	}
}

func buildHunkFromGroup(entries []DiffEntry, gStart, gEnd, contextLines int) DiffHunk {
	start := gStart - contextLines
	if start < 0 {
		start = 0
	}
	end := gEnd + contextLines
	if end >= len(entries) {
		end = len(entries) - 1
	}

	var lines []DiffLine
	var oldStart, newStart, oldCount, newCount int
	for i := start; i <= end; i++ {
		dl, oldInc, newInc := entryToDiffLine(entries[i])
		oldCount += oldInc
		newCount += newInc
		if oldStart == 0 && dl.OldNum > 0 {
			oldStart = dl.OldNum
		}
		if newStart == 0 && dl.NewNum > 0 {
			newStart = dl.NewNum
		}
		lines = append(lines, dl)
	}
	if oldStart == 0 {
		oldStart = 1
	}
	if newStart == 0 {
		newStart = 1
	}

	return DiffHunk{
		OldStart: oldStart, OldCount: oldCount,
		NewStart: newStart, NewCount: newCount,
		Header: fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldStart, oldCount, newStart, newCount),
		Lines:  lines,
	}
}

// DiffEntriesToHunks converts LCS diff entries into DiffHunk format (same as git diff),
// so the frontend can use one unified renderer. Groups changes with 3 lines of context.
func DiffEntriesToHunks(entries []DiffEntry) []DiffHunk {
	if len(entries) == 0 {
		return nil
	}

	const contextLines = 3

	var changedIndices []int
	for i, e := range entries {
		if e.Type != "unchanged" {
			changedIndices = append(changedIndices, i)
		}
	}
	if len(changedIndices) == 0 {
		return nil
	}

	groups := groupChangedIndices(changedIndices, contextLines)

	hunks := make([]DiffHunk, 0, len(groups))
	for _, g := range groups {
		hunks = append(hunks, buildHunkFromGroup(entries, g[0], g[1], contextLines))
	}
	return hunks
}

// splitLines splits content into lines, returning an empty slice for empty input.
func splitLines(content string) []string {
	if content == "" {
		return []string{}
	}
	return strings.Split(content, "\n")
}

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
// using Hirschberg's algorithm (divide-and-conquer LCS in O(n) space).
// Each line is classified as "unchanged", "added", or "removed".
func ComputeLineDiff(oldContent, newContent string) []DiffEntry {
	oldLines := splitLines(oldContent)
	newLines := splitLines(newContent)
	return hirschbergDiff(oldLines, newLines, 0, 0)
}

// hirschbergDiff computes the diff between old[0:len(old)] and new[0:len(new)]
// using Hirschberg's divide-and-conquer algorithm. oldOff and newOff are the
// 0-based offsets into the original arrays, used to compute 1-based line numbers.
func hirschbergDiff(old, new []string, oldOff, newOff int) []DiffEntry {
	m, n := len(old), len(new)

	if m == 0 {
		return allAdded(new, newOff)
	}
	if n == 0 {
		return allRemoved(old, oldOff)
	}
	if m == 1 {
		return diffSingleOld(old[0], new, oldOff, newOff)
	}

	mid := m / 2

	// Forward: LCS row scores for old[0:mid] vs new
	topRow := lcsForwardRow(old[:mid], new)
	// Backward: LCS row scores for old[mid:] vs new (reversed)
	botRow := lcsBackwardRow(old[mid:], new)

	// Find the split point in new that maximizes topRow[k] + botRow[n-k]
	bestK := 0
	bestScore := topRow[0] + botRow[n]
	for k := 1; k <= n; k++ {
		score := topRow[k] + botRow[n-k]
		if score > bestScore {
			bestScore = score
			bestK = k
		}
	}

	// Recurse on two halves
	left := hirschbergDiff(old[:mid], new[:bestK], oldOff, newOff)
	right := hirschbergDiff(old[mid:], new[bestK:], oldOff+mid, newOff+bestK)
	return append(left, right...)
}

// diffSingleOld handles the base case where old has exactly one line.
// It finds the first occurrence in new that matches, emitting adds before,
// the unchanged match, and adds after. If no match, emits a removal then all adds.
func diffSingleOld(oldLine string, new []string, oldOff, newOff int) []DiffEntry {
	matchIdx := -1
	for i, line := range new {
		if line == oldLine {
			matchIdx = i
			break
		}
	}

	if matchIdx < 0 {
		entries := make([]DiffEntry, 0, 1+len(new))
		entries = append(entries, DiffEntry{Type: "removed", OldLine: oldOff + 1, Text: oldLine})
		for i, line := range new {
			entries = append(entries, DiffEntry{Type: "added", NewLine: newOff + i + 1, Text: line})
		}
		return entries
	}

	entries := make([]DiffEntry, 0, len(new))
	for i := 0; i < matchIdx; i++ {
		entries = append(entries, DiffEntry{Type: "added", NewLine: newOff + i + 1, Text: new[i]})
	}
	entries = append(entries, DiffEntry{
		Type: "unchanged", OldLine: oldOff + 1, NewLine: newOff + matchIdx + 1, Text: oldLine,
	})
	for i := matchIdx + 1; i < len(new); i++ {
		entries = append(entries, DiffEntry{Type: "added", NewLine: newOff + i + 1, Text: new[i]})
	}
	return entries
}

// allAdded returns DiffEntry slices for all-added lines.
func allAdded(lines []string, offset int) []DiffEntry {
	entries := make([]DiffEntry, len(lines))
	for i, line := range lines {
		entries[i] = DiffEntry{Type: "added", NewLine: offset + i + 1, Text: line}
	}
	return entries
}

// allRemoved returns DiffEntry slices for all-removed lines.
func allRemoved(lines []string, offset int) []DiffEntry {
	entries := make([]DiffEntry, len(lines))
	for i, line := range lines {
		entries[i] = DiffEntry{Type: "removed", OldLine: offset + i + 1, Text: line}
	}
	return entries
}

// lcsForwardRow computes the last row of the LCS table for old vs new
// scanning forward. Returns a slice of length len(new)+1.
func lcsForwardRow(old, new []string) []int {
	n := len(new)
	prev := make([]int, n+1)
	curr := make([]int, n+1)
	for _, oldLine := range old {
		for j := 1; j <= n; j++ {
			switch {
			case oldLine == new[j-1]:
				curr[j] = prev[j-1] + 1
			case prev[j] >= curr[j-1]:
				curr[j] = prev[j]
			default:
				curr[j] = curr[j-1]
			}
		}
		prev, curr = curr, prev
		// Zero out curr for reuse
		for j := range curr {
			curr[j] = 0
		}
	}
	return prev
}

// lcsBackwardRow computes the last row of the LCS table for old vs new
// scanning backward (i.e., LCS of reversed sequences). Returns a slice of
// length len(new)+1 where result[k] is the LCS length of old reversed vs
// the last k elements of new.
func lcsBackwardRow(old, new []string) []int {
	n := len(new)
	prev := make([]int, n+1)
	curr := make([]int, n+1)
	for i := len(old) - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			switch {
			case old[i] == new[j]:
				curr[n-j] = prev[n-j-1] + 1
			case prev[n-j] >= curr[n-j-1]:
				curr[n-j] = prev[n-j]
			default:
				curr[n-j] = curr[n-j-1]
			}
		}
		prev, curr = curr, prev
		for j := range curr {
			curr[j] = 0
		}
	}
	return prev
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

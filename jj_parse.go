package main

import (
	"strings"
)

// jjSummaryStatusMap maps `jj diff --summary` status letters to crit status strings.
var jjSummaryStatusMap = map[byte]string{
	'M': "modified",
	'A': "added",
	'D': "deleted",
	'R': "renamed",
}

// parseJJDiffSummary parses `jj diff --summary` output into crit file changes.
// Renames keep the new path so the review attaches comments to the current file.
func parseJJDiffSummary(output string) []FileChange {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil
	}

	var changes []FileChange
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 3 || line[1] != ' ' {
			continue
		}
		status, ok := jjSummaryStatusMap[line[0]]
		if !ok {
			continue
		}
		path := line[2:]
		if status == "renamed" {
			path = parseJJRenameTarget(path)
		}
		if path == "" {
			continue
		}
		changes = append(changes, FileChange{Path: path, Status: status})
	}
	return changes
}

// parseJJRenameTarget extracts the destination from JJ's compact rename syntax.
// Examples: `{old.txt => new.txt}` -> `new.txt`, `src/{old.go => new.go}` -> `src/new.go`.
func parseJJRenameTarget(path string) string {
	path = strings.TrimSpace(path)
	idx := strings.LastIndex(path, " => ")
	if idx < 0 {
		return strings.Trim(path, "{}")
	}

	leftBrace := strings.LastIndex(path[:idx], "{")
	rightRel := strings.Index(path[idx+4:], "}")
	if leftBrace >= 0 && rightRel >= 0 {
		rightBrace := idx + 4 + rightRel
		return path[:leftBrace] + path[idx+4:rightBrace] + path[rightBrace+1:]
	}
	return strings.Trim(strings.TrimSpace(path[idx+4:]), "{}")
}

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
		var oldPath string
		if status == "renamed" {
			oldPath, path = parseJJRenamePaths(path)
		}
		if path == "" {
			continue
		}
		changes = append(changes, FileChange{Path: path, OldPath: oldPath, Status: status})
	}
	return changes
}

// parseJJRenamePaths extracts old and new paths from JJ's compact rename syntax.
// Examples: `{old.txt => new.txt}` -> `old.txt`, `new.txt`;
// `src/{old.go => new.go}` -> `src/old.go`, `src/new.go`.
func parseJJRenamePaths(path string) (oldPath, newPath string) {
	path = strings.TrimSpace(path)
	idx := strings.LastIndex(path, " => ")
	if idx < 0 {
		return "", strings.Trim(path, "{}")
	}

	leftBrace := strings.LastIndex(path[:idx], "{")
	rightRel := strings.Index(path[idx+4:], "}")
	if leftBrace >= 0 && rightRel >= 0 {
		rightBrace := idx + 4 + rightRel
		oldPart := path[leftBrace+1 : idx]
		newPath = path[:leftBrace] + path[idx+4:rightBrace] + path[rightBrace+1:]
		oldPath = path[:leftBrace] + oldPart + path[rightBrace+1:]
		return oldPath, newPath
	}
	oldPath = strings.Trim(strings.TrimSpace(path[:idx]), "{}")
	newPath = strings.Trim(strings.TrimSpace(path[idx+4:]), "{}")
	return oldPath, newPath
}

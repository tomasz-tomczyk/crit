package main

import (
	"fmt"
	"sort"
	"strings"
)

func GenerateReviewMD(content string, comments []Comment) string {
	// Filter out resolved comments
	var activeComments []Comment
	for _, c := range comments {
		if !c.Resolved {
			activeComments = append(activeComments, c)
		}
	}

	if len(activeComments) == 0 {
		return content
	}

	lines := strings.Split(content, "\n")

	// Group comments by the line AFTER which they should be inserted.
	// Comments are inserted after their end_line.
	// We need to find the end of the block that contains end_line.
	// For simplicity, insert after end_line.
	sorted := make([]Comment, len(activeComments))
	copy(sorted, activeComments)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].EndLine == sorted[j].EndLine {
			return sorted[i].StartLine < sorted[j].StartLine
		}
		return sorted[i].EndLine < sorted[j].EndLine
	})

	// Build a map of end_line -> comments to insert after that line
	insertAfter := map[int][]Comment{}
	for _, c := range sorted {
		insertAfter[c.EndLine] = append(insertAfter[c.EndLine], c)
	}

	var result strings.Builder
	for i, line := range lines {
		lineNum := i + 1 // 1-indexed
		result.WriteString(line)
		if i < len(lines)-1 {
			result.WriteString("\n")
		}

		if cmts, ok := insertAfter[lineNum]; ok {
			for _, c := range cmts {
				result.WriteString("\n")
				result.WriteString(formatComment(c))
				result.WriteString("\n")
			}
		}
	}


	return result.String()
}

func formatComment(c Comment) string {
	var header string
	if c.StartLine == c.EndLine {
		header = fmt.Sprintf("Line %d", c.StartLine)
	} else {
		header = fmt.Sprintf("Lines %d-%d", c.StartLine, c.EndLine)
	}

	// Format comment body as blockquote lines
	bodyLines := strings.Split(c.Body, "\n")
	var quoted strings.Builder
	quoted.WriteString(fmt.Sprintf("> **[REVIEW COMMENT — %s]**: ", header))

	for i, bl := range bodyLines {
		if i == 0 {
			quoted.WriteString(bl)
		} else {
			quoted.WriteString("\n> " + bl)
		}
	}

	return quoted.String()
}

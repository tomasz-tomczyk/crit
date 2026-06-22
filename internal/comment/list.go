package comment

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ListedComment is a flattened comment for CLI output.
type ListedComment struct {
	Scope     string  `json:"scope"`
	ID        string  `json:"id"`
	Path      *string `json:"path"`
	StartLine *int    `json:"start_line"`
	EndLine   *int    `json:"end_line"`
	Body      string  `json:"body"`
	Quote     *string `json:"quote,omitempty"`
	Anchor    *string `json:"anchor,omitempty"`
	Drifted   bool    `json:"drifted,omitempty"`
	Replies   []Reply `json:"replies,omitempty"`
}

func isUnresolved(c Comment) bool {
	return !c.Resolved
}

func commentScope(c Comment) string {
	if c.Scope == "review" {
		return "review"
	}
	if c.Scope == "file" {
		return "file"
	}
	return "line"
}

func listCommentsFromCritJSON(cj CritJSON, unresolvedOnly bool) []ListedComment {
	var out []ListedComment

	for _, c := range cj.ReviewComments {
		if unresolvedOnly && !isUnresolved(c) {
			continue
		}
		out = append(out, toListedComment("review", "", c))
	}

	paths := make([]string, 0, len(cj.Files))
	for path := range cj.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		var fileLevel, lineLevel []Comment
		for _, c := range cj.Files[path].Comments {
			if unresolvedOnly && !isUnresolved(c) {
				continue
			}
			if commentScope(c) == "file" {
				fileLevel = append(fileLevel, c)
			} else {
				lineLevel = append(lineLevel, c)
			}
		}
		sort.Slice(lineLevel, func(i, j int) bool {
			return lineLevel[i].StartLine < lineLevel[j].StartLine
		})
		for _, c := range fileLevel {
			out = append(out, toListedComment("file", path, c))
		}
		for _, c := range lineLevel {
			out = append(out, toListedComment("line", path, c))
		}
	}
	return out
}

func toListedComment(scope, path string, c Comment) ListedComment {
	lc := ListedComment{
		Scope:   scope,
		ID:      c.ID,
		Body:    c.Body,
		Drifted: c.Drifted,
		Replies: c.Replies,
	}
	if path != "" {
		lc.Path = &path
	}
	if scope == "line" {
		start, end := c.StartLine, c.EndLine
		if end == 0 {
			end = start
		}
		lc.StartLine = &start
		lc.EndLine = &end
	}
	if c.Quote != "" {
		q := c.Quote
		lc.Quote = &q
	}
	if c.Anchor != "" {
		a := c.Anchor
		lc.Anchor = &a
	}
	return lc
}

func formatCommentsText(entries []ListedComment, unresolvedOnly bool) string {
	n := len(entries)
	if n == 0 {
		if unresolvedOnly {
			return "No unresolved comments."
		}
		return "No comments."
	}
	var b strings.Builder
	if unresolvedOnly {
		fmt.Fprintf(&b, "%d unresolved comment%s:\n", n, plural(n))
	} else {
		fmt.Fprintf(&b, "%d comment%s:\n", n, plural(n))
	}
	for _, e := range entries {
		b.WriteByte('\n')
		b.WriteString(formatCommentHeader(e))
		if e.Quote != nil && *e.Quote != "" {
			b.WriteByte('\n')
			b.WriteString(indentLines(2, "quote:  "+*e.Quote))
		}
		if e.Anchor != nil && *e.Anchor != "" {
			b.WriteByte('\n')
			b.WriteString(indentLines(2, "anchor: "+*e.Anchor))
		}
		b.WriteByte('\n')
		b.WriteString(indentLines(2, "body:   "+e.Body))
		if len(e.Replies) > 0 {
			b.WriteString("\n  replies:")
			for _, r := range e.Replies {
				author := r.Author
				if author == "" {
					author = "?"
				}
				b.WriteByte('\n')
				b.WriteString(indentLines(4, fmt.Sprintf("- [%s] %s: %s", r.ID, author, r.Body)))
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatCommentHeader(e ListedComment) string {
	header := fmt.Sprintf("[%s] %s", e.ID, e.Scope)
	if e.Path != nil {
		header += " " + *e.Path + formatLineLoc(e.StartLine, e.EndLine)
	}
	if e.Drifted {
		header += " (drifted)"
	}
	return header
}

func formatLineLoc(start, end *int) string {
	if start == nil {
		return ""
	}
	if end == nil || *end == *start {
		return fmt.Sprintf(":%d", *start)
	}
	return fmt.Sprintf(":%d-%d", *start, *end)
}

func indentLines(spaces int, s string) string {
	pad := strings.Repeat(" ", spaces)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = pad + line
	}
	return strings.Join(lines, "\n")
}

func encodeCommentsJSON(entries []ListedComment) ([]byte, error) {
	return json.MarshalIndent(entries, "", "  ")
}

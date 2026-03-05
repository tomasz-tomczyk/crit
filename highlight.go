package main

import (
	"html"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

var chromaFormatter = chromahtml.New(
	chromahtml.WithClasses(true),
	chromahtml.PreventSurroundingPre(true),
)

var chromaStyle = styles.Get("github-dark")

// HighlightLines highlights full file content and returns a 1-indexed slice of
// per-line HTML strings. Returns nil if the language is not recognized or content is empty.
// result[0] = nil (unused), result[1] = highlighted HTML for line 1, etc.
func HighlightLines(content, filename string) []*string {
	if content == "" {
		return nil
	}

	lexer := lexers.Match(filename)
	if lexer == nil {
		return nil
	}
	lexer = chroma.Coalesce(lexer)

	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return nil
	}

	var buf strings.Builder
	if err := chromaFormatter.Format(&buf, chromaStyle, iterator); err != nil {
		return nil
	}

	highlighted := buf.String()
	rawLines := strings.Split(highlighted, "\n")

	result := make([]*string, len(rawLines)+1)
	for i, line := range rawLines {
		l := line
		result[i+1] = &l
	}

	return result
}

// HighlightLine highlights a single line of code. Used as a fallback for
// old-side diff lines not in the current file content.
func HighlightLine(content, filename string) string {
	lexer := lexers.Match(filename)
	if lexer == nil {
		return html.EscapeString(content)
	}
	lexer = chroma.Coalesce(lexer)

	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return html.EscapeString(content)
	}

	var buf strings.Builder
	if err := chromaFormatter.Format(&buf, chromaStyle, iterator); err != nil {
		return html.EscapeString(content)
	}

	return buf.String()
}

var fencedCodeBlockRe = regexp.MustCompile("(?m)^```(\\w+)\\s*\\n([\\s\\S]*?)^```\\s*$")

// HighlightCodeBlocks finds fenced code blocks in markdown content and returns
// a map of lang+"\n"+code -> highlighted HTML. The frontend uses this to avoid
// client-side highlighting for markdown code blocks.
func HighlightCodeBlocks(content string) map[string]string {
	matches := fencedCodeBlockRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}

	result := make(map[string]string, len(matches))
	for _, m := range matches {
		lang := m[1]
		code := m[2]
		key := lang + "\n" + code

		if _, exists := result[key]; exists {
			continue
		}

		highlighted := HighlightCode(code, lang)
		if highlighted != "" {
			result[key] = highlighted
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// HighlightCode highlights a code snippet using a language name (not filename).
func HighlightCode(code, lang string) string {
	lexer := lexers.Get(lang)
	if lexer == nil {
		return ""
	}
	lexer = chroma.Coalesce(lexer)

	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return ""
	}

	var buf strings.Builder
	if err := chromaFormatter.Format(&buf, chromaStyle, iterator); err != nil {
		return ""
	}

	return buf.String()
}

// HighlightDiffHunks attaches pre-highlighted HTML to each DiffLine in the hunks.
func HighlightDiffHunks(hunks []DiffHunk, highlightedLines []*string, filename string) {
	for i := range hunks {
		for j := range hunks[i].Lines {
			line := &hunks[i].Lines[j]
			switch line.Type {
			case "add", "context":
				if highlightedLines != nil && line.NewNum > 0 && line.NewNum < len(highlightedLines) && highlightedLines[line.NewNum] != nil {
					line.HTML = *highlightedLines[line.NewNum]
				} else {
					line.HTML = HighlightLine(line.Content, filename)
				}
			case "del":
				line.HTML = HighlightLine(line.Content, filename)
			}
		}
	}
}

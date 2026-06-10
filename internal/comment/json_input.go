package comment

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func readCommentJSONInput(path string, stdin io.Reader) ([]byte, error) {
	if path == "" || path == "-" {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return data, nil
}

func parseCommentJSONEntries(data []byte, source string) ([]BulkCommentEntry, error) {
	var entries []BulkCommentEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, formatJSONParseError(data, source, err)
	}
	return entries, nil
}

func formatJSONParseError(data []byte, source string, err error) error {
	label := jsonSourceLabel(source)
	offset, hasOffset := jsonErrorOffset(err)
	if !hasOffset {
		return fmt.Errorf("error parsing JSON from %s: %w", label, err)
	}
	line, col := lineColForOffset(data, offset)
	snippet := jsonSnippet(data, offset)
	return fmt.Errorf("error parsing JSON from %s at byte %d (line %d, column %d):\n  %s\n  %w",
		label, offset, line, col, snippet, err)
}

func jsonSourceLabel(source string) string {
	if source == "" || source == "-" {
		return "stdin"
	}
	return source
}

func jsonErrorOffset(err error) (int64, bool) {
	var syn *json.SyntaxError
	if errors.As(err, &syn) {
		return syn.Offset, true
	}
	var typ *json.UnmarshalTypeError
	if errors.As(err, &typ) {
		return typ.Offset, true
	}
	return 0, false
}

func lineColForOffset(data []byte, offset int64) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if int(offset) > len(data) {
		offset = int64(len(data))
	}
	line, col := 1, 1
	for i := int64(0); i < offset; i++ {
		if data[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return line, col
}

func jsonSnippet(data []byte, offset int64) string {
	const before, after = 40, 40
	if offset < 0 {
		offset = 0
	}
	if int(offset) > len(data) {
		offset = int64(len(data))
	}
	start := offset - before
	if start < 0 {
		start = 0
	}
	end := offset + after
	if int(end) > len(data) {
		end = int64(len(data))
	}
	left := visibleControl(data[start:offset])
	right := visibleControl(data[offset:end])
	prefix := ""
	if start > 0 {
		prefix = "..."
	}
	suffix := ""
	if int(end) < len(data) {
		suffix = "..."
	}
	return prefix + left + ">>>HERE<<<" + right + suffix
}

func visibleControl(b []byte) string {
	var out strings.Builder
	out.Grow(len(b))
	for _, c := range b {
		switch c {
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}

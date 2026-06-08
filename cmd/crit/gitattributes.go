package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// generatedRule is one parsed line from .gitattributes that mentions
// linguist-generated. negate=true means "-linguist-generated" (un-mark).
type generatedRule struct {
	pattern string
	negate  bool
	re      *regexp.Regexp
}

// parseGeneratedRules reads <repoRoot>/.gitattributes and returns the rules
// that mention linguist-generated. A missing file returns an empty slice with
// no error. Only the top-level .gitattributes is consulted (no nested files,
// per MVP scope of issue #503).
func parseGeneratedRules(repoRoot string) ([]generatedRule, error) {
	if repoRoot == "" {
		return nil, nil
	}
	path := filepath.Join(repoRoot, ".gitattributes")
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	var rules []generatedRule
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		rule, ok := parseGeneratedRuleLine(scanner.Text())
		if !ok {
			continue
		}
		rules = append(rules, rule)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return rules, nil
}

// parseGeneratedRuleLine parses one .gitattributes line. Returns ok=false for
// blank/comment lines or lines that do not mention linguist-generated.
func parseGeneratedRuleLine(line string) (generatedRule, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return generatedRule{}, false
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return generatedRule{}, false
	}
	pattern := fields[0]
	negate := false
	matched := false
	for _, attr := range fields[1:] {
		switch attr {
		case "linguist-generated", "linguist-generated=true":
			matched = true
			negate = false
		case "-linguist-generated", "linguist-generated=false":
			matched = true
			negate = true
		}
	}
	if !matched {
		return generatedRule{}, false
	}
	re, err := compileGitattrPattern(pattern)
	if err != nil {
		return generatedRule{}, false
	}
	return generatedRule{pattern: pattern, negate: negate, re: re}, true
}

// compileGitattrPattern translates a gitignore-style pattern into a regex
// anchored to the full repo-relative path. Supported syntax:
//   - **        — any number of path segments (including zero)
//   - *         — any run of non-slash chars
//   - ?         — single non-slash char
//   - dir/      — match anything under dir
//   - exact/path — match this exact path
//
// Unsupported (silently treated literally): character classes [...].
func compileGitattrPattern(pattern string) (*regexp.Regexp, error) {
	trailingSlash := strings.HasSuffix(pattern, "/")
	pat := strings.TrimSuffix(pattern, "/")

	// A pattern with no slash (other than a trailing one) matches by basename
	// at any depth, mirroring gitignore semantics.
	hasSlash := strings.Contains(pat, "/")

	var sb strings.Builder
	sb.WriteString("^")
	if !hasSlash {
		// match in any directory
		sb.WriteString("(?:.*/)?")
	}
	i := 0
	for i < len(pat) {
		c := pat[i]
		switch {
		case c == '*' && i+1 < len(pat) && pat[i+1] == '*':
			// ** — any number of path segments
			sb.WriteString(".*")
			i += 2
			// consume an optional following slash so "**/" doesn't force one
			if i < len(pat) && pat[i] == '/' {
				i++
			}
		case c == '*':
			sb.WriteString("[^/]*")
			i++
		case c == '?':
			sb.WriteString("[^/]")
			i++
		default:
			sb.WriteString(regexp.QuoteMeta(string(c)))
			i++
		}
	}
	if trailingSlash {
		sb.WriteString("(?:/.*)?")
	}
	sb.WriteString("$")
	return regexp.Compile(sb.String())
}

// isGenerated returns true if path matches at least one linguist-generated
// rule and is not subsequently un-marked by a negation. Last matching rule
// wins (gitattributes semantics).
func isGenerated(path string, rules []generatedRule) bool {
	if len(rules) == 0 {
		return false
	}
	path = filepath.ToSlash(path)
	generated := false
	for _, r := range rules {
		if r.re == nil {
			continue
		}
		if r.re.MatchString(path) {
			generated = !r.negate
		}
	}
	return generated
}

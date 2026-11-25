package vcs

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

// GeneratedRule is one parsed line from .gitattributes that mentions
// linguist-generated. negate=true means "-linguist-generated" (un-mark).
type GeneratedRule struct {
	pattern string
	negate  bool
	re      *regexp.Regexp
}

// ParseGeneratedRules reads <repoRoot>/.gitattributes and returns the rules
// that mention linguist-generated. A missing file returns an empty slice with
// no error. Only the top-level .gitattributes is consulted (no nested files,
// per MVP scope of issue #503).
func ParseGeneratedRules(repoRoot string) ([]GeneratedRule, error) {
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

	var rules []GeneratedRule
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
func parseGeneratedRuleLine(line string) (GeneratedRule, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return GeneratedRule{}, false
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return GeneratedRule{}, false
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
		return GeneratedRule{}, false
	}
	re, err := compileGitattrPattern(pattern)
	if err != nil {
		return GeneratedRule{}, false
	}
	return GeneratedRule{pattern: pattern, negate: negate, re: re}, true
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
		case c == '*':
			// Measure the run of consecutive asterisks. A run of two or more
			// is the recursive wildcard "**" only when it forms a complete
			// path segment — preceded by the start or a slash and followed by
			// the end or a slash. Any other consecutive asterisks are plain
			// single-star wildcards (gitignore semantics), so "a**/b" behaves
			// like "a*/b" and cannot cross into neighboring segments.
			j := i
			for j < len(pat) && pat[j] == '*' {
				j++
			}
			fullSegment := (i == 0 || pat[i-1] == '/') && (j == len(pat) || pat[j] == '/')
			if j-i >= 2 && fullSegment {
				if j < len(pat) && pat[j] == '/' {
					// "**/" — zero or more whole directories; the slash is
					// folded in so the zero-directory case needs no separator.
					sb.WriteString("(?:.*/)?")
					i = j + 1
				} else {
					// trailing "**" — everything below this point
					sb.WriteString(".*")
					i = j
				}
			} else {
				for range j - i {
					sb.WriteString("[^/]*")
				}
				i = j
			}
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

// IsGenerated returns true if path matches at least one linguist-generated
// rule and is not subsequently un-marked by a negation. Last matching rule
// wins (gitattributes semantics).
func IsGenerated(path string, rules []GeneratedRule) bool {
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

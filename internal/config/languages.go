package config

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// LanguageMarkdown is the `languages` value that reviews matching files as
// markdown documents (document view, inter-round document diff) instead of
// highlighted code.
const LanguageMarkdown = "markdown"

// LanguageText is the `languages` value that turns highlighting off for
// matching files. Shiki handles it without a grammar. "plaintext", "txt" and
// "plain" (Shiki's own names for it) are accepted as aliases.
const LanguageText = "text"

// LanguageAuto is the `languages` value that keeps Crit's own detection for
// matching files, so a project can switch off a global rule for a pattern.
const LanguageAuto = "auto"

// languageAliases maps accepted spellings to the value Crit stores.
var languageAliases = map[string]string{
	"md":        LanguageMarkdown,
	"plaintext": LanguageText,
	"txt":       LanguageText,
	"plain":     LanguageText,
}

// LanguageRules is the compiled `languages` config: file patterns mapped to
// the language Crit reviews matching files as. The zero value matches nothing.
type LanguageRules struct {
	rules []languageRule
}

type languageRule struct {
	pattern string // lowercased: rules match file names case-insensitively
	lang    string
}

// CompileLanguages validates a `languages` config map against the grammar
// names Crit ships (known) and returns rules ordered most specific first.
// Patterns use MatchPattern syntax and match case-insensitively, like Crit's
// own extension detection. A value must be "markdown" (or "md"), "text",
// "auto", or a known grammar name; every unknown value is reported in one
// error so a typo fails at startup instead of rendering as plain text.
func CompileLanguages(languages map[string]string, known []string) (LanguageRules, error) {
	if len(languages) == 0 {
		return LanguageRules{}, nil
	}
	knownSet := make(map[string]bool, len(known))
	for _, k := range known {
		knownSet[k] = true
	}
	var rules []languageRule
	var problems []string
	for pattern, value := range languages {
		lang := strings.ToLower(strings.TrimSpace(value))
		if alias, ok := languageAliases[lang]; ok {
			lang = alias
		}
		switch {
		case strings.TrimSpace(pattern) == "":
			problems = append(problems, fmt.Sprintf("empty pattern for %q", value))
		case !validPattern(pattern):
			problems = append(problems, fmt.Sprintf("%q: invalid pattern", pattern))
		case lang == "":
			problems = append(problems, fmt.Sprintf("%q: empty language", pattern))
		case lang != LanguageMarkdown && lang != LanguageText && lang != LanguageAuto && !knownSet[lang]:
			problems = append(problems, fmt.Sprintf("%q: unknown language %q%s", pattern, value, suggestLanguages(lang, known)))
		default:
			rules = append(rules, languageRule{pattern: strings.ToLower(pattern), lang: lang})
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return LanguageRules{}, fmt.Errorf("invalid languages config:\n  %s", strings.Join(problems, "\n  "))
	}
	// Most specific first: an exact name ("app.conf") or a directory
	// ("deploy/") beats any glob; then the longer pattern ("deploy/*.conf" over
	// "*/*.conf" over "*.conf"). Ties sort by pattern for a stable order.
	sort.Slice(rules, func(i, j int) bool {
		gi, gj := isGlob(rules[i].pattern), isGlob(rules[j].pattern)
		if gi != gj {
			return gj
		}
		if len(rules[i].pattern) != len(rules[j].pattern) {
			return len(rules[i].pattern) > len(rules[j].pattern)
		}
		return rules[i].pattern < rules[j].pattern
	})
	return LanguageRules{rules: rules}, nil
}

// isGlob reports whether a pattern has glob metacharacters.
func isGlob(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

// validPattern reports whether MatchPattern can use pattern: it drops
// filepath.Match's ErrBadPattern, so a malformed glob ("[abc") would
// otherwise compile and silently match nothing.
func validPattern(pattern string) bool {
	_, err := filepath.Match(strings.TrimSuffix(pattern, "/"), "x")
	return err == nil
}

// suggestLanguages lists up to five known names that start with the unknown
// name ("glimmer" → glimmer-js) or that it starts with ("yaml2" → yaml).
// Names under three characters are not suggested as prefixes, since "c" or
// "d" would match almost anything.
func suggestLanguages(lang string, known []string) string {
	var matches []string
	for _, k := range known {
		if strings.HasPrefix(k, lang) || (len(k) > 2 && strings.HasPrefix(lang, k)) {
			matches = append(matches, k)
			if len(matches) == 5 {
				break
			}
		}
	}
	if len(matches) == 0 {
		return ""
	}
	return " (did you mean " + strings.Join(matches, ", ") + "?)"
}

// match returns the language of the most specific rule matching path. An
// "auto" rule matches but reports no language, so detection applies.
func (r LanguageRules) match(path string) (string, bool) {
	if len(r.rules) == 0 {
		return "", false
	}
	lower := strings.ToLower(path)
	for _, rule := range r.rules {
		if MatchPattern(rule.pattern, lower) {
			return rule.lang, rule.lang != LanguageAuto
		}
	}
	return "", false
}

// FileType returns "markdown" or "code" for a path a rule matches, and false
// when no rule (or an "auto" rule) matches, so the caller keeps its own
// detection.
func (r LanguageRules) FileType(path string) (string, bool) {
	lang, ok := r.match(path)
	if !ok {
		return "", false
	}
	if lang == LanguageMarkdown {
		return "markdown", true
	}
	return "code", true
}

// Lang returns the highlighting language a rule sets for path, or "" when no
// rule matches or the rule makes the file a markdown document.
func (r LanguageRules) Lang(path string) string {
	lang, ok := r.match(path)
	if !ok || lang == LanguageMarkdown {
		return ""
	}
	return lang
}

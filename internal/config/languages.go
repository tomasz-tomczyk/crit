package config

import (
	"fmt"
	"sort"
	"strings"
)

// LanguageMarkdown is the `languages` value that reviews matching files as
// markdown documents (document view, inter-round document diff) instead of
// highlighted code.
const LanguageMarkdown = "markdown"

// LanguageText is the `languages` value that turns highlighting off for
// matching files. Shiki handles it without a grammar.
const LanguageText = "text"

// LanguageRules is the compiled `languages` config: file patterns mapped to
// the language Crit reviews matching files as. The zero value matches nothing.
type LanguageRules struct {
	rules []languageRule
}

type languageRule struct {
	pattern string
	lang    string
}

// CompileLanguages validates a `languages` config map against the grammar
// names Crit ships (known) and returns rules ordered most specific first.
// Patterns use MatchPattern syntax. A value must be "markdown" (or "md"),
// "text", or a known grammar name; every unknown value is reported in one
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
		if lang == "md" {
			lang = LanguageMarkdown
		}
		switch {
		case strings.TrimSpace(pattern) == "":
			problems = append(problems, fmt.Sprintf("empty pattern for %q", value))
		case lang == "":
			problems = append(problems, fmt.Sprintf("%q: empty language", pattern))
		case lang != LanguageMarkdown && lang != LanguageText && !knownSet[lang]:
			problems = append(problems, fmt.Sprintf("%q: unknown language %q%s", pattern, value, suggestLanguages(lang, known)))
		default:
			rules = append(rules, languageRule{pattern: pattern, lang: lang})
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return LanguageRules{}, fmt.Errorf("invalid languages config:\n  %s", strings.Join(problems, "\n  "))
	}
	// Most specific first: a longer pattern names files more precisely
	// ("deploy/*.conf" over "*.conf"). Ties sort by pattern for a stable order.
	sort.Slice(rules, func(i, j int) bool {
		if len(rules[i].pattern) != len(rules[j].pattern) {
			return len(rules[i].pattern) > len(rules[j].pattern)
		}
		return rules[i].pattern < rules[j].pattern
	})
	return LanguageRules{rules: rules}, nil
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

func (r LanguageRules) match(path string) (string, bool) {
	for _, rule := range r.rules {
		if MatchPattern(rule.pattern, path) {
			return rule.lang, true
		}
	}
	return "", false
}

// FileType returns "markdown" or "code" for a path a rule matches, and false
// when no rule matches (the caller keeps its own detection).
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
	lang, _ := r.match(path)
	if lang == LanguageMarkdown {
		return ""
	}
	return lang
}

package story

import (
	"fmt"
	"strings"
)

// BuildStoryPrompt assembles the final prompt string handed to agent_cmd.
//
// It is prompt-by-reference (spec §4.3): the guide already carries the prep
// file's PATH and instructs the agent to READ it — the diff is never inlined
// here. guide is the fully-resolved on_story_generate template (interpolated
// with the StoryContext variables by the caller); schemaJSON is the JSON shape
// the agent must emit. retryFeedback, when non-empty, is the parse error from a
// prior attempt plus the strict-output reminder, appended so the second (and
// only) attempt can self-correct.
func BuildStoryPrompt(guide, schemaJSON, retryFeedback string) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(guide, "\n"))
	b.WriteString("\n\n---\n\n")
	b.WriteString("Emit a single JSON object matching this schema (no prose, no fences):\n\n")
	b.WriteString("```json\n")
	b.WriteString(schemaJSON)
	b.WriteString("\n```\n")
	if retryFeedback != "" {
		b.WriteString("\n")
		b.WriteString(retryFeedback)
		b.WriteString("\n")
	}
	return b.String()
}

// RetryFeedback is the message appended to the prompt on the single retry after
// a parse failure (spec §4.3): the parse error plus a strict-output reminder.
func RetryFeedback(parseErr error) string {
	return fmt.Sprintf(
		"Your previous output could not be parsed as JSON: %v\nOutput raw JSON only — no prose, no fences.",
		parseErr,
	)
}

// ExtractJSON pulls the JSON object candidate out of agent_cmd's stdout
// (spec §4.3): if the output is wrapped in a single ```json / ``` (or bare
// ```) fence pair, strip it; otherwise take the substring from the first "{"
// to the last "}". The returned string is the candidate for a strict
// json.Unmarshal — this function does not itself validate JSON.
func ExtractJSON(out string) string {
	trimmed := strings.TrimSpace(out)

	if fenced, ok := stripFence(trimmed); ok {
		return strings.TrimSpace(fenced)
	}

	first := strings.IndexByte(trimmed, '{')
	last := strings.LastIndexByte(trimmed, '}')
	if first >= 0 && last > first {
		return trimmed[first : last+1]
	}
	return trimmed
}

// stripFence removes a single leading ```json (or ```) fence and its matching
// trailing ``` if the text is fenced. Returns the inner content and true when a
// fence pair was found, else ("", false).
func stripFence(s string) (string, bool) {
	if !strings.HasPrefix(s, "```") {
		return "", false
	}
	// Drop the opening fence line (```json, ```JSON, or bare ```).
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		return "", false
	}
	rest := s[nl+1:]
	// Drop the closing fence: everything up to the last ``` .
	closeIdx := strings.LastIndex(rest, "```")
	if closeIdx < 0 {
		return "", false
	}
	return rest[:closeIdx], true
}

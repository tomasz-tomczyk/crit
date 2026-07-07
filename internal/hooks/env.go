package hooks

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/prompt"
)

// EnvPrefix is the namespace for hook environment variables. Crit sets
// CRIT_REVIEW_PATH, CRIT_SESSION_KEY, CRIT_MODE, CRIT_APPROVED, etc.
const EnvPrefix = "CRIT_"

// EnvMap converts a finish prompt.Context to CRIT_-prefixed environment
// variables available to a hook. Numbers become their decimal strings; lists
// become newline-joined strings (and also a _count var); JSON payload vars
// (comments_unresolved_json, comments_json) are exposed verbatim.
func EnvMap(c prompt.Context) map[string]string {
	m := map[string]string{
		EnvPrefix + "REVIEW_PATH":               c.ReviewPath,
		EnvPrefix + "COMMENTS_CMD":              c.CommentsCmd,
		EnvPrefix + "COMMENTS_ALL_CMD":          c.CommentsAllCmd,
		EnvPrefix + "NEXT_ROUND_CMD":            c.NextRoundCmd,
		EnvPrefix + "SESSION_KEY":               c.SessionKey,
		EnvPrefix + "MODE":                      c.Mode,
		EnvPrefix + "UNRESOLVED_COUNT":          strconv.Itoa(c.UnresolvedCount),
		EnvPrefix + "TOTAL_COUNT":               strconv.Itoa(c.TotalCount),
		EnvPrefix + "APPROVED":                  boolStr(c.Approved),
		EnvPrefix + "PLAN_SLUG":                 c.PlanSlug,
		EnvPrefix + "INTERNAL_SESSION_MODE":     c.InternalSessionMode,
		EnvPrefix + "FILES_WITH_COMMENTS":       joinNewline(c.FilesWithComments),
		EnvPrefix + "FILES_WITH_COMMENTS_COUNT": strconv.Itoa(len(c.FilesWithComments)),
		EnvPrefix + "COMMENTS_UNRESOLVED_JSON":  c.CommentsUnresolvedJSON,
		EnvPrefix + "COMMENTS_JSON":             c.CommentsJSON,
	}
	if c.SessionStats != nil {
		m[EnvPrefix+"SESSION_DURATION_SECONDS"] = strconv.Itoa(c.SessionStats.DurationSeconds)
		m[EnvPrefix+"SESSION_FILES_REVIEWED"] = strconv.Itoa(c.SessionStats.FilesReviewed)
		m[EnvPrefix+"SESSION_COMMENTS_SUBMITTED"] = strconv.Itoa(c.SessionStats.CommentsSubmitted)
	}
	return m
}

// JSONPayload serializes the finish context as JSON for the hook's stdin.
// It mirrors the prompt template variables (snake_case) plus an explicit
// files_with_comments array and session_stats object.
func JSONPayload(c prompt.Context) []byte {
	payload := map[string]any{
		"review_path":              c.ReviewPath,
		"comments_cmd":             c.CommentsCmd,
		"comments_all_cmd":         c.CommentsAllCmd,
		"next_round_cmd":           c.NextRoundCmd,
		"session_key":              c.SessionKey,
		"mode":                     c.Mode,
		"unresolved_count":         c.UnresolvedCount,
		"total_count":              c.TotalCount,
		"approved":                 c.Approved,
		"plan_slug":                c.PlanSlug,
		"internal_session_mode":    c.InternalSessionMode,
		"files_with_comments":      c.FilesWithComments,
		"comments_unresolved_json": json.RawMessage(nilOrJSON(c.CommentsUnresolvedJSON)),
		"comments_json":            json.RawMessage(nilOrJSON(c.CommentsJSON)),
	}
	if c.SessionStats != nil {
		payload["session_stats"] = map[string]any{
			"duration_seconds":   c.SessionStats.DurationSeconds,
			"files_reviewed":     c.SessionStats.FilesReviewed,
			"comments_submitted": c.SessionStats.CommentsSubmitted,
		}
	}
	b, _ := json.MarshalIndent(payload, "", "  ")
	return b
}

// nilOrJSON returns a non-null JSON fragment; empty input becomes `[]` so hooks
// parsing with jq/awk always see an array.
func nilOrJSON(s string) json.RawMessage {
	if s == "" {
		return json.RawMessage("[]")
	}
	return json.RawMessage(s)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func joinNewline(s []string) string {
	return strings.Join(s, "\n")
}

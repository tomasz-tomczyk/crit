package prompt

// SessionStats mirrors finish JSON session_stats for templates.
type SessionStats struct {
	DurationSeconds   int
	FilesReviewed     int
	CommentsSubmitted int
}

// Context is the template variable object for finish hooks.
type Context struct {
	ReviewPath             string
	CommentsCmd            string
	CommentsAllCmd         string
	NextRoundCmd           string
	SessionKey             string
	Mode                   string // files | diff | live | preview
	UnresolvedCount        int
	TotalCount             int
	FilesWithComments      []string
	SessionStats           *SessionStats
	PlanSlug               string
	CommentsUnresolvedJSON string // unresolved threads only
	CommentsJSON           string // all comments in the session
	Approved               bool
	InternalSessionMode    string // files | git | plan — for default action builders
}

// TemplateData returns a map with snake_case keys for text/template.
func (c Context) TemplateData() map[string]any {
	data := map[string]any{
		"review_path":              c.ReviewPath,
		"comments_cmd":             c.CommentsCmd,
		"comments_all_cmd":         c.CommentsAllCmd,
		"next_round_cmd":           c.NextRoundCmd,
		"session_key":              c.SessionKey,
		"mode":                     c.Mode,
		"unresolved_count":         c.UnresolvedCount,
		"total_count":              c.TotalCount,
		"files_with_comments":      c.FilesWithComments,
		"plan_slug":                c.PlanSlug,
		"comments_unresolved_json": c.CommentsUnresolvedJSON,
		"comments_json":            c.CommentsJSON,
		"approved":                 c.Approved,
		"internal_session_mode":    c.InternalSessionMode,
	}
	if c.SessionStats != nil {
		data["session_stats"] = map[string]any{
			"duration":           c.SessionStats.DurationSeconds,
			"duration_seconds":   c.SessionStats.DurationSeconds,
			"files_reviewed":     c.SessionStats.FilesReviewed,
			"comments_submitted": c.SessionStats.CommentsSubmitted,
		}
	}
	return data
}

// Meta describes which hook and template produced a rendered prompt.
type Meta struct {
	Hook           string `json:"hook"`
	TemplateSource string `json:"template_source"`
}

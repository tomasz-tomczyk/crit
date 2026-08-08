package prompt

// StoryContext is the template variable object for the on_story_generate hook.
// Story is diff-scoped only (no :mode split — see hooks.go), so there is no
// InternalSessionMode/Mode field here.
type StoryContext struct {
	PrepPath        string // path to the prep file on disk; agent must READ it, diff is never inlined
	StorySchemaJSON string // JSON schema for the Story struct, embedded at the end of the prompt
	CommitMessages  string // `git log --oneline` over the diff scope
	DiffScopeKind   string // "committed" | "workingTree"
	BaseSHA         string
	HeadSHA         string
	MergeBaseSHA    string
	PRNumber        string
	PRURL           string
	PRTitle         string
	PRBody          string
	MRNumber        string
	MRURL           string
	SessionKey      string
	ReviewPath      string
}

// TemplateData returns a map with snake_case keys for text/template.
func (c StoryContext) TemplateData() map[string]any {
	return map[string]any{
		"prep_path":         c.PrepPath,
		"story_schema_json": c.StorySchemaJSON,
		"commit_messages":   c.CommitMessages,
		"diff_scope_kind":   c.DiffScopeKind,
		"base_sha":          c.BaseSHA,
		"head_sha":          c.HeadSHA,
		"merge_base_sha":    c.MergeBaseSHA,
		"pr_number":         c.PRNumber,
		"pr_url":            c.PRURL,
		"pr_title":          c.PRTitle,
		"pr_body":           c.PRBody,
		"mr_number":         c.MRNumber,
		"mr_url":            c.MRURL,
		"session_key":       c.SessionKey,
		"review_path":       c.ReviewPath,
	}
}

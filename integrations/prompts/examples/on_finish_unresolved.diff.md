{{if gt .unresolved_count 10}}
This review has {{.unresolved_count}} comments across {{len .files_with_comments}} files.

{{if .comments_unresolved_json}}{{.comments_unresolved_json}}

{{end}}1. Group by topic and spawn one subagent per group.
2. Reply with `crit comment --reply-to <id> --author <name> "..."`
3. When done: `{{.next_round_cmd}}`
{{else}}
{{if eq .unresolved_count 1}}The review finished with 1 unresolved comment.{{else}}The review finished with {{.unresolved_count}} unresolved comments.{{end}}

{{if .comments_unresolved_json}}{{.comments_unresolved_json}}

{{end}}Address each comment. For each one, reply explaining what you did using `crit comment --reply-to <comment-id> --author <your-name> "<explanation>"`. When done run: `{{.next_round_cmd}}`
{{end}}

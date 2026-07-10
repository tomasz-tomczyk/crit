{{if eq .unresolved_count 1}}The story-mode review finished with 1 unresolved comment.{{else}}The story-mode review finished with {{.unresolved_count}} unresolved comments.{{end}}

Story mode is an editorial view over the underlying diff. Address the comments against the actual source files. Do not edit, regenerate, or patch the saved story JSON unless the user explicitly asks for that.

{{if .comments_unresolved_json}}{{.comments_unresolved_json}}

{{end}}{{if eq .internal_session_mode "plan"}}Revise the plan to address each comment. To reply to comments, use `crit comment --plan {{.plan_slug}} --reply-to <id> --author <your-name> "<explanation>"`.{{else}}For each comment, make any needed code edit and reply explaining what you did using `crit comment --reply-to <comment-id> --author <your-name> "<explanation>"`.{{end}}{{if .next_round_cmd}}

When you're done, run:

  {{.next_round_cmd}}{{end}}

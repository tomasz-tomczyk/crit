package comment

import (
	"github.com/tomasz-tomczyk/crit/internal/clicmd"
)

type commentFlags struct {
	outputDir string
	author    string
	userID    string
	replyTo   string
	resolve   bool
	path      string
	json      bool
	file      string
	plan      string
	scope     CommentFocusOverride
	args      []string
}

func parseCommentFlags(args []string) (commentFlags, error) { //nolint:gocyclo // CLI flag parser
	var f commentFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--plan":
			val, err := clicmd.RequireFlagValue(args, i, "--plan")
			if err != nil {
				return f, err
			}
			f.plan = val
			i++
		case "--output", "-o":
			val, err := clicmd.RequireFlagValue(args, i, arg)
			if err != nil {
				return f, err
			}
			f.outputDir = val
			i++
		case "--author":
			val, err := clicmd.RequireFlagValue(args, i, "--author")
			if err != nil {
				return f, err
			}
			f.author = val
			i++
		case "--reply-to":
			val, err := clicmd.RequireFlagValue(args, i, "--reply-to")
			if err != nil {
				return f, err
			}
			f.replyTo = val
			i++
		case "--resolve":
			f.resolve = true
		case "--path":
			val, err := clicmd.RequireFlagValue(args, i, "--path")
			if err != nil {
				return f, err
			}
			f.path = val
			i++
		case "--json":
			f.json = true
		case "--file", "-f":
			val, err := clicmd.RequireFlagValue(args, i, arg)
			if err != nil {
				return f, err
			}
			f.file = val
			i++
		case "--scope":
			raw, err := clicmd.RequireFlagValue(args, i, "--scope")
			if err != nil {
				return f, err
			}
			override, err := CommentScopeOverrideFromFlag(raw)
			if err != nil {
				return f, err
			}
			f.scope = override
			i++
		default:
			f.args = append(f.args, arg)
		}
	}
	return f, nil
}

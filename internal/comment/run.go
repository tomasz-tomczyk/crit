package comment

import (
	"fmt"
	"os"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

// RunComment is the crit comment subcommand implementation.
func RunComment(args []string) error { //nolint:gocyclo // CLI dispatcher
	f, err := parseCommentFlags(args)
	if err != nil {
		return err
	}
	if err := resolveCommentFlags(&f); err != nil {
		return err
	}

	scope, err := ResolveCommentScope(f.scope, f.outputDir)
	if err != nil {
		return err
	}

	if f.json {
		return runCommentJSONScoped(f, scope)
	}

	if f.replyTo != "" {
		return runCommentReply(f)
	}

	if len(f.args) >= 1 && f.args[0] == "--clear" {
		return runCommentClear(f.outputDir)
	}

	if len(f.args) < 1 {
		return commentUsageError()
	}

	if len(f.args) == 1 {
		body := f.args[0]
		if err := addReviewCommentToCritJSONScoped(body, f.author, f.userID, f.outputDir, scope); err != nil {
			return err
		}
		fmt.Println("Added review comment")
		return nil
	}

	loc := f.args[0]
	colonIdx := strings.LastIndex(loc, ":")
	if colonIdx > 0 && looksLikeLineSpec(loc[colonIdx+1:]) {
		return runCommentLineLevelScoped(loc, f.args, f.author, f.userID, f.outputDir, scope)
	}

	if len(f.args) >= 2 {
		candidatePath := f.args[0]
		if fileExistsOnDiskOrSession(candidatePath, f.outputDir) {
			body := strings.Join(f.args[1:], " ")
			if err := addFileCommentToCritJSONScoped(candidatePath, body, f.author, f.userID, f.outputDir, scope); err != nil {
				return err
			}
			fmt.Printf("Added file comment on %s\n", candidatePath)
			return nil
		}
	}

	if colonIdx < 0 {
		return fmt.Errorf("invalid location %q — expected <path>:<line[-end]>, or a valid file path for file-level comments", loc)
	}
	return fmt.Errorf("invalid line spec in %q", loc)
}

func resolveCommentFlags(f *commentFlags) error {
	if f.plan != "" {
		if f.outputDir != "" {
			return fmt.Errorf("--plan and --output cannot be used together")
		}
		var planDirErr error
		f.outputDir, planDirErr = session.PlanStorageDir(session.Slugify(f.plan))
		if planDirErr != nil {
			return planDirErr
		}
	}

	cfgDir, err := os.Getwd()
	if err != nil {
		return err
	}
	if v := vcs.DetectVCS(""); v != nil {
		if root, rootErr := v.RepoRoot(); rootErr == nil {
			cfgDir = root
		}
	}
	cfg := config.LoadConfig(cfgDir)
	if f.author == "" {
		f.author = cfg.Author
	}
	if f.userID == "" {
		f.userID = cfg.AuthUserID
	}
	return nil
}

func runCommentJSONScoped(f commentFlags, scope session.InheritedScope) error {
	data, err := readCommentJSONInput(f.file, os.Stdin)
	if err != nil {
		return err
	}

	entries, err := parseCommentJSONEntries(data, f.file)
	if err != nil {
		return err
	}

	if err := bulkAddCommentsToCritJSONScoped(entries, f.author, f.userID, f.outputDir, scope); err != nil {
		return err
	}

	var comments, replies int
	for _, e := range entries {
		if e.ReplyTo != "" {
			replies++
		} else {
			comments++
		}
	}

	var parts []string
	if comments > 0 {
		parts = append(parts, fmt.Sprintf("%d comment%s", comments, plural(comments)))
	}
	if replies > 0 {
		word := "replies"
		if replies == 1 {
			word = "reply"
		}
		parts = append(parts, fmt.Sprintf("%d %s", replies, word))
	}
	fmt.Printf("Added %s\n", strings.Join(parts, " and "))
	return nil
}

func runCommentReply(f commentFlags) error {
	if len(f.args) < 1 {
		return clicmd.Usage("Usage: crit comment --reply-to <comment-id> [--resolve] <body>")
	}
	replyBody := strings.Join(f.args, " ")
	if err := addReplyToCritJSON(f.replyTo, replyBody, f.author, f.userID, f.resolve, f.outputDir, f.path); err != nil {
		return err
	}
	if f.resolve {
		fmt.Printf("Replied to %s and marked resolved\n", f.replyTo)
	} else {
		fmt.Printf("Replied to %s\n", f.replyTo)
	}
	return nil
}

func runCommentClear(outputDir string) error {
	if err := review.ClearCritJSON(outputDir); err != nil {
		return err
	}
	fmt.Println("Cleared review file")
	return nil
}

func commentUsageError() error {
	fmt.Fprintln(os.Stderr, "Usage: crit comment [--output <dir>] [--author <name>] <body>                    Review-level comment")
	fmt.Fprintln(os.Stderr, "       crit comment [--output <dir>] [--author <name>] <path> <body>             File-level comment")
	fmt.Fprintln(os.Stderr, "       crit comment [--output <dir>] [--author <name>] <path>:<line[-end]> <body> Line-level comment")
	fmt.Fprintln(os.Stderr, "       crit comment --reply-to <id> [--resolve] [--author <name>] <body>")
	fmt.Fprintln(os.Stderr, "       crit comment --json [--file <path>] [--author <name>] [--output <dir>]")
	fmt.Fprintln(os.Stderr, "                                                                  Bulk add comments from JSON (stdin by default; --file <path> or --file - for stdin)")
	fmt.Fprintln(os.Stderr, "       crit comment [--output <dir>] --clear")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Examples:")
	fmt.Fprintln(os.Stderr, "  crit comment --author 'Claude' 'Overall this looks good'")
	fmt.Fprintln(os.Stderr, "  crit comment --author 'Claude' src/auth.go 'Restructure this file'")
	fmt.Fprintln(os.Stderr, "  crit comment --author 'Claude' main.go:42 'Fix this bug'")
	fmt.Fprintln(os.Stderr, "  crit comment --author 'Claude' src/auth.go:10-25 'This block needs refactoring'")
	fmt.Fprintln(os.Stderr, "  crit comment --reply-to c_a3f8b2 --resolve --author 'Claude' 'Split into two functions'")
	fmt.Fprintln(os.Stderr, "  crit comment --output /tmp/reviews main.go:42 'Fix this bug'")
	fmt.Fprintln(os.Stderr, "  echo '[{\"file\":\"main.go\",\"line\":42,\"body\":\"Fix this\"}]' | crit comment --json --author 'Claude'")
	fmt.Fprintln(os.Stderr, "  crit comment --json --file comments.json --author 'Claude'")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Tips:")
	fmt.Fprintln(os.Stderr, "  Use --author to identify who left the comment (recommended for AI agents)")
	fmt.Fprintln(os.Stderr, "  Use single quotes for the body to avoid shell interpretation of backticks")
	fmt.Fprintln(os.Stderr, "  Use --json for bulk operations (multiple comments/replies in one atomic write)")
	return clicmd.Usage("invalid comment usage")
}

func runCommentLineLevelScoped(loc string, commentArgs []string, author, userID, outputDir string, scope session.InheritedScope) error {
	colonIdx := strings.LastIndex(loc, ":")
	lineSpec := loc[colonIdx+1:]
	filePath := loc[:colonIdx]
	startLine, endLine, err := parseLineSpec(lineSpec)
	if err != nil {
		return fmt.Errorf("invalid line spec in %q", loc)
	}
	body := strings.Join(commentArgs[1:], " ")
	if critPath, pathErr := review.ResolveReviewPath(outputDir); pathErr == nil {
		if guardErr := checkCommentCLIAllowed(critPath); guardErr != nil {
			return guardErr
		}
	}
	if err := addCommentToCritJSONScoped(filePath, startLine, endLine, body, author, userID, outputDir, scope); err != nil {
		return err
	}
	fmt.Printf("Added comment on %s:%s\n", filePath, lineSpec)
	return nil
}

func looksLikeLineSpec(s string) bool {
	if s == "" {
		return false
	}
	if dashIdx := strings.Index(s, "-"); dashIdx >= 0 {
		_, _, err1 := parseLineSpec(s[:dashIdx])
		_, _, err2 := parseLineSpec(s[dashIdx+1:])
		return err1 == nil && err2 == nil
	}
	_, _, err := parseLineSpec(s)
	return err == nil
}

func fileExistsOnDiskOrSession(path string, outputDir string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	critPath, err := review.ResolveReviewPath(outputDir)
	if err != nil {
		return false
	}
	cj, err := review.LoadCritJSON(critPath)
	if err != nil {
		return false
	}
	_, ok := cj.Files[path]
	return ok
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

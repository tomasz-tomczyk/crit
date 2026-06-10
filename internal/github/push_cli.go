package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/share"
)

type pushFlags struct {
	prFlag    int
	dryRun    bool
	message   string
	outputDir string
	eventFlag string
}

func parsePushFlags(args []string) (pushFlags, error) {
	var f pushFlags
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--dry-run" {
			f.dryRun = true
			continue
		}
		if arg == "--message" || arg == "-m" {
			if i+1 >= len(args) {
				return f, fmt.Errorf("--message requires a value")
			}
			i++
			f.message = args[i]
			continue
		}
		if arg == "--output" || arg == "-o" {
			if i+1 >= len(args) {
				return f, fmt.Errorf("--output requires a value")
			}
			i++
			f.outputDir = args[i]
			continue
		}
		if arg == "--event" || arg == "-e" {
			if i+1 >= len(args) {
				return f, fmt.Errorf("--event requires a value (comment, approve, request-changes)")
			}
			i++
			f.eventFlag = args[i]
			continue
		}
		n, err := strconv.Atoi(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Usage: crit push [--dry-run] [--event <type>] [--message <msg>] [--output <dir>] [pr-number]\n")
			return f, clicmd.ExitError{Code: 1, Err: errors.New("exit")}
		}
		f.prFlag = n
	}
	return f, nil
}

// postPushReplies posts each reply via `gh api`. On the first auth-rotation
// failure (HTTP 401) it aborts the rest of the batch — every subsequent
// call would fail identically, and bailing cleanly lets the outer push
// loop print the K-of-N recovery message. authFailed signals that to the
// caller. replyCount is the number of replies actually accepted by GitHub
// before the abort (or in total if no abort happened).
// resolveCurrentPRHead fetches the PR's current head SHA when in range mode.
// Returns "" silently when not in range mode or on tolerated fetch failure
// (dry-run); returns an error when fetching is required but failed.
//
// On dry-run with a fetch error, surfaces a stderr note so the user knows the
// stale-head check was skipped — silent skipping makes the dry-run plan
// misleading.
func resolveCurrentPRHead(prNumber int, inRange, dryRun bool) (string, error) {
	if !inRange {
		return "", nil
	}
	info, err := FetchPRByNumber(prNumber)
	if err != nil {
		if dryRun {
			fmt.Fprintf(os.Stderr,
				"Note: could not resolve current PR #%d head; stale-head check not enforced in this dry-run: %v\n",
				prNumber, err)
			return "", nil
		}
		return "", fmt.Errorf("fetching PR #%d for stale-head check: %w", prNumber, err)
	}
	if info == nil {
		return "", nil
	}
	return info.HeadRefOid, nil
}

// pushContext captures everything runPush needs after parsing flags +
// loading the review file. Splitting this out keeps runPush itself short
// and the cyclomatic complexity inside Go Report Card limits.
type pushContext struct {
	flags    pushFlags
	event    string
	prNumber int
	critPath string
	cj       session.CritJSON
}

// loadPushContext parses flags, validates them, resolves the PR number, and
// reads + parses the review file.
func loadPushContext(args []string) (pushContext, error) {
	if err := RequireGH(); err != nil {
		return pushContext{}, err
	}

	f, err := parsePushFlags(args)
	if err != nil {
		return pushContext{}, err
	}

	event, err := ParsePushEvent(f.eventFlag)
	if err != nil {
		return pushContext{}, err
	}
	if event == "REQUEST_CHANGES" && f.message == "" {
		fmt.Fprintln(os.Stderr, "Error: --event request-changes requires --message")
		return pushContext{}, clicmd.ExitError{Code: 1, Err: errors.New("--event request-changes requires --message")}
	}

	prNumber, err := DetectPR(f.prFlag)
	if err != nil {
		return pushContext{}, err
	}

	critPath, err := review.ResolveReviewPath(f.outputDir)
	if err != nil {
		return pushContext{}, err
	}

	// Read the cwd-resolved file first (best-effort) so we know its branch.
	// We tolerate "not found" here so an explicit `--pr N` from a clean
	// checkout can still find the right file by branch via the redirect.
	var cj session.CritJSON
	cwdFileExists := true
	data, readErr := session.ReadFileShared(session.ReviewPathsFor(critPath).Review)
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			return pushContext{}, readErr
		}
		cwdFileExists = false
	} else if err := json.Unmarshal(data, &cj); err != nil {
		return pushContext{}, fmt.Errorf("invalid review file: %w", err)
	}

	// Redirect when the user passed an explicit PR number and the cwd-resolved
	// review file is for a different branch (or is missing) — pushing the wrong
	// comments to a PR is destructive, so honor the explicit intent first. Same
	// pattern as PR #424's findReviewFileByCommentID fallback for `crit comment`.
	if f.prFlag != 0 && f.outputDir == "" {
		if altPath, altCJ, ok := review.RedirectReviewPathForPR(prNumber, cj.Branch, critPath); ok {
			critPath = altPath
			cj = altCJ
			cwdFileExists = true
		}
	}

	if !cwdFileExists {
		return pushContext{}, fmt.Errorf("no review file found. Run a crit review first")
	}

	return pushContext{flags: f, event: event, prNumber: prNumber, critPath: critPath, cj: cj}, nil
}

// runPushDryRun prints the bucket plan to stdout and returns. Does not write
// the export file — dry-run is read-only by definition.
func runPushDryRun(ctx pushContext, b PushBuckets) {
	fmt.Println(SummarizeBuckets(ctx.prNumber, b))
	fmt.Println()
	fmt.Print(DetailedDryRun(b))
	fmt.Printf("Use `crit push --pr %d` to confirm.\n", ctx.prNumber)
}

// runPushLive performs the actual push: writes the orphan export (if any
// orphans), posts the postable bucket via gh, and prints a summary. Replies
// to existing GitHub comments are also posted (only for postable parents).
//
// Returns the process exit code so callers (and tests) can decide what to
// do without runPushLive itself terminating the process.
func RunPushLive(ctx pushContext, b PushBuckets) int { //nolint:gocyclo // CLI push flow
	exportPath := writePushOrphanExport(ctx, b)

	rewrite := DefaultStripBodyRewriter

	postable := len(b.Postable)
	posted, postFailed, postAuthFailed, commentIDs := runPushPostReview(ctx, b, rewrite)

	totalReplies := countNewReplies(ctx.cj)
	postedReplies := 0
	replyAuthFailed := false
	if !postFailed && !postAuthFailed {
		var allReplies []GhReplyForPush
		for _, cf := range ctx.cj.Files {
			allReplies = append(allReplies, CollectNewRepliesForPush(cf, rewrite)...)
		}
		var replyIDs map[ReplyKey]int64
		replyIDs, postedReplies, replyAuthFailed = PostPushReplies(ctx.prNumber, allReplies)
		if len(commentIDs) > 0 || len(replyIDs) > 0 {
			if uerr := UpdateCritJSONWithGitHubIDs(ctx.critPath, commentIDs, replyIDs); uerr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update review file with GitHub IDs: %v\n", uerr)
			}
		}
	}

	totalEdits := len(CollectEditedForPush(ctx.cj))
	patched := 0
	editAuthFailed := false
	if !postAuthFailed && !replyAuthFailed {
		patched, editAuthFailed = pushEditedBodies(ctx)
	}

	totalDeletes := len(CollectDeletesForPush(ctx.cj))
	deleted := 0
	deleteFailed := false
	deleteAuthFailed := false
	if !postAuthFailed && !replyAuthFailed && !editAuthFailed {
		deleted, deleteFailed, deleteAuthFailed = pushDeletedComments(ctx)
	}

	authAborted := postAuthFailed || replyAuthFailed || editAuthFailed || deleteAuthFailed
	if authAborted {
		k := posted + postedReplies + patched + deleted
		n := postable + totalReplies + totalEdits + totalDeletes
		fmt.Fprintf(os.Stderr,
			"Pushed %d of %d comments before auth failed. Run 'gh auth refresh' then re-run 'crit push' to post the rest.\n",
			k, n)
		return 1
	}

	printPushSummary(posted, patched, deleted, len(b.FullStack)+len(b.Unmapped), exportPath)

	if pushShouldExitFailure(posted, patched, deleted, exportPath, postFailed, deleteFailed) {
		return 1
	}
	return 0
}

// writePushOrphanExport writes the off-PR (full-stack + unmapped) bucket
// to a side file when needed. Split out of runPushLive purely to keep
// cyclomatic complexity inside Go Report Card limits.
func writePushOrphanExport(ctx pushContext, b PushBuckets) string {
	if len(b.FullStack)+len(b.Unmapped) == 0 {
		return ""
	}
	dir, err := ExportsDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not resolve export dir: %v\n", err)
		return ""
	}
	path, werr := WriteOrphanExport(ctx.prNumber, b, dir)
	if werr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write orphan export: %v\n", werr)
		return ""
	}
	return path
}

// runPushPostReview posts the Postable bucket as a single GitHub review.
// rewrite preprocesses each comment body before it ships (image upload+swap
// in production; nil falls back to strip-with-placeholder).
// Returns the count posted, whether the call failed (any reason), whether
// the failure was specifically an auth-rotation 401, and the path-to-id
// mapping for body-hash bookkeeping.
func runPushPostReview(ctx pushContext, b PushBuckets, rewrite BodyRewriter) (int, bool, bool, map[string]int64) {
	if len(b.Postable) == 0 {
		return 0, false, false, nil
	}
	ghComments := BucketsToGHComments(b.Postable, rewrite)
	ids, err := CreateGHReview(ctx.prNumber, ghComments, ctx.flags.message, ctx.event)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error posting review: %v\n", err)
		return 0, true, errors.Is(err, ErrGHAuthFailed), nil
	}
	return len(ghComments), false, false, ids
}

// countNewReplies counts replies that would be sent in this push (no
// GitHubID yet, parent already on GitHub). Mirrors collectNewRepliesForPush
// without allocating the full slice — used purely for the K-of-N total.
// nil rewriter is fine: the body content does not affect the count.
func countNewReplies(cj session.CritJSON) int {
	n := 0
	for _, cf := range cj.Files {
		n += len(CollectNewRepliesForPush(cf, nil))
	}
	return n
}

// pushShouldExitFailure encodes the exit-code policy for `crit push`. The
// process should fail (exit 1) only when nothing meaningful landed and at
// least one operation failed. Failed per-ID deletes stay in
// PendingGitHubDeletes for the next push (existing retry semantics), so a
// partial delete failure must not mask successful posts/patches/drains.
func pushShouldExitFailure(posted, patched, deleted int, exportPath string, postFailed, deleteFailed bool) bool {
	anySuccess := posted > 0 || patched > 0 || deleted > 0 || exportPath != ""
	anyFailure := postFailed || deleteFailed
	return anyFailure && !anySuccess
}

// printPushSummary writes the one-line stdout summary describing what
// happened. Adapts wording to the actual outcome (no orphans, no posts, etc).
func printPushSummary(posted, patched, deleted, orphans int, exportPath string) {
	if posted == 0 && patched == 0 && deleted == 0 && orphans == 0 {
		fmt.Println("No comments to push.")
		return
	}
	parts := []string{fmt.Sprintf("Posted %d comments", posted)}
	if patched > 0 {
		parts = append(parts, fmt.Sprintf("edited %d", patched))
	}
	if deleted > 0 {
		parts = append(parts, fmt.Sprintf("deleted %d", deleted))
	}
	line := strings.Join(parts, ", ") + "."
	if exportPath != "" {
		line += fmt.Sprintf(" %d comments exported to %s.", orphans, exportPath)
	}
	fmt.Println(line)
}

// pushEditedBodies PATCHes already-pushed comments/replies whose local body
// diverged from the recorded push-time hash. Returns the count of records
// successfully PATCHed and updated in the review file, plus an authFailed
// flag when an HTTP 401 aborted the batch. Non-auth failures log to stderr
// and are excluded from the count, so the next push will retry them.
func pushEditedBodies(ctx pushContext) (int, bool) {
	edits := CollectEditedForPush(ctx.cj)
	if len(edits) == 0 {
		return 0, false
	}
	succeeded := make([]GhEditForPush, 0, len(edits))
	authFailed := false
	for _, e := range edits {
		if err := PatchGHComment(e.GitHubID, e.Body); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to edit comment %d: %v\n", e.GitHubID, err)
			if errors.Is(err, ErrGHAuthFailed) {
				authFailed = true
				break
			}
			continue
		}
		succeeded = append(succeeded, e)
	}
	if uerr := UpdateCritJSONWithEditedBodies(ctx.critPath, succeeded); uerr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update review file after edit push: %v\n", uerr)
	}
	return len(succeeded), authFailed
}

// pushDeletedComments issues DELETE for every GitHub comment ID queued in
// PendingGitHubDeletes. Returns the count of IDs whose DELETE was drained
// (200 / 204, plus 404 "already gone" and 403 "not the author") and whether
// any DELETE returned an error severe enough to surface a non-zero exit.
//
// 403 is logged but treated as drained — the GitHub API rejects deletes by
// non-authors, so retrying is futile and a stuck pending entry would block
// all future pushes for this review file.
func pushDeletedComments(ctx pushContext) (int, bool, bool) {
	pending := CollectDeletesForPush(ctx.cj)
	if len(pending) == 0 {
		return 0, false, false
	}
	drained := make([]int64, 0, len(pending))
	failed := false
	authFailed := false
	for _, id := range pending {
		status, err := DeleteGHComment(id)
		switch {
		case err != nil && errors.Is(err, ErrGHAuthFailed):
			fmt.Fprintf(os.Stderr, "Warning: failed to delete comment %d: %v\n", id, err)
			authFailed = true
		case err != nil:
			fmt.Fprintf(os.Stderr, "Warning: failed to delete comment %d: %v\n", id, err)
			failed = true
		case status >= 200 && status < 300, status == 404:
			drained = append(drained, id)
		case status == 403:
			fmt.Fprintf(os.Stderr, "Warning: cannot delete comment %d on GitHub (403; not the author) — dropping pending delete\n", id)
			drained = append(drained, id)
		default:
			fmt.Fprintf(os.Stderr, "Warning: unexpected status %d deleting comment %d\n", status, id)
			failed = true
		}
		if authFailed {
			break
		}
	}
	if uerr := UpdateCritJSONAfterDeletes(ctx.critPath, drained); uerr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update review file after delete push: %v\n", uerr)
	}
	return len(drained), failed, authFailed
}

// fullStackPushGateMessage is the user-facing error string emitted when
// `crit push` is invoked while the active diff scope is the cumulative
// stack range. Comments authored under that scope carry line numbers that
// don't correspond to the PR's head diff, so the entire push is refused.
// The exact wording is asserted by test/shell/test-diff.sh Instance 6.
const fullStackPushGateMessage = "Switch to Layer diff before posting a platform review"

// pushBlockedByFullStackScope reports whether the on-disk active diff scope
// requires `crit push` to abort with the gate message.
func pushBlockedByFullStackScope(activeScope string) bool {
	return activeScope == string(session.DiffScopeFullStack)
}

func RunPush(args []string) error {
	ctx, err := loadPushContext(args)
	if err != nil {
		return err
	}

	if err := share.CheckGitHubSyncAllowed(ctx.cj, "crit push"); err != nil {
		return err
	}

	// Full-stack push gate — see fullStackPushGateMessage.
	if pushBlockedByFullStackScope(ctx.cj.ActiveDiffScope) {
		fmt.Fprintln(os.Stderr, "Error: "+fullStackPushGateMessage)
		return clicmd.ExitError{Code: 1, Err: errors.New("exit")}
	}

	inRange := ctx.cj.ActiveDiffScope != ""
	currentHead, err := resolveCurrentPRHead(ctx.prNumber, inRange, ctx.flags.dryRun)
	if err != nil {
		return err
	}

	b := BucketCommentsForPush(ctx.cj, currentHead, inRange)

	if ctx.flags.dryRun {
		runPushDryRun(ctx, b)
		return nil
	}
	if code := RunPushLive(ctx, b); code != 0 {
		return clicmd.ExitError{Code: code, Err: errors.New("push failed")}
	}
	return nil
}

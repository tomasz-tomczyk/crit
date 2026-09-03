package gitlab

import (
	"context"
	"crypto/sha1" //nolint:gosec // GitLab's line_code protocol requires SHA-1.
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/share"
)

type pushFlags struct {
	spec      string
	dryRun    bool
	message   string
	outputDir string
	sessionID string
	event     string
}

func parsePushFlags(args []string) (pushFlags, error) {
	flags := pushFlags{event: "comment"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			flags.dryRun = true
		case "--message", "-m":
			if i+1 >= len(args) {
				return flags, fmt.Errorf("%s requires a value", args[i])
			}
			i++
			flags.message = args[i]
		case "--output", "-o":
			if i+1 >= len(args) {
				return flags, fmt.Errorf("%s requires a value", args[i])
			}
			i++
			flags.outputDir = args[i]
		case "--session":
			if i+1 >= len(args) {
				return flags, fmt.Errorf("--session requires a value")
			}
			i++
			flags.sessionID = args[i]
		case "--event", "-e":
			if i+1 >= len(args) {
				return flags, fmt.Errorf("%s requires a value", args[i])
			}
			i++
			flags.event = strings.ToLower(args[i])
		default:
			if flags.spec != "" {
				return flags, usagePushError()
			}
			flags.spec = args[i]
		}
	}
	switch flags.event {
	case "comment", "approve", "request-changes":
	default:
		return flags, fmt.Errorf("invalid --event %q (expected comment, approve, or request-changes)", flags.event)
	}
	if flags.event == "request-changes" && flags.message == "" {
		return flags, fmt.Errorf("--event request-changes requires --message")
	}
	return flags, nil
}

func usagePushError() error {
	fmt.Fprintln(os.Stderr, "Usage: crit push [--session <id>] [--dry-run] [--event <type>] [--message <msg>] [--output <dir>] [mr-iid|url]")
	return clicmd.ExitError{Code: 1, Err: errors.New("exit")}
}

type pushCandidate struct {
	Path         string
	CommentID    string
	ReplyID      string
	Body         string
	DiscussionID string
	StartLine    int
	EndLine      int
	Side         string
}

type draftNoteRaw struct {
	ID           int64           `json:"id"`
	Note         string          `json:"note"`
	DiscussionID string          `json:"discussion_id"`
	Position     *gitlabPosition `json:"position"`
}

const localMarkerPrefix = "<!-- crit-local-id:"

func withLocalMarker(body, id string) string {
	return strings.TrimRight(body, "\n") + "\n\n" + localMarkerPrefix + id + " -->"
}

func splitLocalMarker(body string) (string, string) {
	start := strings.LastIndex(body, localMarkerPrefix)
	if start < 0 {
		return body, ""
	}
	end := strings.Index(body[start:], " -->")
	if end < 0 {
		return body, ""
	}
	end += start
	id := strings.TrimSpace(strings.TrimPrefix(body[start:end], localMarkerPrefix))
	if id == "" {
		return body, ""
	}
	return strings.TrimRight(body[:start], "\n"), id
}

func runPush(ctx context.Context, request forge.PushRequest) (forge.PushResult, error) { //nolint:gocyclo
	flags := pushFlags{spec: request.ChangeSpec, dryRun: request.DryRun, message: request.Message, outputDir: request.OutputDir, sessionID: request.SessionID, event: request.Event}
	if flags.event == "" {
		flags.event = "comment"
	}
	if request.Args != nil {
		var err error
		flags, err = parsePushFlags(request.Args)
		if err != nil {
			return forge.PushResult{}, err
		}
	}
	id, err := resolveChangeID(ctx, request.Repo, flags.spec)
	if err != nil {
		return forge.PushResult{}, err
	}
	repo := request.Repo
	repo.Host = first(id.Host, repo.Host)
	if err := requireGLab(ctx, repo); err != nil {
		return forge.PushResult{}, err
	}
	critPath, cj, err := loadPushReview(flags.sessionID, flags.outputDir)
	if err != nil {
		return forge.PushResult{}, err
	}
	if err := share.CheckGitHubSyncAllowed(cj, "crit push"); err != nil {
		return forge.PushResult{}, err
	}
	if cj.ActiveDiffScope == string(session.DiffScopeFullStack) {
		return forge.PushResult{}, fmt.Errorf("switch to Layer diff before posting a platform review")
	}

	mr, err := fetchMRRaw(ctx, repo, id)
	if err != nil {
		return forge.PushResult{}, err
	}
	candidates, skipped := collectPushCandidates(cj, mr.DiffRefs.HeadSHA)
	if flags.dryRun {
		fmt.Printf("GitLab MR !%d: %d new comments/replies, %d skipped; event=%s\n", id.Number, len(candidates), skipped, flags.event)
		return forge.PushResult{}, nil
	}
	existingDrafts, err := fetchDraftNotes(ctx, repo, id)
	if err != nil {
		return forge.PushResult{}, err
	}
	ownedDraftCount := countOwnedDrafts(existingDrafts)
	needsBulkPublish := len(candidates) > 0 || ownedDraftCount > 0 || flags.message != "" || flags.event != "comment"
	if needsBulkPublish {
		if draft := firstUnownedDraft(existingDrafts); draft != nil {
			return forge.PushResult{}, fmt.Errorf("GitLab has an unrelated unpublished draft note (%d); publish or discard it before Crit submits this review", draft.ID)
		}
	}

	result := forge.PushResult{}
	edited, err := pushGitLabEdits(ctx, repo, id, &cj)
	result.Edited = edited
	if err != nil {
		if edited > 0 {
			err = savePartialGitLabPush(critPath, cj, err)
		}
		return result, err
	}
	deleted, err := pushGitLabDeletes(ctx, repo, id, &cj)
	result.Deleted = deleted
	if err != nil {
		if result.Edited+deleted > 0 {
			err = savePartialGitLabPush(critPath, cj, err)
		}
		return result, err
	}
	if result.Edited+result.Deleted > 0 {
		if err := review.SaveCritJSON(critPath, cj); err != nil {
			return result, err
		}
	}

	created, replies, err := stageDraftNotes(ctx, repo, id, mr.DiffRefs, candidates, existingDrafts)
	if err != nil {
		return result, fmt.Errorf("GitLab draft review remains unpublished: %w", err)
	}
	result.Created = created
	result.Replied = replies
	if !needsBulkPublish {
		resolved, resolveErr := syncGitLabResolution(ctx, repo, id, &cj)
		result.Resolved = resolved
		if resolveErr != nil {
			if result.Edited+result.Deleted+resolved > 0 {
				resolveErr = savePartialGitLabPush(critPath, cj, resolveErr)
			}
			return result, resolveErr
		}
		if resolved > 0 {
			if err := review.SaveCritJSON(critPath, cj); err != nil {
				return result, err
			}
		}
		if result.Edited+result.Deleted+result.Resolved == 0 {
			fmt.Println("No comments to push.")
		} else {
			fmt.Printf("Posted %d comments, %d replies, edited %d, deleted %d, resolved %d on MR !%d.\n",
				result.Created, result.Replied, result.Edited, result.Deleted, result.Resolved, id.Number)
		}
		return result, nil
	}

	publishPayload := map[string]any{"reviewer_state": reviewerState(flags.event)}
	if flags.message != "" {
		publishPayload["note"] = flags.message
	}
	if _, err := runAPI(ctx, repo.Host, projectEndpoint(id, "/draft_notes/bulk_publish"), "POST", publishPayload); err != nil {
		if flags.event == "request-changes" {
			return result, fmt.Errorf("GitLab did not publish the requested-changes review (this feature may require Premium or Ultimate); drafts remain unpublished: %w", err)
		}
		return result, fmt.Errorf("publishing GitLab draft review: %w", err)
	}
	if err := refreshGitLabIDs(ctx, repo, id, critPath, &cj); err != nil {
		return result, err
	}
	resolved, err := syncGitLabResolution(ctx, repo, id, &cj)
	result.Resolved = resolved
	if err != nil {
		if resolved > 0 {
			err = savePartialGitLabPush(critPath, cj, err)
		}
		return result, err
	}
	if resolved > 0 {
		if err := review.SaveCritJSON(critPath, cj); err != nil {
			return result, err
		}
	}
	if flags.event == "approve" {
		if _, err := runAPI(ctx, repo.Host, projectEndpoint(id, "/approve"), "POST", map[string]any{"sha": mr.DiffRefs.HeadSHA}); err != nil {
			return result, fmt.Errorf("comments published, but GitLab approval failed: %w", err)
		}
	}
	if result.Created+result.Replied+result.Edited+result.Deleted+result.Resolved == 0 && flags.message == "" && flags.event == "comment" {
		fmt.Println("No comments to push.")
	} else {
		fmt.Printf("Posted %d comments, %d replies, edited %d, deleted %d, resolved %d on MR !%d.\n",
			result.Created, result.Replied, result.Edited, result.Deleted, result.Resolved, id.Number)
	}
	return result, nil
}

func savePartialGitLabPush(path string, cj session.CritJSON, operationErr error) error {
	if err := review.SaveCritJSON(path, cj); err != nil {
		return errors.Join(operationErr, fmt.Errorf("persisting successful GitLab operations: %w", err))
	}
	return operationErr
}

func loadPushReview(sessionID, outputDir string) (string, session.CritJSON, error) {
	critPath, err := resolvePushPullReviewPath(sessionID, outputDir)
	if err != nil {
		return "", session.CritJSON{}, err
	}
	data, err := session.ReadFileShared(session.ReviewPathsFor(critPath).Review)
	if err != nil {
		if os.IsNotExist(err) {
			return "", session.CritJSON{}, fmt.Errorf("no review file found. Run a crit review first")
		}
		return "", session.CritJSON{}, err
	}
	var cj session.CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return "", cj, fmt.Errorf("invalid review file: %w", err)
	}
	return critPath, cj, nil
}

func resolvePushPullReviewPath(sessionID, outputDir string) (string, error) {
	if sessionID != "" {
		return review.ResolveCommandReviewPathWithSession(sessionID, outputDir, "")
	}
	return review.ResolveReviewPath(outputDir)
}

func fetchMRRaw(ctx context.Context, repo forge.RepoContext, id forge.ChangeID) (mrRaw, error) {
	out, err := runAPI(ctx, first(id.Host, repo.Host), projectEndpoint(id, ""), "GET", nil)
	if err != nil {
		return mrRaw{}, err
	}
	var mr mrRaw
	if err := json.Unmarshal(out, &mr); err != nil {
		return mr, fmt.Errorf("parsing GitLab merge request: %w", err)
	}
	if mr.DiffRefs.BaseSHA == "" || mr.DiffRefs.StartSHA == "" || mr.DiffRefs.HeadSHA == "" {
		return mr, fmt.Errorf("GitLab merge request diff is not ready (missing diff_refs)")
	}
	return mr, nil
}

func collectPushCandidates(cj session.CritJSON, currentHead string) ([]pushCandidate, int) { //nolint:gocyclo // comment/reply eligibility is clearest as one traversal
	paths := make([]string, 0, len(cj.Files))
	for path := range cj.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var candidates []pushCandidate
	skipped := 0
	for _, path := range paths {
		for _, comment := range cj.Files[path].Comments {
			switch {
			case comment.Resolved || comment.DOMAnchor != nil || comment.Drifted || comment.EndLine <= 0:
				skipped++
			case comment.HeadSHA != "" && currentHead != "" && comment.HeadSHA != currentHead:
				skipped++
			case comment.GitLabNoteID == 0 && comment.GitHubID == 0:
				candidates = append(candidates, pushCandidate{
					Path: path, CommentID: comment.ID, Body: session.StripBodyRewriter(comment.Body),
					StartLine: comment.StartLine, EndLine: comment.EndLine, Side: comment.Side,
				})
			}
			// Replies are independent remote mutations. A locally resolved,
			// drifted, or outdated root can still have an unsynced reply on an
			// existing GitLab discussion.
			if comment.GitLabDiscussionID != "" {
				for _, reply := range comment.Replies {
					if reply.GitLabNoteID == 0 && reply.GitHubID == 0 {
						candidates = append(candidates, pushCandidate{
							Path: path, CommentID: comment.ID, ReplyID: reply.ID,
							Body: session.StripBodyRewriter(reply.Body), DiscussionID: comment.GitLabDiscussionID,
						})
					}
				}
			}
		}
	}
	return candidates, skipped
}

func fetchDraftNotes(ctx context.Context, repo forge.RepoContext, id forge.ChangeID) ([]draftNoteRaw, error) {
	out, err := runAPI(ctx, repo.Host, projectEndpoint(id, "/draft_notes"), "GET", nil)
	if err != nil {
		return nil, err
	}
	var drafts []draftNoteRaw
	if err := json.Unmarshal(out, &drafts); err != nil {
		return nil, fmt.Errorf("parsing GitLab draft notes: %w", err)
	}
	return drafts, nil
}

func stageDraftNotes(ctx context.Context, repo forge.RepoContext, id forge.ChangeID, refs diffRefs, candidates []pushCandidate, existing []draftNoteRaw) (int, int, error) {
	created, replies := 0, 0
	for _, candidate := range candidates {
		if matchingDraft(candidate, existing) {
			continue
		}
		payload := map[string]any{"note": withLocalMarker(candidate.Body, candidateLocalID(candidate))}
		if candidate.DiscussionID != "" {
			payload["in_reply_to_discussion_id"] = candidate.DiscussionID
		} else {
			payload["position"] = buildPosition(refs, candidate)
		}
		out, err := runAPI(ctx, repo.Host, projectEndpoint(id, "/draft_notes"), "POST", payload)
		if err != nil {
			return created, replies, fmt.Errorf("staging comment %s: %w", candidateLocalID(candidate), err)
		}
		var draft draftNoteRaw
		if err := json.Unmarshal(out, &draft); err == nil {
			existing = append(existing, draft)
		}
		if candidate.ReplyID != "" {
			replies++
		} else {
			created++
		}
	}
	return created, replies, nil
}

func matchingDraft(candidate pushCandidate, drafts []draftNoteRaw) bool {
	for _, draft := range drafts {
		body, localID := splitLocalMarker(draft.Note)
		if localID == candidateLocalID(candidate) {
			return true
		}
		if body != candidate.Body {
			continue
		}
		if candidate.DiscussionID != "" {
			if draft.DiscussionID == candidate.DiscussionID {
				return true
			}
			continue
		}
		if draft.Position == nil {
			continue
		}
		path, start, end, side := noteLocation(gitlabNote{Position: draft.Position})
		if path == candidate.Path && start == candidate.StartLine && end == candidate.EndLine && normalizedSide(side) == normalizedSide(candidate.Side) {
			return true
		}
	}
	return false
}

func firstUnownedDraft(drafts []draftNoteRaw) *draftNoteRaw {
	for i := range drafts {
		_, id := splitLocalMarker(drafts[i].Note)
		if id == "" {
			return &drafts[i]
		}
	}
	return nil
}

func countOwnedDrafts(drafts []draftNoteRaw) int {
	count := 0
	for _, draft := range drafts {
		if _, id := splitLocalMarker(draft.Note); id != "" {
			count++
		}
	}
	return count
}

func candidateLocalID(candidate pushCandidate) string {
	if candidate.ReplyID != "" {
		return candidate.ReplyID
	}
	return candidate.CommentID
}

func buildPosition(refs diffRefs, candidate pushCandidate) map[string]any {
	position := map[string]any{
		"position_type": "text", "base_sha": refs.BaseSHA, "start_sha": refs.StartSHA,
		"head_sha": refs.HeadSHA, "old_path": candidate.Path, "new_path": candidate.Path,
	}
	side := normalizedSide(candidate.Side)
	if side == "old" {
		position["old_line"] = candidate.EndLine
	} else {
		position["new_line"] = candidate.EndLine
	}
	if candidate.StartLine > 0 && candidate.StartLine != candidate.EndLine {
		position["line_range"] = buildLineRange(candidate.Path, candidate.StartLine, candidate.EndLine, side)
	}
	return position
}

func buildLineRange(path string, start, end int, side string) map[string]any {
	startLine := map[string]any{"line_code": gitLabLineCode(path, lineForSide(start, side)), "type": side}
	endLine := map[string]any{"line_code": gitLabLineCode(path, lineForSide(end, side)), "type": side}
	if side == "old" {
		startLine["old_line"] = start
		endLine["old_line"] = end
	} else {
		startLine["new_line"] = start
		endLine["new_line"] = end
	}
	return map[string]any{"start": startLine, "end": endLine}
}

func lineForSide(line int, side string) string {
	if side == "old" {
		return fmt.Sprintf("%d_0", line)
	}
	return fmt.Sprintf("0_%d", line)
}

func gitLabLineCode(path, suffix string) string {
	sum := sha1.Sum([]byte(path))
	return hex.EncodeToString(sum[:]) + "_" + suffix
}

func normalizedSide(side string) string {
	if strings.EqualFold(side, "old") || strings.EqualFold(side, "LEFT") {
		return "old"
	}
	return "new"
}

func reviewerState(event string) string {
	if event == "request-changes" {
		return "requested_changes"
	}
	return "reviewed"
}

func refreshGitLabIDs(ctx context.Context, repo forge.RepoContext, id forge.ChangeID, critPath string, cj *session.CritJSON) error {
	discussions, err := fetchAllDiscussions(ctx, repo, id)
	if err != nil {
		return fmt.Errorf("review published but could not refresh GitLab discussion IDs: %w", err)
	}
	// Stamp imported threads with this MR's focus key even when no daemon is
	// probing (headless `crit push`). Falling back to ResolvePullScope alone
	// can yield DiffScope-only scope and stamp foreign notes as range:.. .
	scope := session.InheritedScope{
		Forge:        string(forge.GitLab),
		ChangeNumber: id.Number,
		DiffScope:    "layer",
	}
	if probed := session.ResolvePullScope(cj); probed.ChangeNumber > 0 || probed.HeadSHA != "" {
		scope = probed
	}
	mergeDiscussions(cj, discussions, scope)
	return review.SaveCritJSON(critPath, *cj)
}

func pushGitLabEdits(ctx context.Context, repo forge.RepoContext, id forge.ChangeID, cj *session.CritJSON) (int, error) {
	edited := 0
	for path, file := range cj.Files {
		for i := range file.Comments {
			comment := &file.Comments[i]
			if comment.GitLabNoteID != 0 && comment.GitLabDiscussionID != "" && comment.LastPushedBodyHash != bodyHash(comment.Body) {
				endpoint := fmt.Sprintf("%s/discussions/%s/notes/%d", projectEndpoint(id, ""), comment.GitLabDiscussionID, comment.GitLabNoteID)
				if _, err := runAPI(ctx, repo.Host, endpoint, "PUT", map[string]any{"body": session.StripBodyRewriter(comment.Body)}); err != nil {
					return edited, err
				}
				comment.LastPushedBodyHash = bodyHash(comment.Body)
				edited++
			}
			for j := range comment.Replies {
				reply := &comment.Replies[j]
				if reply.GitLabNoteID == 0 || reply.GitLabDiscussionID == "" || reply.LastPushedBodyHash == bodyHash(reply.Body) {
					continue
				}
				endpoint := fmt.Sprintf("%s/discussions/%s/notes/%d", projectEndpoint(id, ""), reply.GitLabDiscussionID, reply.GitLabNoteID)
				if _, err := runAPI(ctx, repo.Host, endpoint, "PUT", map[string]any{"body": session.StripBodyRewriter(reply.Body)}); err != nil {
					return edited, err
				}
				reply.LastPushedBodyHash = bodyHash(reply.Body)
				edited++
			}
			file.Comments[i] = *comment
		}
		cj.Files[path] = file
	}
	return edited, nil
}

func syncGitLabResolution(ctx context.Context, repo forge.RepoContext, id forge.ChangeID, cj *session.CritJSON) (int, error) {
	seen := make(map[string]bool)
	count := 0
	for path, file := range cj.Files {
		for i := range file.Comments {
			comment := &file.Comments[i]
			if comment.GitLabDiscussionID == "" || seen[comment.GitLabDiscussionID] ||
				(comment.GitLabResolved != nil && *comment.GitLabResolved == comment.Resolved) {
				continue
			}
			seen[comment.GitLabDiscussionID] = true
			endpoint := fmt.Sprintf("%s/discussions/%s", projectEndpoint(id, ""), comment.GitLabDiscussionID)
			if _, err := runAPI(ctx, repo.Host, endpoint, "PUT", map[string]any{"resolved": comment.Resolved}); err != nil {
				return count, err
			}
			remoteResolved := comment.Resolved
			comment.GitLabResolved = &remoteResolved
			count++
		}
		cj.Files[path] = file
	}
	return count, nil
}

func pushGitLabDeletes(ctx context.Context, repo forge.RepoContext, id forge.ChangeID, cj *session.CritJSON) (int, error) {
	refs := session.RemoteDeletesFor(*cj, forge.GitLab)
	if len(refs) == 0 {
		return 0, nil
	}
	drained := 0
	remaining := make([]session.RemoteRef, 0, len(refs))
	for _, ref := range refs {
		discussionEndpoint := fmt.Sprintf("%s/discussions/%s", projectEndpoint(id, ""), ref.ThreadID)
		out, err := runAPI(ctx, repo.Host, discussionEndpoint, "GET", nil)
		if err != nil {
			// Discussion already gone (cascade / prior delete) — drain like GitHub 404.
			if isNotFoundAPIError(err) {
				drained++
				continue
			}
			remaining = append(remaining, ref)
			continue
		}
		var discussion gitlabDiscussion
		if err := json.Unmarshal(out, &discussion); err != nil {
			remaining = append(remaining, ref)
			continue
		}
		failed := false
		for _, noteID := range gitLabDeleteNoteIDs(discussion, ref.CommentID) {
			endpoint := fmt.Sprintf("%s/notes/%d", discussionEndpoint, noteID)
			if _, err := runAPI(ctx, repo.Host, endpoint, "DELETE", nil); err != nil {
				if isNotFoundAPIError(err) {
					continue
				}
				failed = true
				break
			}
		}
		if failed {
			remaining = append(remaining, ref)
			continue
		}
		drained++
	}
	session.ReplaceRemoteDeletes(cj, forge.GitLab, remaining)
	if len(remaining) > 0 {
		return drained, fmt.Errorf("deleted %d GitLab notes, but %d deletions failed and remain queued", drained, len(remaining))
	}
	return drained, nil
}

func gitLabDeleteNoteIDs(discussion gitlabDiscussion, refNoteID int64) []int64 {
	rootIndex := rootDiffNoteIndex(discussion.Notes)
	if rootIndex < 0 || discussion.Notes[rootIndex].ID != refNoteID {
		return []int64{refNoteID}
	}
	// GitLab promotes a surviving reply when the root note is deleted. Delete
	// replies first, then the root, so a local parent deletion removes the
	// complete remote thread.
	ids := make([]int64, 0, len(discussion.Notes))
	for i := len(discussion.Notes) - 1; i >= 0; i-- {
		note := discussion.Notes[i]
		if !note.System && note.ID != 0 {
			ids = append(ids, note.ID)
		}
	}
	return ids
}

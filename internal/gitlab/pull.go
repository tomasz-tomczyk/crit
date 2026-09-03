package gitlab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/forge"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/share"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

type pullFlags struct {
	spec      string
	outputDir string
	sessionID string
}

func parsePullFlags(args []string) (pullFlags, error) {
	var flags pullFlags
	for i := 0; i < len(args); i++ {
		switch args[i] {
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
		default:
			if flags.spec != "" {
				return flags, usagePullError()
			}
			flags.spec = args[i]
		}
	}
	return flags, nil
}

func usagePullError() error {
	fmt.Fprintln(os.Stderr, "Usage: crit pull [--session <id>] [--output <dir>] [mr-iid|url]")
	return clicmd.ExitError{Code: 1, Err: errors.New("exit")}
}

type gitlabPositionLine struct {
	LineCode string `json:"line_code"`
	Type     string `json:"type"`
	OldLine  int    `json:"old_line"`
	NewLine  int    `json:"new_line"`
}

type gitlabLineRange struct {
	Start gitlabPositionLine `json:"start"`
	End   gitlabPositionLine `json:"end"`
}

type gitlabPosition struct {
	PositionType string           `json:"position_type"`
	BaseSHA      string           `json:"base_sha"`
	StartSHA     string           `json:"start_sha"`
	HeadSHA      string           `json:"head_sha"`
	OldPath      string           `json:"old_path"`
	NewPath      string           `json:"new_path"`
	OldLine      int              `json:"old_line"`
	NewLine      int              `json:"new_line"`
	LineRange    *gitlabLineRange `json:"line_range"`
}

type gitlabNote struct {
	ID         int64           `json:"id"`
	Type       string          `json:"type"`
	Body       string          `json:"body"`
	Author     gitlabUser      `json:"author"`
	CreatedAt  string          `json:"created_at"`
	Resolvable bool            `json:"resolvable"`
	Resolved   bool            `json:"resolved"`
	System     bool            `json:"system"`
	Position   *gitlabPosition `json:"position"`
}

type gitlabDiscussion struct {
	ID    string       `json:"id"`
	Notes []gitlabNote `json:"notes"`
}

func runPull(ctx context.Context, request forge.PullRequest) (forge.PullResult, error) {
	flags := pullFlags{spec: request.ChangeSpec, outputDir: request.OutputDir, sessionID: request.SessionID}
	if request.Args != nil {
		var err error
		flags, err = parsePullFlags(request.Args)
		if err != nil {
			return forge.PullResult{}, err
		}
	}
	id, err := resolveChangeID(ctx, request.Repo, flags.spec)
	if err != nil {
		return forge.PullResult{}, err
	}
	repo := request.Repo
	repo.Host = first(id.Host, repo.Host)
	if err := requireGLab(ctx, repo); err != nil {
		return forge.PullResult{}, err
	}

	discussions, err := fetchAllDiscussions(ctx, repo, id)
	if err != nil {
		return forge.PullResult{}, err
	}

	critPath, err := resolvePushPullReviewPath(flags.sessionID, flags.outputDir)
	if err != nil {
		return forge.PullResult{}, err
	}
	cj, err := loadOrInitializeReview(critPath)
	if err != nil {
		return forge.PullResult{}, err
	}
	if err := share.CheckGitHubSyncAllowed(cj, "crit pull"); err != nil {
		return forge.PullResult{}, err
	}

	scope := session.ResolvePullScope(&cj)
	imported, updated := mergeDiscussions(&cj, discussions, scope)
	if imported+updated > 0 {
		if err := review.SaveCritJSON(critPath, cj); err != nil {
			return forge.PullResult{}, err
		}
	}
	if imported+updated == 0 {
		fmt.Printf("No new inline comments found on MR !%d\n", id.Number)
	} else {
		fmt.Printf("Pulled %d comments from MR !%d into %s\n", imported, id.Number, critPath)
		fmt.Println("Run 'crit' to view them in the browser.")
	}
	return forge.PullResult{Imported: imported, Updated: updated}, nil
}

func resolveChangeID(ctx context.Context, repo forge.RepoContext, spec string) (forge.ChangeID, error) {
	if spec != "" {
		id, err := ParseMRSpec(spec)
		if err != nil {
			return forge.ChangeID{}, err
		}
		if repo.Host != "" && id.Host != "" && !strings.EqualFold(repo.Host, id.Host) {
			return forge.ChangeID{}, fmt.Errorf("merge request URL host %q does not match configured gitlab_url host %q", id.Host, repo.Host)
		}
		if repo.Host != "" {
			id.Host = repo.Host
		}
		return id, nil
	}
	return (Provider{Host: repo.Host}).Detect(ctx, repo)
}

func loadOrInitializeReview(critPath string) (session.CritJSON, error) {
	var cj session.CritJSON
	data, err := session.ReadFileShared(session.ReviewPathsFor(critPath).Review)
	if err == nil {
		if unmarshalErr := json.Unmarshal(data, &cj); unmarshalErr != nil {
			return cj, fmt.Errorf("invalid review file: %w", unmarshalErr)
		}
	} else if !os.IsNotExist(err) {
		return cj, err
	}
	if cj.Files == nil {
		cj.Files = make(map[string]session.CritJSONFile)
		cj.Branch = vcs.CurrentBranch()
		cfg := config.LoadConfig("")
		base := cfg.BaseBranch
		if base == "" {
			base = vcs.DefaultBaseRef()
		}
		cj.BaseRef, _ = vcs.MergeBase(base)
		cj.ReviewRound = 1
	}
	return cj, nil
}

func mergeDiscussions(cj *session.CritJSON, discussions []gitlabDiscussion, scope session.InheritedScope) (int, int) { //nolint:gocyclo // thread reconciliation intentionally keeps local/remote conflict rules together
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now
	imported, updated := 0, 0
	seenNotes := make(map[int64]bool)
	remoteDeletes := session.RemoteDeletesFor(*cj, forge.GitLab)
	pendingDeletes := make(map[int64]bool, len(remoteDeletes))
	for _, ref := range remoteDeletes {
		pendingDeletes[ref.CommentID] = true
	}
	for _, discussion := range discussions {
		for _, note := range discussion.Notes {
			seenNotes[note.ID] = true
		}
		rootIndex := rootDiffNoteIndex(discussion.Notes)
		if rootIndex < 0 {
			continue
		}
		root := discussion.Notes[rootIndex]
		if pendingDeletes[root.ID] {
			continue
		}
		var rootLocalID string
		root.Body, rootLocalID = splitLocalMarker(root.Body)
		path, startLine, endLine, side := noteLocation(root)
		if path == "" || endLine <= 0 {
			continue
		}
		cf := cj.Files[path]
		if cf.Comments == nil {
			cf.Status = "modified"
			cf.Comments = []session.Comment{}
		}
		commentIndex := findGitLabComment(cf.Comments, root, rootLocalID, startLine, endLine)
		if commentIndex < 0 {
			commentID := rootLocalID
			if !strings.HasPrefix(commentID, "c_") {
				commentID = session.RandomCommentID()
			}
			remoteResolved := root.Resolved
			comment := session.StampWithFocus(session.Comment{
				ID: commentID, StartLine: startLine, EndLine: endLine,
				Side: side, Body: root.Body, Author: displayName(root.Author),
				CreatedAt: root.CreatedAt, UpdatedAt: now, Resolved: root.Resolved,
				GitLabNoteID: root.ID, GitLabDiscussionID: discussion.ID,
				GitLabResolved:     &remoteResolved,
				LastPushedBodyHash: bodyHash(root.Body),
			}, scope.AsFocus())
			if root.Resolved {
				comment.ResolvedRound = cj.ReviewRound
			}
			cf.Comments = append(cf.Comments, comment)
			commentIndex = len(cf.Comments) - 1
			imported++
		} else {
			comment := &cf.Comments[commentIndex]
			previousRemoteResolved := comment.GitLabResolved
			if comment.LastPushedBodyHash == bodyHash(comment.Body) && comment.Body != root.Body {
				comment.Body = root.Body
				comment.UpdatedAt = now
				updated++
			}
			comment.LastPushedBodyHash = bodyHash(root.Body)
			remoteResolved := root.Resolved
			if comment.GitLabNoteID == 0 {
				comment.GitLabNoteID = root.ID
				comment.GitLabDiscussionID = discussion.ID
				updated++
			}
			if previousRemoteResolved == nil || comment.Resolved == *previousRemoteResolved {
				if comment.Resolved != root.Resolved {
					comment.Resolved = root.Resolved
					comment.UpdatedAt = now
					if root.Resolved {
						comment.ResolvedRound = cj.ReviewRound
					} else {
						comment.ResolvedRound = 0
					}
					updated++
				}
			}
			comment.GitLabResolved = &remoteResolved
		}
		for i, note := range discussion.Notes {
			if i == rootIndex || note.System || note.ID == 0 || pendingDeletes[note.ID] {
				continue
			}
			added, changed := appendGitLabReply(&cf.Comments[commentIndex], discussion.ID, note)
			if added {
				imported++
			} else if changed {
				updated++
			}
		}
		cj.Files[path] = cf
	}
	updated += dropDeletedGitLabNotes(cj, seenNotes)
	updated += dropCompletedGitLabDeletes(cj, seenNotes)
	return imported, updated
}

func fetchAllDiscussions(ctx context.Context, repo forge.RepoContext, id forge.ChangeID) ([]gitlabDiscussion, error) {
	const perPage = 100
	var all []gitlabDiscussion
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("%s/discussions?per_page=%d&page=%d", projectEndpoint(id, ""), perPage, page)
		out, err := runAPI(ctx, repo.Host, endpoint, "GET", nil)
		if err != nil {
			return nil, err
		}
		var batch []gitlabDiscussion
		if err := json.Unmarshal(out, &batch); err != nil {
			return nil, fmt.Errorf("parsing GitLab discussions: %w", err)
		}
		all = append(all, batch...)
		if len(batch) < perPage {
			return all, nil
		}
	}
}

func dropDeletedGitLabNotes(cj *session.CritJSON, seen map[int64]bool) int {
	removed := 0
	for path, file := range cj.Files {
		comments := file.Comments[:0]
		for _, comment := range file.Comments {
			if comment.GitLabNoteID != 0 && !seen[comment.GitLabNoteID] {
				removed++
				continue
			}
			replies := comment.Replies[:0]
			for _, reply := range comment.Replies {
				if reply.GitLabNoteID != 0 && !seen[reply.GitLabNoteID] {
					removed++
					continue
				}
				replies = append(replies, reply)
			}
			comment.Replies = replies
			comments = append(comments, comment)
		}
		file.Comments = comments
		cj.Files[path] = file
	}
	return removed
}

func dropCompletedGitLabDeletes(cj *session.CritJSON, seen map[int64]bool) int {
	refs := session.RemoteDeletesFor(*cj, forge.GitLab)
	remaining := make([]session.RemoteRef, 0, len(refs))
	drained := 0
	for _, ref := range refs {
		if !seen[ref.CommentID] {
			drained++
			continue
		}
		remaining = append(remaining, ref)
	}
	session.ReplaceRemoteDeletes(cj, forge.GitLab, remaining)
	return drained
}

func rootDiffNoteIndex(notes []gitlabNote) int {
	for i, note := range notes {
		if !note.System && note.Position != nil && note.Position.PositionType == "text" {
			return i
		}
	}
	return -1
}

func noteLocation(note gitlabNote) (path string, start, end int, side string) {
	if note.Position == nil {
		return "", 0, 0, ""
	}
	p := note.Position
	side = noteSide(p)
	if side == "old" {
		path, start, end = noteSpan(p.OldPath, p.OldLine, p.LineRange, true)
	} else {
		path, start, end = noteSpan(p.NewPath, p.NewLine, p.LineRange, false)
	}
	return path, start, end, side
}

// noteSide prefers line_range type when present — GitLab often fills both
// old_line and new_line on context lines; NewLine>0 alone would mis-label
// old-side comments as "new".
func noteSide(p *gitlabPosition) string {
	if p.LineRange != nil && (p.LineRange.Start.Type == "old" || p.LineRange.End.Type == "old") {
		return "old"
	}
	if p.NewLine == 0 && p.OldLine > 0 {
		return "old"
	}
	return "new"
}

func noteSpan(path string, end int, lr *gitlabLineRange, old bool) (string, int, int) {
	if end == 0 && lr != nil {
		if old {
			end = lr.End.OldLine
		} else {
			end = lr.End.NewLine
		}
	}
	start := end
	if lr != nil {
		if old && lr.Start.OldLine > 0 {
			start = lr.Start.OldLine
		} else if !old && lr.Start.NewLine > 0 {
			start = lr.Start.NewLine
		}
	}
	return path, start, end
}

func findGitLabComment(comments []session.Comment, note gitlabNote, localID string, start, end int) int {
	for i, comment := range comments {
		if localID != "" && comment.ID == localID {
			return i
		}
		if note.ID != 0 && comment.GitLabNoteID == note.ID {
			return i
		}
		if comment.Author == displayName(note.Author) && comment.StartLine == start &&
			comment.EndLine == end && comment.Body == note.Body {
			return i
		}
	}
	return -1
}

func appendGitLabReply(comment *session.Comment, discussionID string, note gitlabNote) (added bool, changed bool) {
	var localID string
	note.Body, localID = splitLocalMarker(note.Body)
	for i := range comment.Replies {
		reply := &comment.Replies[i]
		if localID != "" && reply.ID == localID {
			if reply.GitLabNoteID == 0 {
				reply.GitLabNoteID = note.ID
				reply.GitLabDiscussionID = discussionID
				reply.LastPushedBodyHash = bodyHash(reply.Body)
				return false, true
			}
			return false, syncGitLabReply(reply, note)
		}
		if reply.GitLabNoteID == note.ID {
			return false, syncGitLabReply(reply, note)
		}
		if reply.Author == displayName(note.Author) && reply.Body == note.Body {
			if reply.GitLabNoteID == 0 {
				reply.GitLabNoteID = note.ID
				reply.GitLabDiscussionID = discussionID
				reply.LastPushedBodyHash = bodyHash(reply.Body)
				return false, true
			}
			return false, false
		}
	}
	replyID := localID
	if !strings.HasPrefix(replyID, "rp_") {
		replyID = session.RandomReplyID()
	}
	comment.Replies = append(comment.Replies, session.Reply{
		ID: replyID, Body: note.Body, Author: displayName(note.Author),
		CreatedAt: note.CreatedAt, GitLabNoteID: note.ID,
		GitLabDiscussionID: discussionID, LastPushedBodyHash: bodyHash(note.Body),
	})
	return true, false
}

func syncGitLabReply(reply *session.Reply, note gitlabNote) bool {
	changed := false
	if reply.LastPushedBodyHash == bodyHash(reply.Body) && reply.Body != note.Body {
		reply.Body = note.Body
		changed = true
	}
	reply.LastPushedBodyHash = bodyHash(note.Body)
	return changed
}

func bodyHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:8])
}

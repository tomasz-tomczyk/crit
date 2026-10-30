package share

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/focus"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

// DefaultShareURL is the production crit-web service URL, used as the fallback
// when no share URL is configured via flag, env, or config.
const DefaultShareURL = "https://crit.md"

// checkShareAllowed returns an error when the review at critPath is a live
// review. Sharing live reviews is deferred (v1 spec §Non-goals).
func checkShareAllowed(critPath string) error {
	data, err := os.ReadFile(session.ReviewPathsFor(critPath).Review)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cj session.CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return nil //nolint:nilerr // malformed review file: do not block share
	}
	if cj.ReviewType == "live" {
		return fmt.Errorf("crit share is not supported for live reviews in v1")
	}
	return nil
}

// checkGitHubSyncAllowed gates `crit pull` and `crit push` from running on
// live reviews. Live pins have no line anchors and cannot round-trip
// through GitHub PR review comments.
func checkGitHubSyncAllowed(cj session.CritJSON, op string) error {
	if cj.ReviewType == "live" {
		return fmt.Errorf("%s is not supported for live reviews", op)
	}
	return nil
}

var ErrShareUnauthorized = errors.New("auth token rejected by share service")
var ErrShareNotFound = errors.New("shared review not found")

// shareScope computes a hash of sorted file paths, used to detect when
// share state belongs to a different file set.
func ShareScope(paths []string) string {
	sorted := make([]string, len(paths))
	copy(sorted, paths)
	sort.Strings(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(h[:8]) // 16-char hex prefix is enough
}

// computeShareHash returns a short stable hash of the current share state:
// file contents (for change detection) and comment resolution states, plus,
// for each sub-thread, its anchor and its replies, so a new sub-thread or a
// reply in one triggers the upsert. A share without sub-threads hashes as it
// did before they existed, so upgrading crit does not force a re-upload.
// If the hash equals LastShareHash in the review file, nothing has changed since
// the last push and no new round is needed.
func computeShareHash(files []ShareFile, comments []ShareComment) string {
	sorted := make([]ShareFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	sortedC := make([]ShareComment, len(comments))
	copy(sortedC, comments)
	sort.Slice(sortedC, func(i, j int) bool { return sortedC[i].ExternalID < sortedC[j].ExternalID })

	h := sha256.New()
	for _, f := range sorted {
		fmt.Fprintf(h, "file:%s:%s:%s\n", f.Path, f.Content, f.Status)
	}
	for _, c := range sortedC {
		fmt.Fprintf(h, "comment:%s:%v\n", c.ExternalID, c.Resolved)
		if c.ParentExternalID == "" {
			continue
		}
		fmt.Fprintf(h, "sub_thread:%s:%s\n", c.ExternalID, c.ParentExternalID)
		for _, r := range c.Replies {
			fmt.Fprintf(h, "sub_thread_reply:%s:%s\n", c.ExternalID, r.ExternalID)
		}
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// ShareFile is an alias for session.ShareFile.
type ShareFile = session.ShareFile

// ShareReply represents a reply to include in the shared review.
//
// GitHubID is the GitHub PR review-comment ID this reply was synced from
// (zero when the reply originated locally). crit-web uses a non-zero value
// as the signal that the reply was imported from GitHub and renders an
// inline marker so re-sharers can tell synced replies apart from native
// crit replies. Encoded with `omitempty` so locally-authored replies stay
// indistinguishable on the wire (issue #370).
type ShareReply struct {
	Body       string `json:"body"`
	Author     string `json:"author_display_name,omitempty"`
	UserID     string `json:"user_id,omitempty"`
	ExternalID string `json:"external_id,omitempty"`
	GitHubID   int64  `json:"github_id,omitempty"`
}

// ShareComment represents a comment to include in the shared review.
//
// GitHubID is the GitHub PR review-comment ID this comment was synced from
// (zero when the comment originated locally). crit-web treats any non-zero
// value as the GitHub-synced signal so it can paint a badge on the
// re-shared review. Encoded with `omitempty` so locally-authored comments
// produce identical wire output to before this field landed (issue #370).
//
// A non-zero github_id already discriminates GitHub-synced entries from
// native crit comments, so a separate `source` field would be redundant.
// If we add other sync sources (GitLab, Gerrit) later, we can introduce a
// `source` enum at that point.
type ShareComment struct {
	File        string             `json:"file,omitempty"`
	StartLine   int                `json:"start_line,omitempty"`
	EndLine     int                `json:"end_line,omitempty"`
	Body        string             `json:"body"`
	Quote       string             `json:"quote,omitempty"`
	QuoteOffset *int               `json:"quote_offset,omitempty"`
	Author      string             `json:"author_display_name,omitempty"`
	UserID      string             `json:"user_id,omitempty"`
	Scope       string             `json:"scope,omitempty"`
	ReviewRound int                `json:"review_round,omitempty"`
	Replies     []ShareReply       `json:"replies,omitempty"`
	ExternalID  string             `json:"external_id,omitempty"`
	Resolved    bool               `json:"resolved,omitempty"`
	GitHubID    int64              `json:"github_id,omitempty"`
	DOMAnchor   *session.DOMAnchor `json:"dom_anchor,omitempty"`

	// ParentExternalID is set on a sub-thread: the local id of the message
	// (comment, sub-thread or reply) it comments on. The payload builders send
	// such an entry under "sub_threads" as a ShareSubThread, never under
	// "comments", where crit-web would take it for a line comment.
	ParentExternalID string `json:"-"`
}

// ShareSubThread is a sub-thread in a share payload, under the "sub_threads"
// key: a comment on a passage (Quote, at QuoteOffset in the rendered body) of
// the shared message ParentExternalID — a comment, another sub-thread or a
// reply of the payload. crit-web places it with that message's thread; it
// has no lines. A crit-web that predates sub-threads ignores the key, so the
// share succeeds there without them.
type ShareSubThread struct {
	ExternalID       string       `json:"external_id"`
	ParentExternalID string       `json:"parent_external_id"`
	Body             string       `json:"body"`
	Quote            string       `json:"quote,omitempty"`
	QuoteOffset      *int         `json:"quote_offset,omitempty"`
	Author           string       `json:"author_display_name,omitempty"`
	UserID           string       `json:"user_id,omitempty"`
	ReviewRound      int          `json:"review_round,omitempty"`
	Resolved         bool         `json:"resolved"`
	Replies          []ShareReply `json:"replies,omitempty"`
}

// splitSubThreads routes the sub-threads of comments to the payload's
// "sub_threads" key. Both lists are non-nil, so they marshal as [] rather
// than null: an explicit empty list tells crit-web the share has none.
func splitSubThreads(comments []ShareComment) ([]ShareComment, []ShareSubThread) {
	roots := []ShareComment{}
	subThreads := []ShareSubThread{}
	for _, c := range comments {
		if c.ParentExternalID == "" {
			roots = append(roots, c)
			continue
		}
		subThreads = append(subThreads, ShareSubThread{
			ExternalID:       c.ExternalID,
			ParentExternalID: c.ParentExternalID,
			Body:             c.Body,
			Quote:            c.Quote,
			QuoteOffset:      c.QuoteOffset,
			Author:           c.Author,
			UserID:           c.UserID,
			ReviewRound:      c.ReviewRound,
			Resolved:         c.Resolved,
			Replies:          c.Replies,
		})
	}
	return roots, subThreads
}

// shareFileEntries serializes ShareFile values into the JSON-friendly maps
// used by both POST and PUT payloads. Keeping a single helper prevents the
// two sites from drifting out of sync as new optional fields land.
func shareFileEntries(files []ShareFile) []map[string]any {
	entries := make([]map[string]any, len(files))
	for i, f := range files {
		entry := map[string]any{"path": f.Path, "content": f.Content}
		if f.Status != "" {
			entry["status"] = f.Status
		}
		if f.Generated {
			entry["generated"] = true
		}
		if f.Encoding != "" {
			entry["encoding"] = f.Encoding
		}
		entries[i] = entry
	}
	return entries
}

// buildSharePayload constructs the JSON payload for POST /api/reviews.
func BuildSharePayload(files []ShareFile, comments []ShareComment, reviewRound int, cliArgs []string, org, visibility, reviewType string) map[string]any {
	fileList := shareFileEntries(files)
	roots, subThreads := splitSubThreads(comments)
	payload := map[string]any{
		"files":        fileList,
		"review_round": reviewRound,
		"comments":     roots,
		"sub_threads":  subThreads,
	}
	if len(cliArgs) > 0 {
		payload["cli_args"] = cliArgs
	}
	if org != "" {
		payload["org"] = org
	}
	if visibility != "" {
		payload["visibility"] = visibility
	}
	if reviewType != "" {
		payload["review_type"] = reviewType
	}
	return payload
}

// BuildUpsertPayload constructs the JSON payload for PUT /api/reviews/:token.
func BuildUpsertPayload(files []ShareFile, comments []ShareComment, deleteToken string, reviewRound int, cliArgs []string) map[string]any {
	fileList := shareFileEntries(files)
	roots, subThreads := splitSubThreads(comments)
	payload := map[string]any{
		"delete_token": deleteToken,
		"files":        fileList,
		"comments":     roots,
		"sub_threads":  subThreads,
		"review_round": reviewRound,
	}
	if len(cliArgs) > 0 {
		payload["cli_args"] = cliArgs
	}
	return payload
}

// ShareReviewFilesResult is the outcome of a fresh share upload.
type ShareReviewFilesResult struct {
	URL         string
	DeleteToken string
	ReviewRound int
	Comments    []ShareComment
}

// LoadPreviewShareComments loads ALL of a preview session's comments and re-keys
// them to entryPath (the crawled HTML's payload path, see
// session.PreviewEntryPath). Preview DOM pins live in separate "live-route"
// FileEntries keyed by the iframe pathname (/preview-content), distinct from the
// previewed HTML's "code" entry — so comments must be loaded across EVERY
// session path (sessionPaths = Session.FilePathsSnapshot()), not just the HTML's,
// then collapsed onto the single crawl entry. Used by the proxy preview-payload
// and re-share upsert-payload builders; handleShare gets the same effect by
// passing all session paths through shareReviewFiles.
func LoadPreviewShareComments(critPath string, sessionPaths []string, fallbackAuthor, entryPath string) ([]ShareComment, int) {
	comments, reviewRound := LoadCommentsForShare(critPath, previewCommentPaths(sessionPaths, entryPath), fallbackAuthor)
	remapPreviewCommentFiles(comments, entryPath)
	return comments, reviewRound
}

// LoadShareComments loads the comments for a share payload. For a preview it
// loads across sessionPaths and re-keys them onto the crawl entry (always the
// first crawled file, files[0]) so crit-web attaches them. Otherwise it loads
// the comments for filePaths.
func LoadShareComments(critPath string, files []ShareFile, sessionPaths, filePaths []string, fallbackAuthor, reviewType string) ([]ShareComment, int) {
	if reviewType == "preview" && len(files) > 0 {
		return LoadPreviewShareComments(critPath, sessionPaths, fallbackAuthor, files[0].Path)
	}
	return LoadCommentsForShare(critPath, filePaths, fallbackAuthor)
}

// ShareFilePaths returns the Path of each file, in order.
func ShareFilePaths(files []ShareFile) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}

// legacyPreviewEntryKey is the payload path older CLIs used for every preview
// entry HTML. Comments pulled from those shares are stored under it locally.
const legacyPreviewEntryKey = "index.html"

// previewCommentPaths adds the keys pulled crit-web comments are stored under
// to the session paths: the entry path (which differs from the session path
// when the file is outside the session root) and the legacy "index.html".
// Without them, pulled comments never load and a re-share deletes them on
// crit-web, which replaces the comment set with what the CLI sends.
func previewCommentPaths(sessionPaths []string, entryPath string) []string {
	paths := make([]string, 0, len(sessionPaths)+2)
	paths = append(paths, sessionPaths...)
	return append(paths, entryPath, legacyPreviewEntryKey)
}

// remapPreviewCommentFiles re-keys per-file comments to entryPath so they attach to the crawled HTML entry in a preview share payload. Review-level
// comments (empty File) are left untouched. Preview is a single rendered page,
// so every per-file comment (including DOM pins on live-route entries) collapses
// onto the one previewed HTML.
func remapPreviewCommentFiles(comments []ShareComment, entryPath string) {
	for i := range comments {
		if comments[i].File != "" {
			comments[i].File = entryPath
		}
	}
}

// shareReviewFiles loads comments + cli_args from the review file at critPath
// and POSTs the files to crit-web. Used by both the CLI (`crit share`) and the
// server's POST /api/share endpoint so payload wiring stays in one place.
func ShareReviewFiles(critPath string, files []ShareFile, filePaths []string, svcURL, authToken, fallbackAuthor, org, visibility, reviewType string) (ShareReviewFilesResult, error) {
	return ShareReviewFilesWithCLIArgs(
		critPath, files, filePaths, svcURL, authToken, fallbackAuthor,
		LoadCliArgsFromReviewFile(critPath), org, visibility, reviewType,
	)
}

// ShareReviewFilesWithCLIArgs is ShareReviewFiles with caller-supplied CLI
// metadata. Server-backed sessions use it so a first share can preserve the
// live session arguments before review.json has been written.
func ShareReviewFilesWithCLIArgs(critPath string, files []ShareFile, filePaths []string, svcURL, authToken, fallbackAuthor string, cliArgs []string, org, visibility, reviewType string) (ShareReviewFilesResult, error) {
	// For a preview, filePaths are the session's on-disk paths, which is where
	// the comments are stored.
	comments, reviewRound := LoadShareComments(critPath, files, filePaths, filePaths, fallbackAuthor, reviewType)

	url, deleteToken, err := shareFilesToWeb(files, comments, svcURL, reviewRound, authToken, cliArgs, org, visibility, reviewType)
	if err != nil {
		return ShareReviewFilesResult{}, err
	}
	return ShareReviewFilesResult{
		URL:         url,
		DeleteToken: deleteToken,
		ReviewRound: reviewRound,
		Comments:    comments,
	}, nil
}

// shareFilesToWeb uploads files to a crit-web instance and returns the share URL and delete token.
func shareFilesToWeb(files []ShareFile, comments []ShareComment, shareURL string, reviewRound int, authToken string, cliArgs []string, org, visibility, reviewType string) (string, string, error) {
	payload := BuildSharePayload(files, comments, reviewRound, cliArgs, org, visibility, reviewType)
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("marshaling payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, shareURL+"/api/reviews", bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	SetBearer(req, authToken)

	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: config.SameOriginRedirectPolicy}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("posting to share service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", "", ErrShareUnauthorized
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		var errBody struct {
			Error string `json:"error"`
		}
		if decErr := decodeJSONOrHTMLHint(resp, &errBody); decErr != nil {
			return "", "", decErr
		}
		if errBody.Error != "" {
			return "", "", fmt.Errorf("share service error: %s", errBody.Error)
		}
		return "", "", fmt.Errorf("share service returned status %d", resp.StatusCode)
	}

	var result struct {
		URL         string `json:"url"`
		DeleteToken string `json:"delete_token"`
	}
	if err := decodeJSONOrHTMLHint(resp, &result); err != nil {
		return "", "", err
	}
	return result.URL, result.DeleteToken, nil
}

// loadCliArgsFromReviewFile reads the review file and returns the stored cli_args.
// A missing file is treated as "no args"; other read or unmarshal errors are
// logged to stderr so they don't silently disappear.
func LoadCliArgsFromReviewFile(critPath string) []string {
	data, err := session.ReadFileShared(session.ReviewPathsFor(critPath).Review)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "warning: failed to load cli_args from review file: %v\n", err)
		}
		return nil
	}
	var cj session.CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to load cli_args from review file: %v\n", err)
		return nil
	}
	return cj.CliArgs
}

// unpublishFromWeb deletes a shared review from a crit-web instance.
// Returns nil if the review was deleted or was already gone (idempotent).
func UnpublishFromWeb(shareURL string, deleteToken string, authToken string) error {
	body, _ := json.Marshal(map[string]string{"delete_token": deleteToken})
	req, err := http.NewRequest(http.MethodDelete, shareURL+"/api/reviews", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	SetBearer(req, authToken)

	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: config.SameOriginRedirectPolicy}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("contacting share service: %w", err)
	}
	defer resp.Body.Close()

	// 204 = deleted, 404 = already gone — both are success
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil
	}

	var errBody struct {
		Error string `json:"error"`
	}
	if decErr := decodeJSONOrHTMLHint(resp, &errBody); decErr != nil {
		return decErr
	}
	if errBody.Error != "" {
		return fmt.Errorf("share service error: %s", errBody.Error)
	}
	return fmt.Errorf("share service returned status %d", resp.StatusCode)
}

// decodeJSONOrHTMLHint reads up to 10MB from resp.Body, detects HTML
// responses (typical of an SSO reverse proxy intercepting the request with a
// login page), and either decodes the JSON into v or returns an actionable
// error pointing the user at proxy_auth=true.
//
// Replaces direct json.NewDecoder(resp.Body).Decode(&v) at every share
// network call site so the SSO failure path produces a meaningful error
// instead of a cryptic "invalid character '<'".
func decodeJSONOrHTMLHint(resp *http.Response, v any) error {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if bytes.HasPrefix(trimmed, []byte("<")) {
		return fmt.Errorf("crit-web returned an HTML page instead of JSON — likely behind an SSO reverse proxy. " +
			"Set 'proxy_auth': true in your crit config and use the browser UI to share")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("decode share response: %w", err)
	}
	return nil
}

// SetBearer sets the Authorization header to "Bearer <token>" when token is non-empty.
func SetBearer(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// loadCommentsForShare reads the review file at critPath and returns unresolved
// ShareComment entries for the given file paths, plus the review round.
// fallbackAuthor is used when a comment has no Author set (typically cfg.Author).
//
// The entries include the sub-threads (ParentExternalID set), which the
// payload builders send under "sub_threads"; see sharedThreadIndexes for the
// resolved threads a share still sends to keep an open sub-thread anchored.
//
// ExternalID is set so the server persists it on first insert and matches it
// on subsequent upserts. This enables round-trip carry-forward of `user_id`:
// replace_comments matches incoming attrs.external_id against the
// existing-by-external_id map and preserves the verified user_id when the
// re-sharer is anonymous (rules #3/#4 of the attribution table).
//
// Used for both the initial share and the upsert path — they need identical
// payloads, so they share this loader.
func LoadCommentsForShare(critPath string, filePaths []string, fallbackAuthor string) ([]ShareComment, int) {
	return loadCommentsFromCritJSON(critPath, filePaths, false, true, fallbackAuthor)
}

// commentToShareComment converts a session.Comment into a ShareComment, applying the
// includeResolved and setExternalID flags. The filePath and scope fields are
// set by the caller based on context. fallbackAuthor is used when c.Author is
// empty (typically cfg.Author).
//
// critPath is the v4 review identity (folder); it's used to locate the
// attachments dir so any attachments/<uuid>.<ext> markdown references in
// the body (or its replies) get rewritten to data: URIs before the share
// payload leaves the host. crit-web has no asset endpoint, so this
// inlining is the only way pasted screenshots survive a share. Passing
// "" disables inlining (used in unit tests that don't write attachments
// to disk).
func commentToShareComment(c session.Comment, filePath, scope, fallbackAuthor, critPath string, includeResolved, setExternalID bool) ShareComment {
	author := c.Author
	if author == "" {
		author = fallbackAuthor
	}
	sc := ShareComment{
		File:        filePath,
		StartLine:   c.StartLine,
		EndLine:     c.EndLine,
		Body:        session.InlineAttachmentsAsDataURIs(critPath, c.Body),
		Quote:       c.Quote,
		Author:      author,
		UserID:      c.UserID,
		Scope:       scope,
		GitHubID:    c.GitHubID,
		DOMAnchor:   c.DOMAnchor,
		QuoteOffset: c.QuoteOffset,
	}
	if includeResolved {
		sc.Resolved = c.Resolved
	}
	if setExternalID {
		sc.ExternalID = c.ID
	}
	if c.ReviewRound >= 1 {
		sc.ReviewRound = c.ReviewRound
	}
	for _, r := range c.Replies {
		ra := r.Author
		if ra == "" {
			ra = fallbackAuthor
		}
		sr := ShareReply{
			Body:     session.InlineAttachmentsAsDataURIs(critPath, r.Body),
			Author:   ra,
			UserID:   r.UserID,
			GitHubID: r.GitHubID,
		}
		if setExternalID {
			sr.ExternalID = r.ID
		}
		sc.Replies = append(sc.Replies, sr)
	}
	return sc
}

// loadCommentsFromCritJSON reads the review file at critPath and returns ShareComment
// entries for the given file paths, plus the review round. When includeResolved is true,
// resolved comments are included. When setExternalID is true, ExternalID is set
// from the local comment ID for round-trip tracking. fallbackAuthor fills missing
// Author fields (typically cfg.Author).
//
// Sub-threads come with ParentExternalID set, for the payload builders to
// route them under "sub_threads": in "comments", crit-web would take them for
// line comments.
func loadCommentsFromCritJSON(critPath string, filePaths []string, includeResolved, setExternalID bool, fallbackAuthor string) ([]ShareComment, int) {
	data, err := session.ReadFileShared(session.ReviewPathsFor(critPath).Review)
	if err != nil {
		return nil, 1
	}
	var cj session.CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return nil, 1
	}

	round := cj.ReviewRound
	if round < 1 {
		round = 1
	}

	pathSet := make(map[string]bool, len(filePaths))
	for _, p := range filePaths {
		pathSet[p] = true
	}

	var comments []ShareComment
	add := func(list []session.Comment, filePath, listScope string) {
		shared := sharedThreadIndexes(list, includeResolved)
		for i, c := range list {
			if !shared[i] {
				continue
			}
			scope := listScope
			if scope == "" {
				scope = c.Scope
			}
			sc := commentToShareComment(c, filePath, scope, fallbackAuthor, critPath, includeResolved, setExternalID)
			// A resolved thread is only shared to hold an open sub-thread:
			// it goes as resolved.
			sc.Resolved = c.Resolved
			sc.ParentExternalID = c.ParentID
			comments = append(comments, sc)
		}
	}
	for filePath, cf := range cj.Files {
		if pathSet[filePath] {
			add(cf.Comments, filePath, "")
		}
	}
	add(cj.ReviewComments, "", "review")
	return comments, round
}

// sharedThreadIndexes returns which comments of one list (a file's comments
// or the review-level comments) a share sends: every unresolved thread and
// sub-thread (every one, when includeResolved), plus every resolved thread or
// sub-thread an open sub-thread hangs from. crit shows such a parent while the
// sub-thread is open, and crit-web needs it as the anchor: without it, the
// sub-thread would vanish from the share. A sub-thread whose chain of parents
// does not reach a root thread of the list is left out, as nothing places it.
func sharedThreadIndexes(comments []session.Comment, includeResolved bool) []bool {
	owner := make(map[string]int, len(comments))
	for i, c := range comments {
		owner[c.ID] = i
		for _, r := range c.Replies {
			owner[r.ID] = i
		}
	}
	shared := make([]bool, len(comments))
	for i, c := range comments {
		if c.Resolved && !includeResolved {
			continue
		}
		if c.ParentID == "" {
			shared[i] = true
			continue
		}
		if chain, ok := parentThreads(comments, owner, i); ok {
			shared[i] = true
			for _, j := range chain {
				shared[j] = true
			}
		}
	}
	return shared
}

// parentThreads returns the indexes of the threads sub-thread i hangs from,
// up to its root thread, and false when the chain breaks or loops.
func parentThreads(comments []session.Comment, owner map[string]int, i int) ([]int, bool) {
	var chain []int
	seen := map[int]bool{i: true}
	for comments[i].ParentID != "" {
		j, ok := owner[comments[i].ParentID]
		if !ok || seen[j] {
			return nil, false
		}
		seen[j] = true
		chain = append(chain, j)
		i = j
	}
	return chain, true
}

// WebReply is the shape of a reply nested inside a WebComment.
//
// ExternalID is the local reply id the reply was shared under: empty for a
// reply written on crit-web, or one crit-web predating it does not return.
// SubThreads are the sub-threads on a passage of this reply.
type WebReply struct {
	Body              string       `json:"body"`
	AuthorDisplayName string       `json:"author_display_name"`
	UserID            string       `json:"user_id"`
	ExternalID        string       `json:"external_id,omitempty"`
	SubThreads        []WebComment `json:"sub_threads,omitempty"`
}

// WebComment is the shape of a comment returned by GET /api/reviews/:token/comments.
// AuthorIdentity is retained for compatibility with existing crit-web responses
// and is treated as a session-owner token for anonymous web visitors. UserID
// is the verified user id, set when the comment was authored by a logged-in
// user (either CLI with bearer token or LiveView while signed in).
//
// crit-web lists only threads at the top level. Each message carries the
// sub-threads on its passages under SubThreads (here and on each WebReply),
// themselves WebComments, at any depth; a crit that predates them ignores the
// key. ID is the crit-web row id, which ties a thread imported in a merge to
// the sub-threads under it.
type WebComment struct {
	ID                string             `json:"id,omitempty"`
	Body              string             `json:"body"`
	FilePath          string             `json:"file_path"`
	StartLine         int                `json:"start_line"`
	EndLine           int                `json:"end_line"`
	ReviewRound       int                `json:"review_round"`
	Resolved          bool               `json:"resolved"`
	ResolvedRound     int                `json:"resolved_round"`
	ExternalID        string             `json:"external_id"`
	AuthorDisplayName string             `json:"author_display_name"`
	AuthorIdentity    string             `json:"author_identity"`
	UserID            string             `json:"user_id"`
	Quote             string             `json:"quote"`
	QuoteOffset       *int               `json:"quote_offset,omitempty"`
	Scope             string             `json:"scope"`
	Replies           []WebReply         `json:"replies"`
	DOMAnchor         *session.DOMAnchor `json:"dom_anchor,omitempty"`
	Anchor            string             `json:"anchor,omitempty"`
	Drifted           bool               `json:"drifted,omitempty"`
	SubThreads        []WebComment       `json:"sub_threads,omitempty"`
}

// ThreadsWithSubThreads returns the threads of incoming that carry a
// sub-thread, on themselves or on a reply: what mergeWebSubThreadsInto walks.
func ThreadsWithSubThreads(incoming []WebComment) []WebComment {
	var out []WebComment
	for _, wc := range incoming {
		if hasSubThreads(wc) {
			out = append(out, wc)
		}
	}
	return out
}

func hasSubThreads(wc WebComment) bool {
	if len(wc.SubThreads) > 0 {
		return true
	}
	for _, wr := range wc.Replies {
		if len(wr.SubThreads) > 0 {
			return true
		}
	}
	return false
}

// buildLocalFingerprintIndex returns both the fingerprint set and a map from
// fingerprint to local comment ID. The ID map lets callers look up the local
// comment when a web comment matches by fingerprint (so replies can be merged
// instead of dropped). Sub-threads are left out: they are never shared, and
// their lines (0) would match a file-level web comment with the same body.
func buildLocalFingerprintIndex(cj session.CritJSON) (map[string]bool, map[string]string) {
	fps := make(map[string]bool)
	ids := make(map[string]string)
	for path, f := range cj.Files {
		for _, c := range f.Comments {
			if c.ParentID != "" {
				continue
			}
			key := fmt.Sprintf("%s|%s|%d|%d", c.Body, path, c.StartLine, c.EndLine)
			fps[key] = true
			if c.ID != "" {
				ids[key] = c.ID
			}
		}
	}
	for _, c := range cj.ReviewComments {
		if c.ParentID != "" {
			continue
		}
		key := c.Body + "||0|0"
		fps[key] = true
		if c.ID != "" {
			ids[key] = c.ID
		}
	}
	return fps, ids
}

// fetchWebCommentsResult holds both new comments and reply updates for existing ones.
type fetchWebCommentsResult struct {
	NewComments  []WebComment
	ReplyUpdates map[string][]WebReply      // external_id -> replies from web
	Placements   map[string]session.Comment // local comment ID -> carried line, anchor, drifted
	// Threads are the fetched threads that carry sub-threads. Their sub-threads
	// are deduplicated and imported at merge time (mergeWebSubThreadsInto),
	// against the review file being written.
	Threads []WebComment
}

// fetchWebComments fetches all comments from crit-web, returning new comments
// and reply updates for existing comments that have replies added on the web.
// localFingerprintIDs maps body+file+line fingerprints to the local comment ID,
// so that web comments matching a previously-imported web-N comment by
// fingerprint can have their replies merged instead of dropped.
func fetchWebComments(shareURL string, localIDs map[string]bool, localFingerprints map[string]bool, localFingerprintIDs map[string]string, authToken string) (fetchWebCommentsResult, error) {
	return fetchWebCommentsFromTarget(shareURL, "", localIDs, localFingerprints, localFingerprintIDs, authToken, nil)
}

// fetchWebCommentsForReview fetches web comments for the review in cj and
// also reports placements crit-web carried for local comments. A placement
// is only taken for a comment still on the lines it was last shared with
// (cj.SharedLines); a comment the local carry moved since keeps its local
// placement.
func fetchWebCommentsForReview(shareURL, shareBaseURL string, cj session.CritJSON, authToken string) (fetchWebCommentsResult, error) {
	localIDs := buildLocalIDSet(cj)
	localFingerprints, localFingerprintIDs := buildLocalFingerprintIndex(cj)
	return fetchWebCommentsFromTarget(shareURL, shareBaseURL, localIDs, localFingerprints, localFingerprintIDs, authToken, localCommentsByID(cj))
}

func fetchWebCommentsFromTarget(shareURL, shareBaseURL string, localIDs map[string]bool, localFingerprints map[string]bool, localFingerprintIDs map[string]string, authToken string, locals map[string]session.Comment) (fetchWebCommentsResult, error) {
	var result fetchWebCommentsResult
	result.ReplyUpdates = make(map[string][]WebReply)
	result.Placements = make(map[string]session.Comment)

	token := TokenFromHostedURL(shareURL)
	u, err := url.Parse(shareURL)
	if err != nil {
		return result, fmt.Errorf("invalid share URL: %w", err)
	}
	if token == "" {
		return result, fmt.Errorf("invalid share URL: missing /r/<token> path")
	}
	if shareBaseURL == "" {
		shareBaseURL = u.Scheme + "://" + u.Host
	}
	apiURL := strings.TrimRight(shareBaseURL, "/") + "/api/reviews/" + token + "/comments"

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return result, fmt.Errorf("creating request: %w", err)
	}
	SetBearer(req, authToken)

	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: config.SameOriginRedirectPolicy}
	resp, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("fetching remote comments: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return result, ErrShareNotFound
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return result, ErrShareUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("remote comments returned status %d", resp.StatusCode)
	}

	var all []WebComment
	if err := decodeJSONOrHTMLHint(resp, &all); err != nil {
		return result, err
	}

	for _, wc := range all {
		if dropDuplicateWebComment(wc, localIDs, localFingerprints, localFingerprintIDs, result.ReplyUpdates) {
			if updated, ok := carriedPlacement(wc, locals); ok {
				result.Placements[wc.ExternalID] = updated
			}
			continue
		}
		result.NewComments = append(result.NewComments, wc)
	}
	result.Threads = ThreadsWithSubThreads(all)
	return result, nil
}

// carriedPlacement reports a local comment whose shared copy has moved or
// been marked drifted. The local id is the external_id the CLI sent.
func carriedPlacement(wc WebComment, locals map[string]session.Comment) (session.Comment, bool) {
	if wc.ExternalID == "" || locals == nil {
		return session.Comment{}, false
	}
	local, ok := locals[wc.ExternalID]
	if !ok {
		return session.Comment{}, false
	}
	if local.StartLine == wc.StartLine && local.EndLine == wc.EndLine && local.Anchor == wc.Anchor && local.Drifted == wc.Drifted {
		return session.Comment{}, false
	}
	local.StartLine = wc.StartLine
	local.EndLine = wc.EndLine
	local.Anchor = wc.Anchor
	local.Drifted = wc.Drifted
	return local, true
}

// localCommentsByID indexes the local comments that crit-web may place: those
// still on the lines they were last shared with. Comments with no record
// (no id, never shared, or a review shared before SharedLines existed) are
// left out, so they keep their local placement.
func localCommentsByID(cj session.CritJSON) map[string]session.Comment {
	out := make(map[string]session.Comment)
	add := func(c session.Comment) {
		shared, ok := cj.SharedLines[c.ID]
		if c.ID == "" || !ok || shared.StartLine != c.StartLine || shared.EndLine != c.EndLine {
			return
		}
		out[c.ID] = c
	}
	for _, f := range cj.Files {
		for _, c := range f.Comments {
			add(c)
		}
	}
	for _, c := range cj.ReviewComments {
		add(c)
	}
	return out
}

// sharedLines records the lines each comment was sent with, keyed by the
// local comment id (the external_id crit-web stores).
func sharedLines(comments []ShareComment) map[string]session.SharedLine {
	out := make(map[string]session.SharedLine, len(comments))
	for _, c := range comments {
		// A sub-thread has no lines for crit-web to carry.
		if c.ExternalID != "" && c.ParentExternalID == "" {
			out[c.ExternalID] = session.SharedLine{StartLine: c.StartLine, EndLine: c.EndLine}
		}
	}
	return out
}

// dropDuplicateWebComment returns true if wc is already represented locally
// (by external_id or fingerprint match) and therefore should not be appended
// to NewComments. Any new replies on the duplicate are recorded in updates.
func dropDuplicateWebComment(
	wc WebComment,
	localIDs map[string]bool,
	localFingerprints map[string]bool,
	localFingerprintIDs map[string]string,
	updates map[string][]WebReply,
) bool {
	if wc.ExternalID != "" && localIDs[wc.ExternalID] {
		if len(wc.Replies) > 0 {
			updates[wc.ExternalID] = wc.Replies
		}
		return true
	}
	if wc.ExternalID != "" {
		return false
	}
	fp := fmt.Sprintf("%s|%s|%d|%d", wc.Body, wc.FilePath, wc.StartLine, wc.EndLine)
	if !localFingerprints[fp] {
		return false
	}
	if localID, ok := localFingerprintIDs[fp]; ok && len(wc.Replies) > 0 {
		updates[localID] = wc.Replies
	}
	return true
}

// upsertResult holds the response from an upsert (PUT) to crit-web.
// Sent is false when the hash matched and no PUT was made, so crit-web
// still holds the comments from the share before.
type upsertResult struct {
	URL         string
	ReviewRound int
	Changed     bool
	Sent        bool
}

// upsertShareToWeb pushes an updated review to crit-web via PUT.
// If the content hash matches LastShareHash (no changes), returns without calling PUT.
func upsertShareToWeb(cfg session.CritJSON, files []ShareFile, comments []ShareComment, authToken string) (upsertResult, error) {
	result := upsertResult{URL: cfg.ShareURL, ReviewRound: cfg.ReviewRound}

	currentHash := computeShareHash(files, comments)
	if currentHash == cfg.LastShareHash {
		return result, nil // nothing changed
	}

	token := TokenFromHostedURL(cfg.ShareURL)
	u, err := url.Parse(cfg.ShareURL)
	if err != nil {
		return result, fmt.Errorf("invalid share URL: %w", err)
	}
	if token == "" {
		return result, fmt.Errorf("invalid share URL: missing /r/<token> path")
	}
	baseURL := cfg.ShareBaseURL
	if baseURL == "" {
		baseURL = u.Scheme + "://" + u.Host
	}
	apiURL := strings.TrimRight(baseURL, "/") + "/api/reviews/" + token

	payload := BuildUpsertPayload(files, comments, cfg.DeleteToken, cfg.ReviewRound, cfg.CliArgs)

	body, err := json.Marshal(payload)
	if err != nil {
		return result, fmt.Errorf("marshaling upsert payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, apiURL, bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	SetBearer(req, authToken)

	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: config.SameOriginRedirectPolicy}
	resp, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("PUT %s: %w", apiURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return result, ErrShareUnauthorized
	}
	if resp.StatusCode == http.StatusNotFound {
		return result, ErrShareNotFound
	}
	if resp.StatusCode >= 400 {
		return result, fmt.Errorf("upsert failed with status %d", resp.StatusCode)
	}

	var respBody struct {
		URL         string `json:"url"`
		ReviewRound int    `json:"review_round"`
		Changed     bool   `json:"changed"`
	}
	if err := decodeJSONOrHTMLHint(resp, &respBody); err != nil {
		return result, err
	}

	result.Sent = true
	result.Changed = respBody.Changed
	result.ReviewRound = respBody.ReviewRound
	if respBody.URL != "" {
		result.URL = respBody.URL
	}
	return result, nil
}

// loadExistingShareCfg returns the full session.CritJSON if a matching share exists
// (same file scope). The bool is true only when an existing share is found.
// A missing review file is reported as (zero, false, nil) so callers can fall
// back to creating a new share. Parse errors and other I/O errors are surfaced
// so callers can refuse to clobber a corrupted file.
func loadExistingShareCfg(critPath string, paths []string) (session.CritJSON, bool, error) {
	data, err := session.ReadFileShared(session.ReviewPathsFor(critPath).Review)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return session.CritJSON{}, false, nil
		}
		return session.CritJSON{}, false, fmt.Errorf("reading review file %q: %w", critPath, err)
	}
	var cj session.CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return session.CritJSON{}, false, fmt.Errorf("parsing review file %q: %w", critPath, err)
	}
	if cj.ShareURL == "" {
		return session.CritJSON{}, false, nil
	}
	if cj.ShareScope != "" && cj.ShareScope != ShareScope(paths) {
		return session.CritJSON{}, false, nil
	}
	return cj, true, nil
}

// DedupWebComments filters incoming web comments against the local review state.
func DedupWebComments(cj session.CritJSON, incoming []WebComment) ([]WebComment, map[string][]WebReply) {
	localIDs := buildLocalIDSet(cj)
	localFingerprints, localFingerprintIDs := buildLocalFingerprintIndex(cj)
	replyUpdates := make(map[string][]WebReply)

	var newComments []WebComment
	for _, wc := range incoming {
		if dropDuplicateWebComment(wc, localIDs, localFingerprints, localFingerprintIDs, replyUpdates) {
			continue
		}
		newComments = append(newComments, wc)
	}
	return newComments, replyUpdates
}

// buildLocalIDSet collects all local comment IDs across all files and review comments.
func buildLocalIDSet(cj session.CritJSON) map[string]bool {
	ids := make(map[string]bool)
	for _, f := range cj.Files {
		for _, c := range f.Comments {
			if c.ID != "" {
				ids[c.ID] = true
			}
		}
	}
	for _, c := range cj.ReviewComments {
		if c.ID != "" {
			ids[c.ID] = true
		}
	}
	return ids
}

// highestWebIndex returns the highest numeric suffix among "web-N" comment IDs
// in a session.CritJSON structure. This ensures new web comment IDs are globally unique.
func highestWebIndex(cj session.CritJSON) int {
	max := 0
	for _, f := range cj.Files {
		for _, c := range f.Comments {
			if strings.HasPrefix(c.ID, "web-") {
				if n, err := strconv.Atoi(strings.TrimPrefix(c.ID, "web-")); err == nil && n > max {
					max = n
				}
			}
		}
	}
	for _, c := range cj.ReviewComments {
		if strings.HasPrefix(c.ID, "web-") {
			if n, err := strconv.Atoi(strings.TrimPrefix(c.ID, "web-")); err == nil && n > max {
				max = n
			}
		}
	}
	return max
}

// mergeWebComments adds web-reviewer comments into the review file under their respective
// files or into review_comments for review-level (scope:"review") comments.
// It also merges reply updates for existing comments identified by external_id,
// and imports the sub-threads of threads (see mergeWebSubThreadsInto).
// Pass nil for replyUpdates to skip reply merging. Returns the number of
// sub-threads added.
//
// When the active focus is a range mode (probed from a running daemon, or
// inferred from the on-disk ActiveDiffScope), imported comments are stamped
// with HeadSHA + DiffScope=layer so they pass visibleInFocus in range view.
// See spec §E "Write path — `mergeWebComments`".
func MergeWebComments(critPath string, newComments []WebComment, replyUpdates map[string][]WebReply, threads []WebComment) (int, error) {
	return MergeWebCommentsFor(critPath, newComments, replyUpdates, threads, session.ProbeDaemonFocus())
}

// MergeWebCommentsFor is MergeWebComments with the daemon focus already
// known. The daemon passes its own focus rather than probing itself over HTTP.
func MergeWebCommentsFor(critPath string, newComments []WebComment, replyUpdates map[string][]WebReply, threads []WebComment, daemonFocus *focus.Focus) (int, error) {
	added := 0
	err := updateReviewFileIf(critPath, func(cj *session.CritJSON) bool {
		var changed bool
		added, changed = mergeWebCommentsInto(cj, newComments, replyUpdates, threads, daemonFocus)
		return changed || len(newComments) > 0 || len(replyUpdates) > 0
	})
	return added, err
}

// updateReviewFile loads the review file, applies mutate, stamps UpdatedAt,
// and saves it, all under the review write lock. A missing review file is an
// error. A missing review folder is returned before the lock would create it.
func updateReviewFile(critPath string, mutate func(*session.CritJSON)) error {
	return updateReviewFileIf(critPath, func(cj *session.CritJSON) bool {
		mutate(cj)
		return true
	})
}

// updateReviewFileIf is updateReviewFile, but saves only when mutate returns true.
func updateReviewFileIf(critPath string, mutate func(*session.CritJSON) bool) error {
	if _, err := os.Stat(session.ReviewPathsFor(critPath).Folder); err != nil {
		return err
	}
	return session.WithReviewWriteLock(critPath, func() error {
		data, err := session.ReadFileShared(session.ReviewPathsFor(critPath).Review)
		if err != nil {
			return err
		}
		var cj session.CritJSON
		if err := json.Unmarshal(data, &cj); err != nil {
			return err
		}
		if cj.Files == nil {
			cj.Files = make(map[string]session.CritJSONFile)
		}
		if !mutate(&cj) {
			return nil
		}
		cj.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		return session.SaveCritJSON(critPath, cj)
	})
}

// mergeWebCommentsInto merges one fetch into cj: the new comments, the reply
// updates, then the sub-threads of threads. daemonFocus comes from
// session.ProbeDaemonFocus, called before the review lock because it makes an
// HTTP call. Returns the number of sub-threads added and whether cj changed.
func mergeWebCommentsInto(cj *session.CritJSON, newComments []WebComment, replyUpdates map[string][]WebReply, threads []WebComment, daemonFocus *focus.Focus) (int, bool) {
	// Find the highest existing web-N index so new IDs are globally unique
	// even if earlier ones were deleted from the review file.
	webCount := highestWebIndex(*cj)

	scope := session.PullScopeFor(daemonFocus, cj)

	// The crit-web id of each comment imported here, to its local id: the
	// sub-threads under it anchor on that.
	imported := make(map[string]string, len(newComments))
	now := time.Now().UTC().Format(time.RFC3339)
	for _, wc := range newComments {
		webCount++
		c := session.StampWithFocus(session.Comment{
			ID:            fmt.Sprintf("web-%d", webCount),
			StartLine:     wc.StartLine,
			EndLine:       wc.EndLine,
			Body:          wc.Body,
			Quote:         wc.Quote,
			QuoteOffset:   wc.QuoteOffset,
			Anchor:        wc.Anchor,
			Drifted:       wc.Drifted,
			Author:        wc.AuthorDisplayName,
			UserID:        wc.UserID,
			Scope:         wc.Scope,
			ReviewRound:   wc.ReviewRound,
			Resolved:      wc.Resolved,
			ResolvedRound: wc.ResolvedRound,
			DOMAnchor:     wc.DOMAnchor,
			CreatedAt:     now,
			UpdatedAt:     now,
		}, scope.AsFocus())
		mergeWebReplies(&c, wc.Replies)
		if wc.ID != "" {
			imported[wc.ID] = c.ID
		}
		if wc.Scope == "review" {
			cj.ReviewComments = append(cj.ReviewComments, c)
		} else {
			entry := cj.Files[wc.FilePath]
			entry.Comments = append(entry.Comments, c)
			cj.Files[wc.FilePath] = entry
		}
	}
	changed := len(newComments) > 0

	// Merge reply updates for existing comments (matched by external_id).
	if len(replyUpdates) > 0 {
		for filePath, cf := range cj.Files {
			for i := range cf.Comments {
				if webReplies, ok := replyUpdates[cf.Comments[i].ID]; ok && mergeWebReplies(&cf.Comments[i], webReplies) {
					changed = true
				}
			}
			cj.Files[filePath] = cf
		}
		for i := range cj.ReviewComments {
			if webReplies, ok := replyUpdates[cj.ReviewComments[i].ID]; ok && mergeWebReplies(&cj.ReviewComments[i], webReplies) {
				changed = true
			}
		}
	}

	added, subThreadsChanged := mergeWebSubThreadsInto(cj, threads, imported)
	return added, changed || subThreadsChanged
}

// applyWebCommentPlacements sets line, anchor, and drifted on the comments in
// cj that the server carried forward. Matched by the local comment id.
func applyWebCommentPlacements(cj *session.CritJSON, placements map[string]session.Comment) {
	place := func(c *session.Comment) bool {
		placed, ok := placements[c.ID]
		if !ok {
			return false
		}
		c.StartLine = placed.StartLine
		c.EndLine = placed.EndLine
		c.Anchor = placed.Anchor
		c.Drifted = placed.Drifted
		return true
	}
	for path, cf := range cj.Files {
		changed := false
		for i := range cf.Comments {
			if place(&cf.Comments[i]) {
				changed = true
			}
		}
		if changed {
			cj.Files[path] = cf
		}
	}
	for i := range cj.ReviewComments {
		place(&cj.ReviewComments[i])
	}
}

// mergeFetchedComments writes new comments, reply updates, sub-threads and
// placements from one fetch in a single load and save of the review file.
// Returns the number of sub-threads added.
func mergeFetchedComments(critPath string, fetched fetchWebCommentsResult) (int, error) {
	return mergeFetchedCommentsFor(critPath, fetched, session.ProbeDaemonFocus())
}

func mergeFetchedCommentsFor(critPath string, fetched fetchWebCommentsResult, daemonFocus *focus.Focus) (int, error) {
	if !fetchHasUpdates(fetched) && len(fetched.Threads) == 0 {
		return 0, nil
	}
	added := 0
	err := updateReviewFileIf(critPath, func(cj *session.CritJSON) bool {
		var changed bool
		added, changed = mergeWebCommentsInto(cj, fetched.NewComments, fetched.ReplyUpdates, fetched.Threads, daemonFocus)
		applyWebCommentPlacements(cj, fetched.Placements)
		return changed || fetchHasUpdates(fetched)
	})
	return added, err
}

func fetchHasUpdates(fetched fetchWebCommentsResult) bool {
	return len(fetched.NewComments) > 0 || len(fetched.ReplyUpdates) > 0 || len(fetched.Placements) > 0
}

// mergeWebReplies merges web replies into c and reports whether c changed.
// A web reply is already there when a local reply has its external id or,
// for one that never went through a share, its body (among the replies c had
// before: two equal web replies both come in). A local copy without an id
// gets one, and a new reply keeps its external id as local id when free, or
// gets a fresh one: sub-threads anchor on reply ids, which must stay the
// same from one fetch to the next.
func mergeWebReplies(c *session.Comment, webReplies []WebReply) bool {
	known := len(c.Replies)
	changed := false
	for _, wr := range webReplies {
		if j := localReplyIndex(c.Replies[:known], wr); j >= 0 {
			if c.Replies[j].ID == "" {
				c.Replies[j].ID = newReplyID(c.Replies, wr.ExternalID)
				changed = true
			}
			continue
		}
		c.Replies = append(c.Replies, session.Reply{
			ID:     newReplyID(c.Replies, wr.ExternalID),
			Body:   wr.Body,
			Author: wr.AuthorDisplayName,
			UserID: wr.UserID,
		})
		changed = true
	}
	return changed
}

// localReplyIndex returns the index of wr's local copy in replies, by
// external id, else by body, or -1.
func localReplyIndex(replies []session.Reply, wr WebReply) int {
	if wr.ExternalID != "" {
		for j, r := range replies {
			if r.ID == wr.ExternalID {
				return j
			}
		}
	}
	for j, r := range replies {
		if r.Body == wr.Body {
			return j
		}
	}
	return -1
}

// safeReplyID matches the reply ids a crit-web external id may become: made
// of letters, digits, "_" and "-", like the rp_ ids crit gives replies, so
// the id stays a plain URL path segment and HTML attribute value.
var safeReplyID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// newReplyID returns the local id of a reply imported from crit-web: its
// external id when that id is safe and free in replies, else a fresh rp_ id.
func newReplyID(replies []session.Reply, externalID string) string {
	if !safeReplyID.MatchString(externalID) {
		return session.RandomReplyID()
	}
	for _, r := range replies {
		if r.ID == externalID {
			return session.RandomReplyID()
		}
	}
	return externalID
}

// reviewListKey keys the review-level comments among the file lists of a
// subThreadImport; no file path is empty.
const reviewListKey = ""

// subThreadImport is the state of one mergeWebSubThreadsInto: the comment
// lists of the review file, keyed by file path (reviewListKey for the
// review-level ones), written back once the walk is done.
type subThreadImport struct {
	lists    map[string][]session.Comment
	webCount int
	now      string
	added    int
	changed  bool
}

// mergeWebSubThreadsInto imports into cj the sub-threads that threads, the
// threads of one fetch, carry on their messages, at any depth. It runs on the
// review file being written, after the threads themselves were merged: this
// is the sub-thread half of the dedup rule, on every import path.
//
// Each web message is mapped to its local copy. A thread: by the local id it
// was just imported under (imported, keyed by crit-web id), else by external
// id, else by fingerprint. A reply: by external id, else by body, within its
// local thread; a local reply without an id gets one. A web sub-thread
// already local, by external id or else by (parent, body, quote), only gets
// its new replies; any other is added under the local message, in the list of
// its root thread, with that thread's scope, side and view tags. A message
// with no local copy keeps its sub-threads out. Returns the number of
// sub-threads added and whether cj changed.
func mergeWebSubThreadsInto(cj *session.CritJSON, threads []WebComment, imported map[string]string) (int, bool) {
	if len(threads) == 0 {
		return 0, false
	}
	m := subThreadImport{
		lists:    map[string][]session.Comment{reviewListKey: cj.ReviewComments},
		webCount: highestWebIndex(*cj),
		now:      time.Now().UTC().Format(time.RFC3339),
	}
	for path, f := range cj.Files {
		m.lists[path] = f.Comments
	}
	_, fingerprints := buildLocalFingerprintIndex(*cj)

	for _, wt := range threads {
		id := imported[wt.ID]
		if id == "" {
			id = wt.ExternalID
		}
		if id == "" {
			id = fingerprints[fmt.Sprintf("%s|%s|%d|%d", wt.Body, wt.FilePath, wt.StartLine, wt.EndLine)]
		}
		if key, ok := m.listOf(id, wt); ok {
			m.walk(key, id, wt)
		}
	}

	if !m.changed {
		return 0, false
	}
	cj.ReviewComments = m.lists[reviewListKey]
	for path, comments := range m.lists {
		if path == reviewListKey {
			continue
		}
		entry := cj.Files[path]
		entry.Comments = comments
		cj.Files[path] = entry
	}
	return m.added, true
}

// listOf returns the key of the list holding comment id, looking first where
// crit-web files the web thread wt.
func (m *subThreadImport) listOf(id string, wt WebComment) (string, bool) {
	if id == "" {
		return "", false
	}
	preferred := wt.FilePath
	if wt.Scope == "review" {
		preferred = reviewListKey
	}
	if commentIndex(m.lists[preferred], id) >= 0 {
		return preferred, true
	}
	for key, comments := range m.lists {
		if commentIndex(comments, id) >= 0 {
			return key, true
		}
	}
	return "", false
}

func commentIndex(comments []session.Comment, id string) int {
	for i, c := range comments {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// walk imports the sub-threads of the web thread w, whose local copy is the
// comment localID of list key: those on the comment, then those on each of
// its replies.
func (m *subThreadImport) walk(key, localID string, w WebComment) {
	for _, ws := range w.SubThreads {
		m.importSubThread(key, localID, ws)
	}
	for _, wr := range w.Replies {
		if len(wr.SubThreads) == 0 {
			continue
		}
		i := commentIndex(m.lists[key], localID)
		if i < 0 {
			return
		}
		c := &m.lists[key][i]
		j := localReplyIndex(c.Replies, wr)
		if j < 0 {
			continue
		}
		if c.Replies[j].ID == "" {
			c.Replies[j].ID = newReplyID(c.Replies, wr.ExternalID)
			m.changed = true
		}
		replyID := c.Replies[j].ID
		for _, ws := range wr.SubThreads {
			m.importSubThread(key, replyID, ws)
		}
	}
}

// importSubThread imports the web sub-thread ws on the local message
// parentID of list key, then the sub-threads under it.
func (m *subThreadImport) importSubThread(key, parentID string, ws WebComment) {
	list := m.lists[key]
	i := -1
	if ws.ExternalID != "" {
		i = commentIndex(list, ws.ExternalID)
	} else {
		for j, c := range list {
			if c.ParentID == parentID && c.Body == ws.Body && c.Quote == ws.Quote {
				i = j
				break
			}
		}
	}
	if i >= 0 {
		if mergeWebReplies(&list[i], ws.Replies) {
			m.changed = true
		}
		m.walk(key, list[i].ID, ws)
		return
	}

	root, ok := session.ThreadRoot(list, parentID)
	if !ok {
		return
	}
	m.webCount++
	c := session.Comment{
		ID:            fmt.Sprintf("web-%d", m.webCount),
		ParentID:      parentID,
		Body:          ws.Body,
		Quote:         ws.Quote,
		QuoteOffset:   ws.QuoteOffset,
		Author:        ws.AuthorDisplayName,
		UserID:        ws.UserID,
		Scope:         root.Scope,
		Side:          root.Side,
		HeadSHA:       root.HeadSHA,
		DiffScope:     root.DiffScope,
		FocusKey:      root.FocusKey,
		ReviewRound:   ws.ReviewRound,
		Resolved:      ws.Resolved,
		ResolvedRound: ws.ResolvedRound,
		CreatedAt:     m.now,
		UpdatedAt:     m.now,
	}
	mergeWebReplies(&c, ws.Replies)
	m.lists[key] = append(list, c)
	m.added++
	m.changed = true
	m.walk(key, c.ID, ws)
}

// updateShareState writes LastShareHash and ReviewRound back to the review
// file after a successful share. commentsSent says whether comments went to
// crit-web in this share; only then are the shared comment lines recorded.
// When the upsert skipped the PUT, crit-web still has the lines from the
// share before, so the old record is kept.
func updateShareState(critPath string, files []ShareFile, comments []ShareComment, reviewRound int, commentsSent bool) error {
	return updateReviewFile(critPath, func(cj *session.CritJSON) {
		cj.LastShareHash = computeShareHash(files, comments)
		if commentsSent {
			cj.SharedLines = sharedLines(comments)
		}
		cj.ReviewRound = reviewRound
	})
}

// recordSharedLines writes only the shared comment lines, for shares whose
// upload happened outside crit (the browser's proxy relay) or that keep the
// rest of their share state elsewhere (the daemon's first share).
func recordSharedLines(critPath string, lines map[string]session.SharedLine) error {
	return updateReviewFile(critPath, func(cj *session.CritJSON) {
		cj.SharedLines = lines
	})
}

func bindShareBaseURL(critPath, baseURL string) error {
	return updateReviewFileIf(critPath, func(cj *session.CritJSON) bool {
		if cj.ShareBaseURL != "" {
			return false
		}
		cj.ShareBaseURL = baseURL
		return true
	})
}

// persistShareState writes the share URL, delete token, and scope hash to the review file,
// preserving any existing content.
func persistShareState(critPath string, shareURL string, deleteToken string, scope string, org, orgName, visibility string) error {
	return persistShareStateForTarget(critPath, shareURL, "", deleteToken, scope, org, orgName, visibility)
}

// persistShareStateForTarget writes the share fields to the review file under
// the review write lock. Unlike updateReviewFile it creates the file when it
// is missing: the first `crit share` of plain files has no review file yet.
// A file that does not read or parse is an error, so it is not overwritten.
func persistShareStateForTarget(critPath string, shareURL string, shareBaseURL string, deleteToken string, scope string, org, orgName, visibility string) error {
	return session.WithReviewWriteLock(critPath, func() error {
		var cj session.CritJSON
		data, err := session.ReadFileShared(session.ReviewPathsFor(critPath).Review)
		switch {
		case err == nil:
			if err := json.Unmarshal(data, &cj); err != nil {
				return fmt.Errorf("invalid review file: %w", err)
			}
		case !os.IsNotExist(err):
			return err
		}
		if cj.Files == nil {
			cj.Files = make(map[string]session.CritJSONFile)
		}
		cj.ShareURL = shareURL
		cj.ShareBaseURL = shareBaseURL
		cj.DeleteToken = deleteToken
		cj.ShareScope = scope
		cj.ShareOrg = org
		cj.ShareOrgName = orgName
		cj.ShareVisibility = visibility
		cj.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		return session.SaveCritJSON(critPath, cj)
	})
}

// clearShareState removes share URL, delete token, share scope, and last-share
// hash from the review file. It is the single source of truth for "undo share
// metadata" — used by both the unpublish CLI path and tests.
func clearShareState(critPath string) error {
	err := updateReviewFile(critPath, func(cj *session.CritJSON) {
		cj.ShareURL = ""
		cj.ShareBaseURL = ""
		cj.DeleteToken = ""
		cj.ShareScope = ""
		cj.LastShareHash = ""
		cj.SharedLines = nil
	})
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntaxErr), errors.As(err, &typeErr):
		return fmt.Errorf("invalid review file: %w", err)
	case err != nil && os.IsNotExist(err):
		return nil // no review file means nothing to clear
	}
	return err
}

// checkProxyAuthCLIAllowed returns an error when proxy_auth is enabled in global
// config. Terminal share/fetch/unpublish cannot authenticate to crit-web behind
// an SSO reverse proxy — those operations must use the browser UI relay instead.
func checkProxyAuthCLIAllowed(command string) error {
	if !loadShareConfig().ProxyAuth {
		return nil
	}
	return fmt.Errorf(`%s is unavailable with proxy_auth enabled

Your crit-web instance is behind an SSO reverse proxy. Terminal commands cannot authenticate there — use Crit's browser interface instead

proxy_auth is set in ~/.crit.config.json (global config only)`, command)
}

// loadShareConfig loads the merged config.Config from the current directory context.
// Used by share/fetch/unpublish commands to avoid redundant config parsing.
func loadShareConfig() config.Config {
	cfg, err := config.LoadCurrentConfig()
	if err != nil {
		return config.Config{}
	}
	return cfg
}

// resolveShareURL resolves the share service URL from flag > env > config > fallback.
// cfg is the already-loaded config.Config so callers avoid redundant config parsing.
// fallback is returned when no other source provides a value (typically
// "https://crit.md" for share/auth commands, or "" for the serve path where an
// empty URL means "sharing not configured").
func resolveShareURL(flagValue string, cfg config.Config, fallback string) string {
	if flagValue != "" {
		return flagValue
	}
	if envShare, ok := os.LookupEnv("CRIT_SHARE_URL"); ok {
		return envShare
	}
	if cfg.ShareURL != "" {
		return cfg.ShareURL
	}
	return fallback
}

// tokenFromHostedURL extracts the review token from a hosted URL of the form
// https://crit.example/r/<token> (with optional trailing slash, query, or
// fragment). Returns the empty string if the URL doesn't match the expected
// shape. Single source of truth — replaces ad-hoc path.Base / strings.Contains
// parses scattered across share.go and tests.
func TokenFromHostedURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.TrimSuffix(u.Path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	if parts[len(parts)-2] != "r" {
		return ""
	}
	return parts[len(parts)-1]
}

// resolveAuthToken returns the auth token from env > config.
// cfg is the already-loaded config.Config so callers avoid redundant config parsing.
// Returns empty string if not configured.
func resolveAuthToken(cfg config.Config) string {
	if token, ok := os.LookupEnv("CRIT_AUTH_TOKEN"); ok {
		return token
	}
	return cfg.AuthToken
}

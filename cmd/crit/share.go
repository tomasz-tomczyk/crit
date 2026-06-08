package main

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
	"sort"
	"strconv"
	"strings"
	"time"
)

// defaultShareURL is the production crit-web service URL, used as the fallback
// when no share URL is configured via flag, env, or config.
const defaultShareURL = "https://crit.md"

// checkShareAllowed returns an error when the review at critPath is a live
// review. Sharing live reviews is deferred (v1 spec §Non-goals).
func checkShareAllowed(critPath string) error {
	data, err := os.ReadFile(reviewPathsFor(critPath).Review)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cj CritJSON
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
func checkGitHubSyncAllowed(cj CritJSON, op string) error {
	if cj.ReviewType == "live" {
		return fmt.Errorf("%s is not supported for live reviews", op)
	}
	return nil
}

// errShareUnauthorized indicates the share endpoint rejected the bearer token.
// Callers wrap this and inspect with errors.Is so they can clear the cached
// auth identity (token + user id + name + email) on top-level share/upsert
// failures.
var errShareUnauthorized = errors.New("auth token rejected by share service")

// shareScope computes a hash of sorted file paths, used to detect when
// share state belongs to a different file set.
func shareScope(paths []string) string {
	sorted := make([]string, len(paths))
	copy(sorted, paths)
	sort.Strings(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(h[:8]) // 16-char hex prefix is enough
}

// computeShareHash returns a short stable hash of the current share state:
// file contents (for change detection) and comment resolution states.
// If the hash equals LastShareHash in the review file, nothing has changed since
// the last push and no new round is needed.
func computeShareHash(files []shareFile, comments []shareComment) string {
	sorted := make([]shareFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	sortedC := make([]shareComment, len(comments))
	copy(sortedC, comments)
	sort.Slice(sortedC, func(i, j int) bool { return sortedC[i].ExternalID < sortedC[j].ExternalID })

	h := sha256.New()
	for _, f := range sorted {
		fmt.Fprintf(h, "file:%s:%s:%s\n", f.Path, f.Content, f.Status)
	}
	for _, c := range sortedC {
		fmt.Fprintf(h, "comment:%s:%v\n", c.ExternalID, c.Resolved)
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// shareFile represents a file to be shared.
// Status values: "added", "modified", "deleted", "renamed", "removed".
// "removed" means the file is orphaned (no longer in the review but has comments).
type shareFile struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Status    string `json:"status,omitempty"`
	Generated bool   `json:"generated,omitempty"`
	Encoding  string `json:"encoding,omitempty"`
}

// shareReply represents a reply to include in the shared review.
//
// GitHubID is the GitHub PR review-comment ID this reply was synced from
// (zero when the reply originated locally). crit-web uses a non-zero value
// as the signal that the reply was imported from GitHub and renders an
// inline marker so re-sharers can tell synced replies apart from native
// crit replies. Encoded with `omitempty` so locally-authored replies stay
// indistinguishable on the wire (issue #370).
type shareReply struct {
	Body       string `json:"body"`
	Author     string `json:"author_display_name,omitempty"`
	UserID     string `json:"user_id,omitempty"`
	ExternalID string `json:"external_id,omitempty"`
	GitHubID   int64  `json:"github_id,omitempty"`
}

// shareComment represents a comment to include in the shared review.
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
type shareComment struct {
	File        string       `json:"file,omitempty"`
	StartLine   int          `json:"start_line,omitempty"`
	EndLine     int          `json:"end_line,omitempty"`
	Body        string       `json:"body"`
	Quote       string       `json:"quote,omitempty"`
	Author      string       `json:"author_display_name,omitempty"`
	UserID      string       `json:"user_id,omitempty"`
	Scope       string       `json:"scope,omitempty"`
	ReviewRound int          `json:"review_round,omitempty"`
	Replies     []shareReply `json:"replies,omitempty"`
	ExternalID  string       `json:"external_id,omitempty"`
	Resolved    bool         `json:"resolved,omitempty"`
	GitHubID    int64        `json:"github_id,omitempty"`
}

// shareFileEntries serializes shareFile values into the JSON-friendly maps
// used by both POST and PUT payloads. Keeping a single helper prevents the
// two sites from drifting out of sync as new optional fields land.
func shareFileEntries(files []shareFile) []map[string]any {
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
func buildSharePayload(files []shareFile, comments []shareComment, reviewRound int, cliArgs []string, org, visibility, reviewType string) map[string]any {
	fileList := shareFileEntries(files)
	if comments == nil {
		comments = []shareComment{}
	}
	payload := map[string]any{
		"files":        fileList,
		"review_round": reviewRound,
		"comments":     comments,
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

// buildUpsertPayload constructs the JSON payload for PUT /api/reviews/:token.
// Used by both the CLI (upsertShareToWeb) and the browser popup-relay path
// (handleUpsertPayload) so the wire format stays in one place.
func buildUpsertPayload(files []shareFile, comments []shareComment, deleteToken string, reviewRound int, cliArgs []string) map[string]any {
	fileList := shareFileEntries(files)
	if comments == nil {
		comments = []shareComment{}
	}
	payload := map[string]any{
		"delete_token": deleteToken,
		"files":        fileList,
		"comments":     comments,
		"review_round": reviewRound,
	}
	if len(cliArgs) > 0 {
		payload["cli_args"] = cliArgs
	}
	return payload
}

// shareReviewFilesResult is the outcome of a fresh share upload.
type shareReviewFilesResult struct {
	URL         string
	DeleteToken string
	ReviewRound int
	Comments    []shareComment
}

// loadPreviewShareComments loads ALL of a preview session's comments and re-keys
// them to previewMainHTMLKey. Preview DOM pins live in separate "live-route"
// FileEntries keyed by the iframe pathname (/preview-content), distinct from the
// previewed HTML's "code" entry — so comments must be loaded across EVERY
// session path (sessionPaths = Session.FilePathsSnapshot()), not just the HTML's,
// then collapsed onto the single crawl entry. Used by the proxy preview-payload
// and re-share upsert-payload builders; handleShare gets the same effect by
// passing all session paths through shareReviewFiles.
func loadPreviewShareComments(critPath string, sessionPaths []string, fallbackAuthor string) ([]shareComment, int) {
	comments, reviewRound := loadCommentsForShare(critPath, sessionPaths, fallbackAuthor)
	remapPreviewCommentFiles(comments)
	return comments, reviewRound
}

// remapPreviewCommentFiles re-keys per-file comments to previewMainHTMLKey so
// they attach to the crawled HTML entry in a preview share payload. Review-level
// comments (empty File) are left untouched. Preview is a single rendered page,
// so every per-file comment (including DOM pins on live-route entries) collapses
// onto the one previewed HTML.
func remapPreviewCommentFiles(comments []shareComment) {
	for i := range comments {
		if comments[i].File != "" {
			comments[i].File = previewMainHTMLKey
		}
	}
}

// shareReviewFiles loads comments + cli_args from the review file at critPath
// and POSTs the files to crit-web. Used by both the CLI (`crit share`) and the
// server's POST /api/share endpoint so payload wiring stays in one place.
func shareReviewFiles(critPath string, files []shareFile, filePaths []string, svcURL, authToken, fallbackAuthor, org, visibility, reviewType string) (shareReviewFilesResult, error) {
	comments, reviewRound := loadCommentsForShare(critPath, filePaths, fallbackAuthor)
	if reviewType == "preview" {
		// Preview comments are stored under the session's on-disk path (passed
		// in filePaths) but the crawled payload keys the HTML as
		// previewMainHTMLKey — re-key so crit-web attaches them to that entry.
		remapPreviewCommentFiles(comments)
	}
	cliArgs := loadCliArgsFromReviewFile(critPath)

	url, deleteToken, err := shareFilesToWeb(files, comments, svcURL, reviewRound, authToken, cliArgs, org, visibility, reviewType)
	if err != nil {
		return shareReviewFilesResult{}, err
	}
	return shareReviewFilesResult{
		URL:         url,
		DeleteToken: deleteToken,
		ReviewRound: reviewRound,
		Comments:    comments,
	}, nil
}

// shareFilesToWeb uploads files to a crit-web instance and returns the share URL and delete token.
func shareFilesToWeb(files []shareFile, comments []shareComment, shareURL string, reviewRound int, authToken string, cliArgs []string, org, visibility, reviewType string) (string, string, error) {
	payload := buildSharePayload(files, comments, reviewRound, cliArgs, org, visibility, reviewType)
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("marshaling payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, shareURL+"/api/reviews", bytes.NewReader(body))
	if err != nil {
		return "", "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setBearer(req, authToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("posting to share service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", "", errShareUnauthorized
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
func loadCliArgsFromReviewFile(critPath string) []string {
	data, err := readFileShared(reviewPathsFor(critPath).Review)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "warning: failed to load cli_args from review file: %v\n", err)
		}
		return nil
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to load cli_args from review file: %v\n", err)
		return nil
	}
	return cj.CliArgs
}

// unpublishFromWeb deletes a shared review from a crit-web instance.
// Returns nil if the review was deleted or was already gone (idempotent).
func unpublishFromWeb(shareURL string, deleteToken string, authToken string) error {
	body, _ := json.Marshal(map[string]string{"delete_token": deleteToken})
	req, err := http.NewRequest(http.MethodDelete, shareURL+"/api/reviews", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setBearer(req, authToken)

	client := &http.Client{Timeout: 30 * time.Second}
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

// setBearer sets the Authorization header to "Bearer <token>" when token is non-empty.
func setBearer(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// loadCommentsForShare reads the review file at critPath and returns unresolved
// shareComment entries for the given file paths, plus the review round.
// fallbackAuthor is used when a comment has no Author set (typically cfg.Author).
//
// ExternalID is set so the server persists it on first insert and matches it
// on subsequent upserts. This enables round-trip carry-forward of `user_id`:
// replace_comments matches incoming attrs.external_id against the
// existing-by-external_id map and preserves the verified user_id when the
// re-sharer is anonymous (rules #3/#4 of the attribution table).
//
// Used for both the initial share and the upsert path — they need identical
// payloads, so they share this loader.
func loadCommentsForShare(critPath string, filePaths []string, fallbackAuthor string) ([]shareComment, int) {
	return loadCommentsFromCritJSON(critPath, filePaths, false, true, fallbackAuthor)
}

// commentToShareComment converts a Comment into a shareComment, applying the
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
func commentToShareComment(c Comment, filePath, scope, fallbackAuthor, critPath string, includeResolved, setExternalID bool) shareComment {
	author := c.Author
	if author == "" {
		author = fallbackAuthor
	}
	sc := shareComment{
		File:      filePath,
		StartLine: c.StartLine,
		EndLine:   c.EndLine,
		Body:      inlineAttachmentsAsDataURIs(critPath, c.Body),
		Quote:     c.Quote,
		Author:    author,
		UserID:    c.UserID,
		Scope:     scope,
		GitHubID:  c.GitHubID,
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
		sr := shareReply{
			Body:     inlineAttachmentsAsDataURIs(critPath, r.Body),
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

// loadCommentsFromCritJSON reads the review file at critPath and returns shareComment
// entries for the given file paths, plus the review round. When includeResolved is true,
// resolved comments are included. When setExternalID is true, ExternalID is set
// from the local comment ID for round-trip tracking. fallbackAuthor fills missing
// Author fields (typically cfg.Author).
func loadCommentsFromCritJSON(critPath string, filePaths []string, includeResolved, setExternalID bool, fallbackAuthor string) ([]shareComment, int) {
	data, err := readFileShared(reviewPathsFor(critPath).Review)
	if err != nil {
		return nil, 1
	}
	var cj CritJSON
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

	var comments []shareComment
	for filePath, cf := range cj.Files {
		if !pathSet[filePath] {
			continue
		}
		for _, c := range cf.Comments {
			if !includeResolved && c.Resolved {
				continue
			}
			scope := c.Scope
			comments = append(comments, commentToShareComment(c, filePath, scope, fallbackAuthor, critPath, includeResolved, setExternalID))
		}
	}
	for _, c := range cj.ReviewComments {
		if !includeResolved && c.Resolved {
			continue
		}
		comments = append(comments, commentToShareComment(c, "", "review", fallbackAuthor, critPath, includeResolved, setExternalID))
	}
	return comments, round
}

// webReply is the shape of a reply nested inside a webComment.
type webReply struct {
	Body              string `json:"body"`
	AuthorDisplayName string `json:"author_display_name"`
	UserID            string `json:"user_id"`
}

// webComment is the shape of a comment returned by GET /api/reviews/:token/comments.
// AuthorIdentity is retained for compatibility with existing crit-web responses
// and is treated as a session-owner token for anonymous web visitors. UserID
// is the verified user id, set when the comment was authored by a logged-in
// user (either CLI with bearer token or LiveView while signed in).
type webComment struct {
	Body              string     `json:"body"`
	FilePath          string     `json:"file_path"`
	StartLine         int        `json:"start_line"`
	EndLine           int        `json:"end_line"`
	ReviewRound       int        `json:"review_round"`
	Resolved          bool       `json:"resolved"`
	ResolvedRound     int        `json:"resolved_round"`
	ExternalID        string     `json:"external_id"`
	AuthorDisplayName string     `json:"author_display_name"`
	AuthorIdentity    string     `json:"author_identity"`
	UserID            string     `json:"user_id"`
	Quote             string     `json:"quote"`
	Scope             string     `json:"scope"`
	Replies           []webReply `json:"replies"`
}

// buildLocalFingerprintIndex returns both the fingerprint set and a map from
// fingerprint to local comment ID. The ID map lets callers look up the local
// comment when a web comment matches by fingerprint (so replies can be merged
// instead of dropped).
func buildLocalFingerprintIndex(cj CritJSON) (map[string]bool, map[string]string) {
	fps := make(map[string]bool)
	ids := make(map[string]string)
	for path, f := range cj.Files {
		for _, c := range f.Comments {
			key := fmt.Sprintf("%s|%s|%d|%d", c.Body, path, c.StartLine, c.EndLine)
			fps[key] = true
			if c.ID != "" {
				ids[key] = c.ID
			}
		}
	}
	for _, c := range cj.ReviewComments {
		key := fmt.Sprintf("%s||0|0", c.Body)
		fps[key] = true
		if c.ID != "" {
			ids[key] = c.ID
		}
	}
	return fps, ids
}

// fetchWebCommentsResult holds both new comments and reply updates for existing ones.
type fetchWebCommentsResult struct {
	NewComments  []webComment
	ReplyUpdates map[string][]webReply // external_id -> replies from web
}

// fetchWebComments fetches all comments from crit-web, returning new comments
// and reply updates for existing comments that have replies added on the web.
// localFingerprintIDs maps body+file+line fingerprints to the local comment ID,
// so that web comments matching a previously-imported web-N comment by
// fingerprint can have their replies merged instead of dropped.
func fetchWebComments(shareURL string, localIDs map[string]bool, localFingerprints map[string]bool, localFingerprintIDs map[string]string, authToken string) (fetchWebCommentsResult, error) {
	var result fetchWebCommentsResult
	result.ReplyUpdates = make(map[string][]webReply)

	token := tokenFromHostedURL(shareURL)
	u, err := url.Parse(shareURL)
	if err != nil {
		return result, fmt.Errorf("invalid share URL: %w", err)
	}
	if token == "" {
		return result, fmt.Errorf("invalid share URL: missing /r/<token> path")
	}
	apiURL := u.Scheme + "://" + u.Host + "/api/reviews/" + token + "/comments"

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return result, fmt.Errorf("creating request: %w", err)
	}
	setBearer(req, authToken)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("fetching remote comments: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return result, nil // review gone
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return result, errShareUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("remote comments returned status %d", resp.StatusCode)
	}

	var all []webComment
	if err := decodeJSONOrHTMLHint(resp, &all); err != nil {
		return result, err
	}

	for _, wc := range all {
		if dropDuplicateWebComment(wc, localIDs, localFingerprints, localFingerprintIDs, result.ReplyUpdates) {
			continue
		}
		result.NewComments = append(result.NewComments, wc)
	}
	return result, nil
}

// dropDuplicateWebComment returns true if wc is already represented locally
// (by external_id or fingerprint match) and therefore should not be appended
// to NewComments. Any new replies on the duplicate are recorded in updates.
func dropDuplicateWebComment(
	wc webComment,
	localIDs map[string]bool,
	localFingerprints map[string]bool,
	localFingerprintIDs map[string]string,
	updates map[string][]webReply,
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
type upsertResult struct {
	URL         string
	ReviewRound int
	Changed     bool
}

// upsertShareToWeb pushes an updated review to crit-web via PUT.
// If the content hash matches LastShareHash (no changes), returns without calling PUT.
func upsertShareToWeb(cfg CritJSON, files []shareFile, comments []shareComment, authToken string) (upsertResult, error) {
	result := upsertResult{URL: cfg.ShareURL, ReviewRound: cfg.ReviewRound}

	currentHash := computeShareHash(files, comments)
	if currentHash == cfg.LastShareHash {
		return result, nil // nothing changed
	}

	token := tokenFromHostedURL(cfg.ShareURL)
	u, err := url.Parse(cfg.ShareURL)
	if err != nil {
		return result, fmt.Errorf("invalid share URL: %w", err)
	}
	if token == "" {
		return result, fmt.Errorf("invalid share URL: missing /r/<token> path")
	}
	apiURL := u.Scheme + "://" + u.Host + "/api/reviews/" + token

	payload := buildUpsertPayload(files, comments, cfg.DeleteToken, cfg.ReviewRound, cfg.CliArgs)

	body, err := json.Marshal(payload)
	if err != nil {
		return result, fmt.Errorf("marshaling upsert payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, apiURL, bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	setBearer(req, authToken)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("PUT %s: %w", apiURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return result, errShareUnauthorized
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

	result.Changed = respBody.Changed
	result.ReviewRound = respBody.ReviewRound
	if respBody.URL != "" {
		result.URL = respBody.URL
	}
	return result, nil
}

// loadExistingShareCfg returns the full CritJSON if a matching share exists
// (same file scope). The bool is true only when an existing share is found.
// A missing review file is reported as (zero, false, nil) so callers can fall
// back to creating a new share. Parse errors and other I/O errors are surfaced
// so callers can refuse to clobber a corrupted file.
func loadExistingShareCfg(critPath string, paths []string) (CritJSON, bool, error) {
	data, err := readFileShared(reviewPathsFor(critPath).Review)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return CritJSON{}, false, nil
		}
		return CritJSON{}, false, fmt.Errorf("reading review file %q: %w", critPath, err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return CritJSON{}, false, fmt.Errorf("parsing review file %q: %w", critPath, err)
	}
	if cj.ShareURL == "" {
		return CritJSON{}, false, nil
	}
	if cj.ShareScope != "" && cj.ShareScope != shareScope(paths) {
		return CritJSON{}, false, nil
	}
	return cj, true, nil
}

// dedupWebComments filters incoming web comments against the local review state,
// returning only genuinely new comments and any reply updates for existing ones.
// This is the dedup entry point used by handleMergeComments (browser relay path)
// to match the same dedup logic that fetchWebComments uses on the direct path.
func dedupWebComments(cj CritJSON, incoming []webComment) ([]webComment, map[string][]webReply) {
	localIDs := buildLocalIDSet(cj)
	localFingerprints, localFingerprintIDs := buildLocalFingerprintIndex(cj)
	replyUpdates := make(map[string][]webReply)

	var newComments []webComment
	for _, wc := range incoming {
		if dropDuplicateWebComment(wc, localIDs, localFingerprints, localFingerprintIDs, replyUpdates) {
			continue
		}
		newComments = append(newComments, wc)
	}
	return newComments, replyUpdates
}

// buildLocalIDSet collects all local comment IDs across all files and review comments.
func buildLocalIDSet(cj CritJSON) map[string]bool {
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
// in a CritJSON structure. This ensures new web comment IDs are globally unique.
func highestWebIndex(cj CritJSON) int {
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
// It also merges reply updates for existing comments identified by external_id.
// Pass nil for replyUpdates to skip reply merging.
//
// When the active focus is a range mode (probed from a running daemon, or
// inferred from the on-disk ActiveDiffScope), imported comments are stamped
// with HeadSHA + DiffScope=layer so they pass visibleInFocus in range view.
// See spec §E "Write path — `mergeWebComments`".
func mergeWebComments(critPath string, newComments []webComment, replyUpdates map[string][]webReply) error {
	data, err := readFileShared(reviewPathsFor(critPath).Review)
	if err != nil {
		return err
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return err
	}
	if cj.Files == nil {
		cj.Files = make(map[string]CritJSONFile)
	}

	// Find the highest existing web-N index so new IDs are globally unique
	// even if earlier ones were deleted from the review file.
	webCount := highestWebIndex(cj)

	scope := resolvePullScope(&cj)

	now := time.Now().UTC().Format(time.RFC3339)
	for _, wc := range newComments {
		webCount++
		var replies []Reply
		for _, wr := range wc.Replies {
			replies = append(replies, Reply{
				Body:   wr.Body,
				Author: wr.AuthorDisplayName,
				UserID: wr.UserID,
			})
		}
		c := stampWithFocus(Comment{
			ID:          fmt.Sprintf("web-%d", webCount),
			StartLine:   wc.StartLine,
			EndLine:     wc.EndLine,
			Body:        wc.Body,
			Quote:       wc.Quote,
			Author:      wc.AuthorDisplayName,
			UserID:      wc.UserID,
			Scope:       wc.Scope,
			ReviewRound: wc.ReviewRound,
			Replies:     replies,
			CreatedAt:   now,
			UpdatedAt:   now,
		}, scope.asFocus())
		if wc.Scope == "review" {
			cj.ReviewComments = append(cj.ReviewComments, c)
		} else {
			entry := cj.Files[wc.FilePath]
			entry.Comments = append(entry.Comments, c)
			cj.Files[wc.FilePath] = entry
		}
	}

	// Merge reply updates for existing comments (matched by external_id).
	if len(replyUpdates) > 0 {
		for filePath, cf := range cj.Files {
			for i := range cf.Comments {
				if webReplies, ok := replyUpdates[cf.Comments[i].ID]; ok {
					cf.Comments[i] = mergeRepliesIntoComment(cf.Comments[i], webReplies)
				}
			}
			cj.Files[filePath] = cf
		}
		for i := range cj.ReviewComments {
			if webReplies, ok := replyUpdates[cj.ReviewComments[i].ID]; ok {
				cj.ReviewComments[i] = mergeRepliesIntoComment(cj.ReviewComments[i], webReplies)
			}
		}
	}

	cj.UpdatedAt = now
	return saveCritJSON(critPath, cj)
}

// mergeRepliesIntoComment merges web replies into a comment, deduplicating by body.
func mergeRepliesIntoComment(c Comment, webReplies []webReply) Comment {
	existing := make(map[string]bool)
	for _, r := range c.Replies {
		existing[r.Body] = true
	}
	for _, wr := range webReplies {
		if existing[wr.Body] {
			continue
		}
		c.Replies = append(c.Replies, Reply{
			Body:   wr.Body,
			Author: wr.AuthorDisplayName,
			UserID: wr.UserID,
		})
	}
	return c
}

// updateShareState writes LastShareHash and ReviewRound back to the review file.
func updateShareState(critPath string, hash string, reviewRound int) error {
	data, err := readFileShared(reviewPathsFor(critPath).Review)
	if err != nil {
		return err
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return err
	}
	cj.LastShareHash = hash
	cj.ReviewRound = reviewRound
	cj.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return saveCritJSON(critPath, cj)
}

// persistShareState writes the share URL, delete token, and scope hash to the review file,
// preserving any existing content.
func persistShareState(critPath string, shareURL string, deleteToken string, scope string, org, orgName, visibility string) error {
	var cj CritJSON
	if data, err := readFileShared(reviewPathsFor(critPath).Review); err == nil {
		_ = json.Unmarshal(data, &cj)
	}
	if cj.Files == nil {
		cj.Files = make(map[string]CritJSONFile)
	}
	cj.ShareURL = shareURL
	cj.DeleteToken = deleteToken
	cj.ShareScope = scope
	cj.ShareOrg = org
	cj.ShareOrgName = orgName
	cj.ShareVisibility = visibility
	cj.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	return saveCritJSON(critPath, cj)
}

// clearShareState removes share URL, delete token, share scope, and last-share
// hash from the review file. It is the single source of truth for "undo share
// metadata" — used by both the unpublish CLI path and tests.
func clearShareState(critPath string) error {
	data, err := readFileShared(reviewPathsFor(critPath).Review)
	if err != nil {
		return nil //nolint:nilerr // no review file means nothing to clear
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return fmt.Errorf("invalid review file: %w", err)
	}
	cj.ShareURL = ""
	cj.DeleteToken = ""
	cj.ShareScope = ""
	cj.LastShareHash = ""
	cj.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	return saveCritJSON(critPath, cj)
}

// loadShareConfig loads the merged Config from the current directory context.
// Used by share/fetch/unpublish commands to avoid redundant config parsing.
func loadShareConfig() Config {
	cfgDir := ""
	if vcs := DetectVCS(""); vcs != nil {
		cfgDir, _ = vcs.RepoRoot()
	}
	if cfgDir == "" {
		cfgDir = mustGetwd()
	}
	return LoadConfig(cfgDir)
}

// resolveShareURL resolves the share service URL from flag > env > config > fallback.
// cfg is the already-loaded Config so callers avoid redundant config parsing.
// fallback is returned when no other source provides a value (typically
// "https://crit.md" for share/auth commands, or "" for the serve path where an
// empty URL means "sharing not configured").
func resolveShareURL(flagValue string, cfg Config, fallback string) string {
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
func tokenFromHostedURL(rawURL string) string {
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
// cfg is the already-loaded Config so callers avoid redundant config parsing.
// Returns empty string if not configured.
func resolveAuthToken(cfg Config) string {
	if token, ok := os.LookupEnv("CRIT_AUTH_TOKEN"); ok {
		return token
	}
	return cfg.AuthToken
}

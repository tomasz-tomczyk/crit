package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// defaultShareURL is the production crit-web service URL, used as the fallback
// when no share URL is configured via flag, env, or config.
const defaultShareURL = "https://crit.md"

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
// If the hash equals LastShareHash in .crit.json, nothing has changed since
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
		fmt.Fprintf(h, "file:%s:%s\n", f.Path, f.Content)
	}
	for _, c := range sortedC {
		fmt.Fprintf(h, "comment:%s:%v\n", c.ExternalID, c.Resolved)
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// shareFile represents a file to be shared.
type shareFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// shareReply represents a reply to include in the shared review.
type shareReply struct {
	Body   string `json:"body"`
	Author string `json:"author_display_name,omitempty"`
}

// shareComment represents a comment to include in the shared review.
type shareComment struct {
	File        string       `json:"file,omitempty"`
	StartLine   int          `json:"start_line,omitempty"`
	EndLine     int          `json:"end_line,omitempty"`
	Body        string       `json:"body"`
	Quote       string       `json:"quote,omitempty"`
	Author      string       `json:"author_display_name,omitempty"`
	Scope       string       `json:"scope,omitempty"`
	ReviewRound int          `json:"review_round,omitempty"`
	Replies     []shareReply `json:"replies,omitempty"`
	ExternalID  string       `json:"external_id,omitempty"`
	Resolved    bool         `json:"resolved,omitempty"`
}

// buildSharePayload constructs the JSON payload for POST /api/reviews.
func buildSharePayload(files []shareFile, comments []shareComment, reviewRound int) map[string]any {
	fileList := make([]map[string]string, len(files))
	for i, f := range files {
		fileList[i] = map[string]string{"path": f.Path, "content": f.Content}
	}
	if comments == nil {
		comments = []shareComment{}
	}
	return map[string]any{
		"files":        fileList,
		"review_round": reviewRound,
		"comments":     comments,
	}
}

// shareFilesToWeb uploads files to a crit-web instance and returns the share URL and delete token.
func shareFilesToWeb(files []shareFile, comments []shareComment, shareURL string, reviewRound int, authToken string) (string, string, error) {
	payload := buildSharePayload(files, comments, reviewRound)
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		if errBody.Error != "" {
			return "", "", fmt.Errorf("share service error: %s", errBody.Error)
		}
		return "", "", fmt.Errorf("share service returned status %d", resp.StatusCode)
	}

	var result struct {
		URL         string `json:"url"`
		DeleteToken string `json:"delete_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", fmt.Errorf("decoding share response: %w", err)
	}
	return result.URL, result.DeleteToken, nil
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
	_ = json.NewDecoder(resp.Body).Decode(&errBody)
	if errBody.Error != "" {
		return fmt.Errorf("share service error: %s", errBody.Error)
	}
	return fmt.Errorf("share service returned status %d", resp.StatusCode)
}

// setBearer sets the Authorization header to "Bearer <token>" when token is non-empty.
func setBearer(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// buildShareFromSession extracts files and unresolved comments from a live session.
func buildShareFromSession(s *Session) ([]shareFile, []shareComment, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var files []shareFile
	var comments []shareComment
	for _, f := range s.Files {
		files = append(files, shareFile{Path: f.Path, Content: f.Content})
		for _, c := range f.Comments {
			if c.Resolved {
				continue
			}
			sc := shareComment{
				File:      f.Path,
				StartLine: c.StartLine,
				EndLine:   c.EndLine,
				Body:      c.Body,
				Quote:     c.Quote,
				Author:    c.Author,
				Scope:     c.Scope,
			}
			if c.ReviewRound >= 1 {
				sc.ReviewRound = c.ReviewRound
			}
			for _, r := range c.Replies {
				sc.Replies = append(sc.Replies, shareReply{Body: r.Body, Author: r.Author})
			}
			comments = append(comments, sc)
		}
	}
	return files, comments, s.ReviewRound
}

// loadCommentsForShare reads the review file at critPath and returns shareComment entries
// for the given file paths, plus the review round. Resolved comments are excluded.
func loadCommentsForShare(critPath string, filePaths []string) ([]shareComment, int) {
	return loadCommentsFromCritJSON(critPath, filePaths, false, false)
}

// loadCommentsForUpsert loads unresolved comments with ExternalID set for
// round-trip tracking. Resolved comments are excluded — same as initial share.
func loadCommentsForUpsert(critPath string, filePaths []string) ([]shareComment, int) {
	return loadCommentsFromCritJSON(critPath, filePaths, false, true)
}

// loadCommentsFromCritJSON reads the review file at critPath and returns shareComment
// entries for the given file paths, plus the review round. When includeResolved is true,
// resolved comments are included. When setExternalID is true, ExternalID is set
// from the local comment ID for round-trip tracking.
func loadCommentsFromCritJSON(critPath string, filePaths []string, includeResolved bool, setExternalID bool) ([]shareComment, int) {
	data, err := os.ReadFile(critPath)
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
			sc := shareComment{
				File:      filePath,
				StartLine: c.StartLine,
				EndLine:   c.EndLine,
				Body:      c.Body,
				Quote:     c.Quote,
				Author:    c.Author,
				Scope:     c.Scope,
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
				sc.Replies = append(sc.Replies, shareReply{Body: r.Body, Author: r.Author})
			}
			comments = append(comments, sc)
		}
	}
	for _, c := range cj.ReviewComments {
		if !includeResolved && c.Resolved {
			continue
		}
		sc := shareComment{
			Body:   c.Body,
			Author: c.Author,
			Scope:  "review",
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
			sc.Replies = append(sc.Replies, shareReply{Body: r.Body, Author: r.Author})
		}
		comments = append(comments, sc)
	}
	return comments, round
}

// webComment is the shape of a comment returned by GET /api/reviews/:token/comments.
type webComment struct {
	Body              string `json:"body"`
	FilePath          string `json:"file_path"`
	StartLine         int    `json:"start_line"`
	EndLine           int    `json:"end_line"`
	ReviewRound       int    `json:"review_round"`
	Resolved          bool   `json:"resolved"`
	ExternalID        string `json:"external_id"`
	AuthorDisplayName string `json:"author_display_name"`
	Quote             string `json:"quote"`
	Scope             string `json:"scope"`
}

// buildLocalFingerprints returns a set of body+file+line fingerprints for all
// local comments. Used to deduplicate web-authored comments (which have no
// ExternalID) on repeated shares.
func buildLocalFingerprints(cj CritJSON) map[string]bool {
	fps := make(map[string]bool)
	for path, f := range cj.Files {
		for _, c := range f.Comments {
			key := fmt.Sprintf("%s|%s|%d|%d", c.Body, path, c.StartLine, c.EndLine)
			fps[key] = true
		}
	}
	for _, c := range cj.ReviewComments {
		key := fmt.Sprintf("%s||0|0", c.Body)
		fps[key] = true
	}
	return fps
}

// fetchNewWebComments fetches comments from crit-web and returns only those
// not already present locally (identified by external_id or body+line fingerprint).
//
// Called automatically inside runShare when an existing ShareURL is detected —
// i.e., when the agent calls `crit share <files>` after applying changes from
// the crit-web prompt. This captures any web-reviewer comments added after the
// prompt was generated (e.g., a late-arriving review) so they appear in local
// .crit.json before the next round is pushed.
//
// shareURL is the full review URL, e.g. "https://crit.md/r/abc123".
func fetchNewWebComments(shareURL string, localIDs map[string]bool, localFingerprints map[string]bool, authToken string) ([]webComment, error) {
	token := path.Base(shareURL)
	u, err := url.Parse(shareURL)
	if err != nil {
		return nil, fmt.Errorf("invalid share URL: %w", err)
	}
	apiURL := u.Scheme + "://" + u.Host + "/api/reviews/" + token + "/comments"

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	setBearer(req, authToken)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching remote comments: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // review gone
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote comments returned status %d", resp.StatusCode)
	}

	var all []webComment
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return nil, fmt.Errorf("decoding remote comments: %w", err)
	}

	var newOnes []webComment
	for _, wc := range all {
		if wc.ExternalID != "" && localIDs[wc.ExternalID] {
			continue // already have this locally by ID
		}
		if wc.ExternalID == "" {
			fp := fmt.Sprintf("%s|%s|%d|%d", wc.Body, wc.FilePath, wc.StartLine, wc.EndLine)
			if localFingerprints[fp] {
				continue // web-authored comment already imported
			}
		}
		newOnes = append(newOnes, wc)
	}
	return newOnes, nil
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

	token := path.Base(cfg.ShareURL)
	u, err := url.Parse(cfg.ShareURL)
	if err != nil {
		return result, fmt.Errorf("invalid share URL: %w", err)
	}
	apiURL := u.Scheme + "://" + u.Host + "/api/reviews/" + token

	fileList := make([]map[string]string, len(files))
	for i, f := range files {
		fileList[i] = map[string]string{"path": f.Path, "content": f.Content}
	}

	payload := map[string]any{
		"delete_token": cfg.DeleteToken,
		"files":        fileList,
		"comments":     comments,
		"review_round": cfg.ReviewRound,
	}

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
		return result, fmt.Errorf("crit-web rejected the request — check your auth_token in config")
	}
	if resp.StatusCode >= 400 {
		return result, fmt.Errorf("upsert failed with status %d", resp.StatusCode)
	}

	var respBody struct {
		URL         string `json:"url"`
		ReviewRound int    `json:"review_round"`
		Changed     bool   `json:"changed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return result, fmt.Errorf("decoding upsert response: %w", err)
	}

	result.Changed = respBody.Changed
	result.ReviewRound = respBody.ReviewRound
	if respBody.URL != "" {
		result.URL = respBody.URL
	}
	return result, nil
}

// loadExistingShareCfg returns the full CritJSON if a matching share exists (same file scope).
func loadExistingShareCfg(critPath string, paths []string) (CritJSON, bool) {
	data, err := os.ReadFile(critPath)
	if err != nil {
		return CritJSON{}, false
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return CritJSON{}, false
	}
	if cj.ShareURL == "" {
		return CritJSON{}, false
	}
	if cj.ShareScope != "" && cj.ShareScope != shareScope(paths) {
		return CritJSON{}, false
	}
	return cj, true
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
func mergeWebComments(critPath string, newComments []webComment) error {
	data, err := os.ReadFile(critPath)
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
	// even if earlier ones were deleted from .crit.json.
	webCount := highestWebIndex(cj)

	now := time.Now().UTC().Format(time.RFC3339)
	for _, wc := range newComments {
		webCount++
		c := Comment{
			ID:          fmt.Sprintf("web-%d", webCount),
			StartLine:   wc.StartLine,
			EndLine:     wc.EndLine,
			Body:        wc.Body,
			Quote:       wc.Quote,
			Author:      wc.AuthorDisplayName,
			Scope:       wc.Scope,
			ReviewRound: wc.ReviewRound,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if wc.Scope == "review" {
			cj.ReviewComments = append(cj.ReviewComments, c)
		} else {
			entry := cj.Files[wc.FilePath]
			entry.Comments = append(entry.Comments, c)
			cj.Files[wc.FilePath] = entry
		}
	}

	cj.UpdatedAt = now
	return saveCritJSON(critPath, cj)
}

// updateShareState writes LastShareHash and ReviewRound back to the review file.
func updateShareState(critPath string, hash string, reviewRound int) error {
	data, err := os.ReadFile(critPath)
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
func persistShareState(critPath string, shareURL string, deleteToken string, scope string) error {
	var cj CritJSON
	if data, err := os.ReadFile(critPath); err == nil {
		_ = json.Unmarshal(data, &cj)
	}
	if cj.Files == nil {
		cj.Files = make(map[string]CritJSONFile)
	}
	cj.ShareURL = shareURL
	cj.DeleteToken = deleteToken
	cj.ShareScope = scope
	cj.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	return saveCritJSON(critPath, cj)
}

// clearShareState removes share URL, delete token, share scope, and last-share
// hash from the review file. It is the single source of truth for "undo share
// metadata" — used by both the unpublish CLI path and tests.
func clearShareState(critPath string) error {
	data, err := os.ReadFile(critPath)
	if err != nil {
		return nil // no .crit.json, nothing to clear
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return fmt.Errorf("invalid .crit.json: %w", err)
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
	if IsGitRepo() {
		cfgDir, _ = RepoRoot()
	}
	if cfgDir == "" {
		cfgDir, _ = os.Getwd()
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

// resolveAuthToken returns the auth token from env > config.
// cfg is the already-loaded Config so callers avoid redundant config parsing.
// Returns empty string if not configured.
func resolveAuthToken(cfg Config) string {
	if token, ok := os.LookupEnv("CRIT_AUTH_TOKEN"); ok {
		return token
	}
	return cfg.AuthToken
}

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// shareScope computes a hash of sorted file paths, used to detect when
// share state belongs to a different file set.
func shareScope(paths []string) string {
	sorted := make([]string, len(paths))
	copy(sorted, paths)
	sort.Strings(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(h[:8]) // 16-char hex prefix is enough
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
	File        string       `json:"file"`
	StartLine   int          `json:"start_line"`
	EndLine     int          `json:"end_line"`
	Body        string       `json:"body"`
	Author      string       `json:"author_display_name,omitempty"`
	ReviewRound int          `json:"review_round,omitempty"`
	Replies     []shareReply `json:"replies,omitempty"`
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
func shareFilesToWeb(files []shareFile, comments []shareComment, shareURL string, reviewRound int) (string, string, error) {
	payload := buildSharePayload(files, comments, reviewRound)
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("marshaling payload: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(shareURL+"/api/reviews", "application/json", bytes.NewReader(body))
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
func unpublishFromWeb(shareURL string, deleteToken string) error {
	body, _ := json.Marshal(map[string]string{"delete_token": deleteToken})
	req, err := http.NewRequest(http.MethodDelete, shareURL+"/api/reviews", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

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
				Author:    c.Author,
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

// loadCommentsForShare reads .crit.json from dir and returns shareComment entries
// for the given file paths, plus the review round.
func loadCommentsForShare(dir string, filePaths []string) ([]shareComment, int) {
	critPath := filepath.Join(dir, ".crit.json")
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
	for path, cf := range cj.Files {
		if !pathSet[path] {
			continue
		}
		for _, c := range cf.Comments {
			if c.Resolved {
				continue
			}
			sc := shareComment{
				File:      path,
				StartLine: c.StartLine,
				EndLine:   c.EndLine,
				Body:      c.Body,
				Author:    c.Author,
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

	return comments, round
}

// persistShareState writes the share URL, delete token, and scope hash to .crit.json,
// preserving any existing content.
func persistShareState(dir string, shareURL string, deleteToken string, scope string) error {
	critPath := filepath.Join(dir, ".crit.json")
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

	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling .crit.json: %w", err)
	}
	return os.WriteFile(critPath, data, 0644)
}

// clearShareState removes share URL and delete token from .crit.json.
func clearShareState(dir string) error {
	critPath := filepath.Join(dir, ".crit.json")
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
	cj.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	out, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling .crit.json: %w", err)
	}
	return os.WriteFile(critPath, out, 0644)
}

// loadExistingShareState reads .crit.json and returns any persisted share URL and delete token.
// Returns ("", "") if no share state exists or if the scope doesn't match the given paths.
func loadExistingShareState(dir string, paths []string) (string, string) {
	critPath := filepath.Join(dir, ".crit.json")
	data, err := os.ReadFile(critPath)
	if err != nil {
		return "", ""
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return "", ""
	}
	// If scope is set, only return share state if it matches the current file set.
	if cj.ShareScope != "" && cj.ShareScope != shareScope(paths) {
		return "", ""
	}
	return cj.ShareURL, cj.DeleteToken
}

// resolveShareURL resolves the share service URL from flag > env > config > default.
func resolveShareURL(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if envShare, ok := os.LookupEnv("CRIT_SHARE_URL"); ok {
		return envShare
	}
	cfgDir := ""
	if IsGitRepo() {
		cfgDir, _ = RepoRoot()
	}
	if cfgDir == "" {
		cfgDir, _ = os.Getwd()
	}
	cfg := LoadConfig(cfgDir)
	if cfg.ShareURL != "" {
		return cfg.ShareURL
	}
	return "https://crit.md"
}

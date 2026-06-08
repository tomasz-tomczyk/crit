package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// fetchPRFileContentFn is the live function used to fetch PR file content
// from the GitHub API. Indirected through a package var so tests can stub
// the network call without mocking exec.Command.
var fetchPRFileContentFn = fetchPRFileContentReal

// fetchPRFileContent fetches one file's content at a specific SHA via
// `gh api repos/<owner>/<repo>/contents/<path>?ref=<sha>`. The injection
// point lets tests bypass the `gh` CLI.
func fetchPRFileContent(repoOwner, repoName, sha, path string) ([]byte, error) {
	return fetchPRFileContentFn(repoOwner, repoName, sha, path)
}

// fetchPRFileContentReal is the production implementation: shells out to
// `gh api`, then decodes the base64-wrapped response payload.
func fetchPRFileContentReal(repoOwner, repoName, sha, path string) ([]byte, error) {
	endpoint := fmt.Sprintf("repos/%s/%s/contents/%s?ref=%s",
		repoOwner, repoName, path, sha)
	out, err := exec.Command("gh", "api", endpoint).Output()
	if err != nil {
		return nil, fmt.Errorf("gh api %s: %w", endpoint, err)
	}
	return decodePRFileContent(out)
}

// prFileContentResp is the subset of the GitHub Contents API response we
// care about. Other fields (sha, size, urls, etc.) are ignored.
type prFileContentResp struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// decodePRFileContent parses a GitHub Contents API response and returns
// the decoded file bytes. GitHub wraps base64 content at 60-char lines, so
// we strip whitespace before decoding.
func decodePRFileContent(raw []byte) ([]byte, error) {
	var resp prFileContentResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decoding contents API response: %w", err)
	}
	if resp.Encoding != "base64" {
		return nil, fmt.Errorf("unexpected contents encoding %q (want base64)", resp.Encoding)
	}
	// GitHub wraps the base64 payload at 60-char lines; the std base64
	// decoder rejects newlines, so collapse whitespace first.
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t', ' ':
			return -1
		}
		return r
	}, resp.Content)
	data, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("base64-decoding contents: %w", err)
	}
	return data, nil
}

// prURLRepoRe matches https://github.com/<owner>/<name>/pull/<num> with
// optional trailing slash, query string, or fragment.
var prURLRepoRe = regexp.MustCompile(`^https?://[^/]+/([^/]+)/([^/]+)/pull/\d+(?:[/?#].*)?$`)

// parseRepoFromPRURL extracts (owner, name) from a PR URL. Returns ok=false
// for unrecognizable inputs; callers should fall back to a local read.
func parseRepoFromPRURL(u string) (owner, name string, ok bool) {
	m := prURLRepoRe.FindStringSubmatch(u)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

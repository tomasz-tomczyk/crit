package session

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
)

// shareScope computes a hash of sorted file paths, used to detect when
// share state belongs to a different file set. Mirrors share.shareScope to
// avoid an import cycle (share imports session).
func shareScope(paths []string) string {
	sorted := make([]string, len(paths))
	copy(sorted, paths)
	sort.Strings(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, "\n")))
	return hex.EncodeToString(h[:8])
}

// tokenFromHostedURL extracts the review token from a hosted URL of the form
// https://crit.example/r/<token>. Mirrors share.tokenFromHostedURL.
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

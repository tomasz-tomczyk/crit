//go:build integration

package main

// Selfhosted (SELFHOSTED=true + OAuth provider configured) integration tests.
// These run against a crit-web instance booted by scripts/run-selfhosted-tests.sh
// on http://localhost:4001 with SELFHOSTED=true and a GitHub OAuth provider
// configured. In that mode:
//
//   - All /api/* endpoints (except /api/auth/*, /api/device/*, /health,
//     and the /api/test/* dev-only seeders) require a Bearer token.
//   - /r/:token redirects unauthenticated visitors to /auth/login.
//
// Helpers (seedUser, runCritShareEnv, envWithout, extractToken, …) are
// defined in share_attribution_test.go and share_integration_test.go and
// shared via the integration build tag.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noRedirectClient returns an http.Client that captures redirect responses
// without following them. We need this for the LiveView gate test.
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// seedReviewWithBearer creates a review on the selfhosted instance using the
// authenticated bearer token (anonymous POST /api/reviews would 401 in
// enforced mode). Returns the review token.
func seedReviewWithBearer(t *testing.T, baseURL, authToken string) string {
	t.Helper()
	payload := map[string]any{
		"files": []map[string]any{
			{"path": "plan.md", "content": "# Plan\n\nStep 1\n", "status": "modified"},
		},
		"comments":     []map[string]any{},
		"review_round": 1,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/reviews", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+authToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("seed review: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("seed review returned %d: %s", resp.StatusCode, raw)
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode seed review: %v", err)
	}
	if !strings.Contains(out.URL, "/r/") {
		t.Fatalf("seed review URL malformed: %q", out.URL)
	}
	return filepath.Base(out.URL)
}

// TestSelfhostedReviewPageRedirectsUnauthenticated verifies that GET /r/:token
// redirects an unauthenticated visitor to /auth/login?return_to=... when the
// instance is selfhosted+OAuth-enforced.
func TestSelfhostedReviewPageRedirectsUnauthenticated(t *testing.T) {
	baseURL := critWebURL(t)

	authToken, _, _ := seedUser(t, baseURL, "Owner For Redirect Test")
	token := seedReviewWithBearer(t, baseURL, authToken)

	client := noRedirectClient()
	reviewPath := "/r/" + token
	req, _ := http.NewRequest(http.MethodGet, baseURL+reviewPath, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", reviewPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 302/303 from %s, got %d", reviewPath, resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/auth/login") {
		t.Errorf("Location = %q, want prefix /auth/login", loc)
	}
	if !strings.Contains(loc, "return_to=") {
		t.Errorf("Location = %q, want return_to= query param", loc)
	}
	// Decoded return_to should reference the review path.
	if !strings.Contains(loc, token) && !strings.Contains(loc, "%2Fr%2F") {
		t.Errorf("Location = %q, want token %q or encoded /r/ in return_to", loc, token)
	}
}

// TestSelfhostedExportEndpointRequires401 verifies the export endpoint
// rejects unauthenticated requests with 401 + JSON body, and accepts a
// valid bearer token.
func TestSelfhostedExportEndpointRequires401(t *testing.T) {
	baseURL := critWebURL(t)

	authToken, _, _ := seedUser(t, baseURL, "Export Auth User")
	token := seedReviewWithBearer(t, baseURL, authToken)

	// Anonymous: 401 with JSON body.
	resp, err := http.Get(baseURL + "/api/export/" + token + "/review")
	if err != nil {
		t.Fatalf("anon export: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anon export status = %d, want 401", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode anon body: %v", err)
	}
	if body["error"] != "authentication required" {
		t.Errorf(`body[error] = %v, want "authentication required"`, body["error"])
	}

	// With bearer: 200 + markdown body (export_review returns text/markdown,
	// not JSON — test the auth gate, not the body shape).
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/export/"+token+"/review", nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authed export: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp2.Body)
		t.Fatalf("authed export status = %d, body: %s", resp2.StatusCode, raw)
	}
	raw, _ := io.ReadAll(resp2.Body)
	if len(raw) == 0 {
		t.Errorf("authed export returned empty body")
	}
}

// TestSelfhostedReviewDocumentApiRequires401 verifies the document and
// comments list endpoints reject anonymous requests in selfhosted mode.
func TestSelfhostedReviewDocumentApiRequires401(t *testing.T) {
	baseURL := critWebURL(t)

	authToken, _, _ := seedUser(t, baseURL, "Doc Api User")
	token := seedReviewWithBearer(t, baseURL, authToken)

	for _, suffix := range []string{"/document", "/comments"} {
		path := "/api/reviews/" + token + suffix
		t.Run(suffix, func(t *testing.T) {
			resp, err := http.Get(baseURL + path)
			if err != nil {
				t.Fatalf("anon GET %s: %v", path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Errorf("anon %s status = %d, want 401", path, resp.StatusCode)
			}
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
				if body["error"] != "authentication required" {
					t.Errorf(`%s body[error] = %v, want "authentication required"`, path, body["error"])
				}
			}

			// With bearer: 200.
			req, _ := http.NewRequest(http.MethodGet, baseURL+path, nil)
			req.Header.Set("Authorization", "Bearer "+authToken)
			resp2, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("authed GET %s: %v", path, err)
			}
			defer resp2.Body.Close()
			if resp2.StatusCode != http.StatusOK {
				raw, _ := io.ReadAll(resp2.Body)
				t.Errorf("authed %s status = %d, body: %s", path, resp2.StatusCode, raw)
			}
		})
	}
}

// TestSelfhostedShareRequiresAuthToken verifies that running `crit share`
// against a selfhosted instance WITHOUT a bearer token fails with auth-related
// output. Strips CRIT_AUTH_TOKEN and HOME from the env so cached creds can't
// leak in.
func TestSelfhostedShareRequiresAuthToken(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"plan.md": {}}})

	// Use empty fake HOME so the user's real ~/.crit.config.json (which may
	// hold a valid token) cannot satisfy the share.
	homeDir := t.TempDir()

	output, err := runCritShareEnv(t, binary, baseURL, dir, []string{"HOME=" + homeDir}, "plan.md")
	if err == nil {
		t.Fatalf("expected crit share to fail without auth in selfhosted mode; output:\n%s", output)
	}
	lower := strings.ToLower(output)
	if !strings.Contains(lower, "auth") && !strings.Contains(lower, "401") &&
		!strings.Contains(lower, "unauthor") && !strings.Contains(lower, "log in") &&
		!strings.Contains(lower, "login") {
		t.Errorf("expected auth-related error in output; got:\n%s", output)
	}
}

// TestSelfhostedShareSucceedsWithAuthToken verifies the full share path
// works with a bearer token, and that the resulting review is fetchable via
// the API (with auth) but redirects unauthenticated LiveView visitors.
func TestSelfhostedShareSucceedsWithAuthToken(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	authToken, userID, _ := seedUser(t, baseURL, "Sharer User")

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 3, EndLine: 3, Body: "needs detail", Scope: "line",
						UserID:    userID,
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	homeDir := t.TempDir()
	output, err := runCritShareEnv(t, binary, baseURL, dir,
		[]string{
			"CRIT_AUTH_TOKEN=" + authToken,
			"HOME=" + homeDir,
		}, "plan.md")
	if err != nil {
		t.Fatalf("crit share with auth failed: %v\n%s", err, output)
	}
	logReview(t, output)
	token := extractToken(t, output)

	// API surface (with bearer): 200 and content.
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/reviews/"+token+"/document", nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authed document fetch: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authed document status = %d", resp.StatusCode)
	}

	// LiveView surface (no bearer, no session): redirect to /auth/login.
	client := noRedirectClient()
	getResp, err := client.Get(baseURL + "/r/" + token)
	if err != nil {
		t.Fatalf("anon LiveView fetch: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusFound && getResp.StatusCode != http.StatusSeeOther {
		t.Errorf("anon /r/%s status = %d, want 302/303", token, getResp.StatusCode)
	}
	if loc := getResp.Header.Get("Location"); !strings.HasPrefix(loc, "/auth/login") {
		t.Errorf("anon /r/%s Location = %q, want /auth/login...", token, loc)
	}
}

// TestSelfhostedExportWithAuthToken exercises the full lifecycle: seed user,
// share with bearer, fetch document and comments via API with bearer,
// validate payload structure.
func TestSelfhostedExportWithAuthToken(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	authToken, userID, userName := seedUser(t, baseURL, "Lifecycle User")

	if err := os.WriteFile(filepath.Join(dir, "plan.md"),
		[]byte("# Plan\n\nStep 1\n\nStep 2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 3, EndLine: 3, Body: "lifecycle check", Scope: "line",
						UserID:    userID,
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	homeDir := t.TempDir()
	output, err := runCritShareEnv(t, binary, baseURL, dir,
		[]string{
			"CRIT_AUTH_TOKEN=" + authToken,
			"HOME=" + homeDir,
		}, "plan.md")
	if err != nil {
		t.Fatalf("crit share: %v\n%s", err, output)
	}
	logReview(t, output)
	token := extractToken(t, output)

	// /api/export/:token/review (auth required in selfhosted mode).
	// This endpoint returns markdown (not JSON); we only assert auth+nonempty.
	exportReviewURL := baseURL + "/api/export/" + token + "/review"
	req, _ := http.NewRequest(http.MethodGet, exportReviewURL, nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("export review: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("export review status = %d: %s", resp.StatusCode, raw)
	}
	mdBody, _ := io.ReadAll(resp.Body)
	if len(mdBody) == 0 {
		t.Errorf("export review returned empty body")
	}

	// /api/export/:token/comments (auth required in selfhosted mode).
	exportCommentsURL := baseURL + "/api/export/" + token + "/comments"
	req2, _ := http.NewRequest(http.MethodGet, exportCommentsURL, nil)
	req2.Header.Set("Authorization", "Bearer "+authToken)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("export comments: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp2.Body)
		t.Fatalf("export comments status = %d: %s", resp2.StatusCode, raw)
	}
	var exportBody map[string]any
	if err := json.NewDecoder(resp2.Body).Decode(&exportBody); err != nil {
		t.Fatalf("decode export comments: %v", err)
	}
	for _, key := range []string{"review_round", "share_url", "delete_token", "files"} {
		if exportBody[key] == nil {
			t.Errorf("export comments missing %q: %+v", key, exportBody)
		}
	}

	// Verify the comment we shared came back with our user attribution.
	files, _ := exportBody["files"].(map[string]any)
	plan, _ := files["plan.md"].(map[string]any)
	if plan == nil {
		t.Fatalf("export missing plan.md entry: %+v", files)
	}
	comments, _ := plan["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment in export, got %d: %+v", len(comments), comments)
	}
	c, _ := comments[0].(map[string]any)
	if body, _ := c["body"].(string); body != "lifecycle check" {
		t.Errorf("comment body = %q, want %q", body, "lifecycle check")
	}
	// `author` may be empty in the export; the real attribution we care about
	// is user_id linked to the authenticated seed user. Verify via the
	// authenticated comments API.
	req3, _ := http.NewRequest(http.MethodGet, baseURL+"/api/reviews/"+token+"/comments", nil)
	req3.Header.Set("Authorization", "Bearer "+authToken)
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatalf("comments fetch: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("comments fetch status = %d", resp3.StatusCode)
	}
	var wcs []webComment
	if err := json.NewDecoder(resp3.Body).Decode(&wcs); err != nil {
		t.Fatalf("decode comments: %v", err)
	}
	if len(wcs) != 1 {
		t.Fatalf("expected 1 comment via api, got %d", len(wcs))
	}
	if wcs[0].UserID != userID {
		t.Errorf("comment user_id = %q, want %q (authenticated user)", wcs[0].UserID, userID)
	}
	if wcs[0].AuthorDisplayName != userName {
		t.Errorf("comment author_display_name = %q, want %q", wcs[0].AuthorDisplayName, userName)
	}

}

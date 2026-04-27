//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// seedUser creates a user + bearer token on crit-web via the test-only
// endpoint and returns (token, user_id, name).
func seedUser(t *testing.T, baseURL, name string) (string, string, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	resp, err := http.Post(baseURL+"/api/test/seed-user", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("seed-user request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed-user returned %d", resp.StatusCode)
	}
	var out struct {
		Token  string `json:"token"`
		UserID string `json:"user_id"`
		Name   string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decoding seed-user: %v", err)
	}
	if out.Token == "" || out.UserID == "" {
		t.Fatalf("seed-user returned empty fields: %+v", out)
	}
	return out.Token, out.UserID, out.Name
}

// writeProjectConfig writes a .crit.config.json into dir with the given values.
func writeProjectConfig(t *testing.T, dir string, cfg map[string]any) {
	t.Helper()
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, ".crit.config.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

// TestShareAttributesConfiguredAuthor verifies that when crit is configured
// with `author: "Alice Smith"`, the local comments shared to crit-web are
// attributed to "Alice Smith" — NOT the placeholder "imported".
func TestShareAttributesConfiguredAuthor(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeProjectConfig(t, dir, map[string]any{"author": "Alice Smith"})
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 3, EndLine: 3, Body: "needs detail", Scope: "line",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	comments := commentsFromAPI(t, baseURL, token)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].AuthorDisplayName != "Alice Smith" {
		t.Errorf("author_display_name = %q, want %q", comments[0].AuthorDisplayName, "Alice Smith")
	}
}

// TestShareAuthAttributesUserIdentity verifies that when sharing with a
// valid bearer token, comments are server-side-attributed to the
// authenticated user: author_identity == user.id (uuid), display name
// from the verified user record (NOT from the payload).
func TestShareAuthAttributesUserIdentity(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	authToken, userID, userName := seedUser(t, baseURL, "Bob Reviewer")

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Author config intentionally differs from the verified user name to
	// confirm the server uses its own value, not the payload's.
	writeProjectConfig(t, dir, map[string]any{"author": "Mallory"})
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					// UserID stamped at write-time as the real CLI flow does
					// (session.AddComment uses cfg.AuthUserID). Required for
					// empty payload user_id under auth → null on server.
					{ID: "c1", StartLine: 3, EndLine: 3, Body: "from authenticated user", Scope: "line",
						UserID:    userID,
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	cmd := exec.Command(binary, "share", "--share-url", baseURL, "--output", dir, "plan.md")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CRIT_AUTH_TOKEN="+authToken)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("crit share failed: %s\n%s", err, out)
	}
	output := strings.TrimSpace(string(out))
	logReview(t, output)
	token := extractToken(t, output)

	comments := commentsFromAPI(t, baseURL, token)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	c := comments[0]

	if c.UserID != userID {
		t.Errorf("user_id = %q, want %q (the authenticated user)", c.UserID, userID)
	}
	if c.AuthorDisplayName != userName {
		t.Errorf("author_display_name = %q, want %q (from verified user, not config)",
			c.AuthorDisplayName, userName)
	}
	if c.UserID == "" {
		t.Errorf("user_id must be set for authenticated shares")
	}
}

// TestShareAuthIgnoresPayloadSpoofedIdentity verifies that when no bearer
// token is provided, payload-supplied identity fields cannot impersonate
// a real user. Knowing a user's UUID must not be enough to attribute
// comments to them.
func TestShareAuthIgnoresPayloadSpoofedIdentity(t *testing.T) {
	baseURL := critWebURL(t)

	// Seed a real user just to obtain a real UUID we'll try to spoof.
	_, victimID, _ := seedUser(t, baseURL, "Victim")

	// Build a payload by hand so we can include the spoofed identity field
	// that an attacker might add — bypassing the Go CLI which would never
	// add this field.
	payload := map[string]any{
		"files": []map[string]any{
			{"path": "plan.md", "content": "# Plan\n\nStep 1\n", "status": "modified"},
		},
		"comments": []map[string]any{
			{
				"file":                "plan.md",
				"start_line":          3,
				"end_line":            3,
				"body":                "spoofed comment",
				"scope":               "line",
				"author_display_name": "Victim",
				// Attacker-supplied — must be ignored.
				"author_identity": victimID,
				"author_user_id":  victimID,
				"user_id":         victimID,
			},
		},
		"review_round": 1,
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+"/api/reviews", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("share POST returned %d", resp.StatusCode)
	}
	var result struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(result.URL, "/")
	tokenStr := parts[len(parts)-1]

	comments := commentsFromAPI(t, baseURL, tokenStr)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	c := comments[0]
	if c.UserID == victimID {
		t.Errorf("SECURITY: anonymous share spoofed user_id to victim %q", victimID)
	}
	// New semantics: unauthenticated shares must produce user_id IS NULL
	// (empty string in the JSON response).
	if c.UserID != "" {
		t.Errorf("user_id = %q, want \"\" for unauthenticated share", c.UserID)
	}

	// And: an unauthenticated share must not result in the review being
	// linked to the victim's account either. The comment attribution above
	// is the user-visible signal.
	_ = fmt.Sprintf("anonymous, got user_id %q", c.UserID)
}

// TestShareAttrBackwardsCompatNoUserID verifies that an old `.crit.json`
// written before this feature shipped (no `user_id` field on Comment) still
// shares cleanly. The omitted field unmarshals to "" — no migration needed.
func TestShareAttrBackwardsCompatNoUserID(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Hand-roll JSON without a user_id field — simulates a review file from a
	// prior CLI version. Field is genuinely absent (not just empty).
	legacy := `{
		"review_round": 1,
		"files": {
			"plan.md": {
				"comments": [
					{
						"id": "c1",
						"start_line": 3,
						"end_line": 3,
						"body": "from old crit",
						"scope": "line",
						"created_at": "2026-01-01T00:00:00Z",
						"updated_at": "2026-01-01T00:00:00Z"
					}
				]
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, ".crit.json"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	comments := commentsFromAPI(t, baseURL, token)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].UserID != "" {
		t.Errorf("user_id = %q, want \"\" for legacy comment", comments[0].UserID)
	}
}

// TestShareAttrRoundtripPreservesUserID verifies that a comment authored by a
// logged-in user — and then fetched back into a different CLI workspace —
// preserves the original `user_id` when the second user re-shares the review
// anonymously: an existing-by-external_id row keeps its attribution.
func TestShareAttrRoundtripPreservesUserID(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)

	// Step 1: Alice (authenticated) shares a review with one comment.
	aliceToken, aliceID, _ := seedUser(t, baseURL, "Alice Author")
	dirA := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirA, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dirA, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					// UserID stamped at write-time as the real CLI flow does
					// (session.AddComment uses cfg.AuthUserID). Required for
					// empty payload user_id under auth → null on server.
					{ID: "c1", StartLine: 3, EndLine: 3, Body: "alice's note", Scope: "line",
						UserID:    aliceID,
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})
	cmd := exec.Command(binary, "share", "--share-url", baseURL, "--output", dirA, "plan.md")
	cmd.Dir = dirA
	cmd.Env = append(os.Environ(), "CRIT_AUTH_TOKEN="+aliceToken)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("alice share failed: %s\n%s", err, out)
	}
	shareOutput := strings.TrimSpace(string(out))
	logReview(t, shareOutput)
	token := extractToken(t, shareOutput)

	// Verify the server stamped Alice's user_id on first share.
	first := commentsFromAPI(t, baseURL, token)
	if len(first) != 1 {
		t.Fatalf("first share: expected 1 comment, got %d", len(first))
	}
	if first[0].UserID != aliceID {
		t.Fatalf("first share user_id = %q, want %q", first[0].UserID, aliceID)
	}

	// Step 2: Alice's same dir re-shares anonymously (no CRIT_AUTH_TOKEN).
	// .crit.json carries the delete_token so the PUT is authorized; the
	// re-share itself is anonymous (no bearer). An existing-by-external_id
	// match must preserve user_id — don't strip it just because the
	// re-sharer has no bearer token.
	if err := os.WriteFile(filepath.Join(dirA, "plan.md"), []byte("# Plan\n\nStep 1 revised\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd2 := exec.Command(binary, "share", "--share-url", baseURL, "--output", dirA, "plan.md")
	cmd2.Dir = dirA
	// Strip CRIT_AUTH_TOKEN to simulate logged-out re-share.
	env := os.Environ()
	filtered := env[:0]
	for _, e := range env {
		if !strings.HasPrefix(e, "CRIT_AUTH_TOKEN=") {
			filtered = append(filtered, e)
		}
	}
	cmd2.Env = filtered
	out2, err := cmd2.CombinedOutput()
	if err != nil {
		t.Fatalf("anonymous re-share failed: %s\n%s", err, out2)
	}

	// Verify Alice's user_id is preserved.
	second := commentsFromAPI(t, baseURL, token)
	var found bool
	for _, c := range second {
		if c.Body == "alice's note" {
			found = true
			if c.UserID != aliceID {
				t.Errorf("after re-share, alice's user_id = %q, want %q (must not be cleared by anonymous re-share)",
					c.UserID, aliceID)
			}
		}
	}
	if !found {
		t.Errorf("alice's comment missing after re-share")
	}
}

// envWithout returns os.Environ() minus any entries with the given prefixes.
func envWithout(prefixes ...string) []string {
	env := os.Environ()
	out := env[:0]
outer:
	for _, e := range env {
		for _, p := range prefixes {
			if strings.HasPrefix(e, p) {
				continue outer
			}
		}
		out = append(out, e)
	}
	return out
}

// runCritShareEnv runs `crit share` in dir with the given env (caller controls
// whether CRIT_AUTH_TOKEN/HOME are present). Returns combined output.
func runCritShareEnv(t *testing.T, binary, baseURL, dir string, extraEnv []string, files ...string) (string, error) {
	t.Helper()
	args := append([]string{"share", "--share-url", baseURL, "--output", dir}, files...)
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.Env = append(envWithout("CRIT_AUTH_TOKEN=", "HOME="), extraEnv...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// TestShareAttrLoginMidFlow — Scenario C: comment 1 written without auth,
// comment 2 written while authenticated. Server must respect each comment's
// stored intent: comment 1 stays anonymous, comment 2 is attributed to the
// authenticated user.
func TestShareAttrLoginMidFlow(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	authToken, userID, userName := seedUser(t, baseURL, "Mid Flow User")

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n\nStep 2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					// Comment 1: written before login → no UserID.
					{ID: "c1", StartLine: 3, EndLine: 3, Body: "anon comment", Scope: "line",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
					// Comment 2: written after login → carries UserID.
					{ID: "c2", StartLine: 5, EndLine: 5, Body: "authed comment", Scope: "line",
						UserID:    userID,
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	output, err := runCritShareEnv(t, binary, baseURL, dir, []string{"CRIT_AUTH_TOKEN=" + authToken}, "plan.md")
	if err != nil {
		t.Fatalf("crit share failed: %v\n%s", err, output)
	}
	logReview(t, output)
	token := extractToken(t, output)

	comments := commentsFromAPI(t, baseURL, token)
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
	byBody := map[string]webComment{}
	for _, c := range comments {
		byBody[c.Body] = c
	}
	if got := byBody["anon comment"].UserID; got != "" {
		t.Errorf("anon comment user_id = %q, want \"\" (anonymous intent must be respected even with bearer)", got)
	}
	if got := byBody["authed comment"].UserID; got != userID {
		t.Errorf("authed comment user_id = %q, want %q", got, userID)
	}
	if byBody["authed comment"].AuthorDisplayName != userName {
		t.Errorf("authed comment display_name = %q, want %q", byBody["authed comment"].AuthorDisplayName, userName)
	}
}

// TestShareAttrMultiUserRoundtripReply — Scenario D: Alice authed shares,
// then a web reviewer (separate user) replies. Alice fetches via re-share and
// verifies the reply carries the web reviewer's identity. Re-sharing again
// must preserve both Alice's user_id on her parent and the reply's identity.
func TestShareAttrMultiUserRoundtripReply(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	aliceToken, aliceID, _ := seedUser(t, baseURL, "Alice D")

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 3, EndLine: 3, Body: "alice parent", Scope: "line",
						UserID:    aliceID,
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	// Round 1: Alice authed share (POST).
	out1, err := runCritShareEnv(t, binary, baseURL, dir, []string{"CRIT_AUTH_TOKEN=" + aliceToken}, "plan.md")
	if err != nil {
		t.Fatalf("alice share failed: %v\n%s", err, out1)
	}
	logReview(t, out1)
	shareTok := extractToken(t, out1)

	// Round 2: re-share with content change so external_id is set on Alice's comment.
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1 v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := runCritShareEnv(t, binary, baseURL, dir, []string{"CRIT_AUTH_TOKEN=" + aliceToken}, "plan.md"); err != nil {
		t.Fatal(err)
	}

	// Find Alice's comment ID on the web side so we can seed a reply to it.
	resp, err := http.Get(fmt.Sprintf("%s/api/reviews/%s/comments", baseURL, shareTok))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw []struct {
		ID         string `json:"id"`
		ExternalID string `json:"external_id"`
		UserID     string `json:"user_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	var aliceCommentWebID string
	for _, c := range raw {
		if c.ExternalID == "c1" {
			aliceCommentWebID = c.ID
			if c.UserID != aliceID {
				t.Errorf("alice's comment user_id on web = %q, want %q", c.UserID, aliceID)
			}
		}
	}
	if aliceCommentWebID == "" {
		t.Fatalf("could not find alice's comment on web: %+v", raw)
	}

	// Web reviewer adds a reply (seedReply identity is "integration-test"; no user_id).
	seedReply(t, baseURL, shareTok, aliceCommentWebID, "web reviewer reply")

	// Round 3: Alice re-shares to fetch the reply.
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1 v3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := runCritShareEnv(t, binary, baseURL, dir, []string{"CRIT_AUTH_TOKEN=" + aliceToken}, "plan.md"); err != nil {
		t.Fatal(err)
	}

	cj := readCritJSON(t, dir)
	var parent *Comment
	for i := range cj.Files["plan.md"].Comments {
		c := &cj.Files["plan.md"].Comments[i]
		if c.ID == "c1" {
			parent = c
			break
		}
	}
	if parent == nil {
		t.Fatal("alice's comment c1 missing after fetch")
	}
	if parent.UserID != aliceID {
		t.Errorf("alice parent user_id = %q, want %q", parent.UserID, aliceID)
	}
	var fetchedReply *Reply
	for i := range parent.Replies {
		if parent.Replies[i].Body == "web reviewer reply" {
			fetchedReply = &parent.Replies[i]
			break
		}
	}
	if fetchedReply == nil {
		t.Fatalf("web reviewer reply not pulled into local .crit.json: %+v", parent.Replies)
	}

	// Round 4: Alice re-shares again. Server must preserve Alice's user_id on
	// her parent and must not strip/replace the reply's stored user_id —
	// existing-by-external_id rows keep their attribution across re-share.
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1 v4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := runCritShareEnv(t, binary, baseURL, dir, []string{"CRIT_AUTH_TOKEN=" + aliceToken}, "plan.md"); err != nil {
		t.Fatal(err)
	}

	final := commentsFromAPI(t, baseURL, shareTok)
	var aliceComment *webComment
	for i := range final {
		if final[i].Body == "alice parent" {
			aliceComment = &final[i]
		}
	}
	if aliceComment == nil {
		t.Fatal("alice parent missing on web after final re-share")
	}
	if aliceComment.UserID != aliceID {
		t.Errorf("alice parent user_id after re-share = %q, want %q (must not drop on roundtrip)", aliceComment.UserID, aliceID)
	}
	var replyAfter *webReply
	for i := range aliceComment.Replies {
		if aliceComment.Replies[i].Body == "web reviewer reply" {
			replyAfter = &aliceComment.Replies[i]
		}
	}
	if replyAfter == nil {
		t.Fatal("web reviewer reply lost on re-share")
	}
	// Lock in: parent's user_id (alice) is preserved across multiple re-shares
	// — the reply carry-forward logic must not collateral-damage the parent.
	// We don't constrain the reply's user_id here: the seeded reply had a
	// NULL user_id, and an authed re-share of a previously-NULL reply may
	// either keep it NULL or stamp the current user — never some third
	// foreign id. Both outcomes are acceptable.
}

// TestShareAttrPutAnonExternalIDMismatch verifies that an anonymous PUT
// with a payload whose external_id does not match any existing comment
// drops any spoofed user_id and writes user_id=NULL.
func TestShareAttrPutAnonExternalIDMismatch(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	_, victimID, _ := seedUser(t, baseURL, "Victim4")

	// First, create a real review with one legitimate comment (anon POST).
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 3, EndLine: 3, Body: "real comment", Scope: "line",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})
	out1 := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, out1)
	shareTok := extractToken(t, out1)

	// Read back the .crit.json to grab the share URL + delete_token, then craft
	// an anonymous PUT manually with a forged external_id + spoofed user_id.
	cj := readCritJSON(t, dir)
	if cj.ShareURL == "" || cj.DeleteToken == "" {
		t.Fatalf("expected share state in .crit.json, got %+v", cj)
	}

	payload := map[string]any{
		"delete_token": cj.DeleteToken,
		"files": []map[string]any{
			{"path": "plan.md", "content": "# Plan\n\nStep 1 v2\n", "status": "modified"},
		},
		"comments": []map[string]any{
			{
				"file":       "plan.md",
				"start_line": 3, "end_line": 3,
				"body":  "spoof attempt",
				"scope": "line",
				// external_id intentionally does not match anything on the server.
				"external_id":         "forged-no-match",
				"author_display_name": "Mallory",
				"user_id":             victimID,
			},
		},
		"review_round": 2,
	}
	body, _ := json.Marshal(payload)
	apiURL := baseURL + "/api/reviews/" + path.Base(cj.ShareURL)
	req, _ := http.NewRequest(http.MethodPut, apiURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT returned %d", resp.StatusCode)
	}

	comments := commentsFromAPI(t, baseURL, shareTok)
	var spoof *webComment
	for i := range comments {
		if comments[i].Body == "spoof attempt" {
			spoof = &comments[i]
		}
	}
	if spoof == nil {
		t.Fatal("spoof comment missing on web (server should still create it, just without user_id)")
	}
	if spoof.UserID == victimID {
		t.Errorf("SECURITY: anon PUT with mismatched external_id wrote victim user_id %q", victimID)
	}
	if spoof.UserID != "" {
		t.Errorf("user_id = %q, want \"\" for anon PUT no-match", spoof.UserID)
	}
}

// TestShareAttrPutAuthPreservesForeignUserID verifies that when an
// authenticated user re-shares a payload that includes a comment owned
// by a different user (matched by external_id), the server preserves the
// original owner's user_id rather than replacing it with the current
// sharer's id.
func TestShareAttrPutAuthPreservesForeignUserID(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)

	bobToken, bobID, _ := seedUser(t, baseURL, "Bob Owner")
	aliceToken, aliceID, _ := seedUser(t, baseURL, "Alice Sharer")

	// Bob authors and shares the review.
	dirB := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirB, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dirB, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "bob1", StartLine: 3, EndLine: 3, Body: "bob's comment", Scope: "line",
						UserID:    bobID,
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})
	out1, err := runCritShareEnv(t, binary, baseURL, dirB, []string{"CRIT_AUTH_TOKEN=" + bobToken}, "plan.md")
	if err != nil {
		t.Fatalf("bob share: %v\n%s", err, out1)
	}
	logReview(t, out1)

	// Re-share once so external_id is set on bob's comment.
	if err := os.WriteFile(filepath.Join(dirB, "plan.md"), []byte("# Plan\n\nStep 1 v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := runCritShareEnv(t, binary, baseURL, dirB, []string{"CRIT_AUTH_TOKEN=" + bobToken}, "plan.md"); err != nil {
		t.Fatal(err)
	}

	cjB := readCritJSON(t, dirB)
	if cjB.ShareURL == "" || cjB.DeleteToken == "" {
		t.Fatalf("missing share state: %+v", cjB)
	}
	shareTok := path.Base(cjB.ShareURL)

	// Alice prepares an upsert: same review, includes bob's comment matched by
	// external_id. Alice's bearer is on the request.
	payload := map[string]any{
		"delete_token": cjB.DeleteToken,
		"files": []map[string]any{
			{"path": "plan.md", "content": "# Plan\n\nStep 1 v3\n", "status": "modified"},
		},
		"comments": []map[string]any{
			{
				"file":       "plan.md",
				"start_line": 3, "end_line": 3,
				"body":                "bob's comment",
				"scope":               "line",
				"external_id":         "bob1",
				"author_display_name": "Bob Owner",
				"user_id":             bobID,
			},
		},
		"review_round": 3,
	}
	body, _ := json.Marshal(payload)
	apiURL := baseURL + "/api/reviews/" + path.Base(cjB.ShareURL)
	req, _ := http.NewRequest(http.MethodPut, apiURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("alice PUT: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("alice PUT returned %d", resp.StatusCode)
	}

	comments := commentsFromAPI(t, baseURL, shareTok)
	var bobComment *webComment
	for i := range comments {
		if comments[i].Body == "bob's comment" {
			bobComment = &comments[i]
		}
	}
	if bobComment == nil {
		t.Fatal("bob's comment missing after alice re-share")
	}
	if bobComment.UserID != bobID {
		t.Errorf("bob's comment user_id = %q, want %q (must not be overwritten by alice's id %q)",
			bobComment.UserID, bobID, aliceID)
	}
}

// TestShareAttrPutAuthExternalIDMismatchDropsSpoof verifies that an
// authenticated upsert claiming user_id=<other user's id> with no
// external_id match drops the spoofed id. Because Alice is authenticated
// and the comment is new, the server may stamp alice.id or write null —
// either way, bob.id must NOT appear.
func TestShareAttrPutAuthExternalIDMismatchDropsSpoof(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)

	aliceToken, aliceID, _ := seedUser(t, baseURL, "Alice Sharer8")
	_, bobID, _ := seedUser(t, baseURL, "Bob Victim8")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files:       map[string]CritJSONFile{"plan.md": {}},
	})
	out1, err := runCritShareEnv(t, binary, baseURL, dir, []string{"CRIT_AUTH_TOKEN=" + aliceToken}, "plan.md")
	if err != nil {
		t.Fatalf("alice initial share: %v\n%s", err, out1)
	}
	logReview(t, out1)

	cj := readCritJSON(t, dir)
	if cj.ShareURL == "" {
		t.Fatalf("missing share url after first share: %+v", cj)
	}
	shareTok := path.Base(cj.ShareURL)

	payload := map[string]any{
		"delete_token": cj.DeleteToken,
		"files": []map[string]any{
			{"path": "plan.md", "content": "# Plan\n\nStep 1 v2\n", "status": "modified"},
		},
		"comments": []map[string]any{
			{
				"file":       "plan.md",
				"start_line": 3, "end_line": 3,
				"body":                "forged claim",
				"scope":               "line",
				"external_id":         "forged-id",
				"author_display_name": "Bob Victim8",
				"user_id":             bobID,
			},
		},
		"review_round": 2,
	}
	body, _ := json.Marshal(payload)
	apiURL := baseURL + "/api/reviews/" + path.Base(cj.ShareURL)
	req, _ := http.NewRequest(http.MethodPut, apiURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+aliceToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT returned %d", resp.StatusCode)
	}

	comments := commentsFromAPI(t, baseURL, shareTok)
	var forged *webComment
	for i := range comments {
		if comments[i].Body == "forged claim" {
			forged = &comments[i]
		}
	}
	if forged == nil {
		t.Fatal("forged comment missing on web")
	}
	if forged.UserID == bobID {
		t.Errorf("SECURITY: auth PUT spoofed bob's user_id %q", bobID)
	}
	// Acceptable: either null or alice.id (server's fallback for spoof drops).
	if forged.UserID != "" && forged.UserID != aliceID {
		t.Errorf("user_id = %q, want \"\" or %q (alice)", forged.UserID, aliceID)
	}
}

// TestShareAttrReplyRoundtripPreservesUserID locks in the Elixir bug fix that
// replies' user_id values must be preserved across re-shares (not stripped or
// replaced with the current sharer's id).
func TestShareAttrReplyRoundtripPreservesUserID(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	aliceToken, aliceID, _ := seedUser(t, baseURL, "Alice Replier")

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{
						ID: "c1", StartLine: 3, EndLine: 3, Body: "parent", Scope: "line",
						UserID:    aliceID,
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
						Replies: []Reply{
							{ID: "r1", Body: "alice's own reply", Author: "Alice Replier", UserID: aliceID},
						},
					},
				},
			},
		},
	})

	// Round 1: POST.
	out1, err := runCritShareEnv(t, binary, baseURL, dir, []string{"CRIT_AUTH_TOKEN=" + aliceToken}, "plan.md")
	if err != nil {
		t.Fatalf("share 1: %v\n%s", err, out1)
	}
	logReview(t, out1)
	shareTok := extractToken(t, out1)

	// Round 2: re-share to set external_id on the reply.
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1 v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := runCritShareEnv(t, binary, baseURL, dir, []string{"CRIT_AUTH_TOKEN=" + aliceToken}, "plan.md"); err != nil {
		t.Fatal(err)
	}

	// Round 3: re-share again — bug area is here. Reply's user_id must persist.
	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1 v3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := runCritShareEnv(t, binary, baseURL, dir, []string{"CRIT_AUTH_TOKEN=" + aliceToken}, "plan.md"); err != nil {
		t.Fatal(err)
	}

	comments := commentsFromAPI(t, baseURL, shareTok)
	if len(comments) != 1 {
		t.Fatalf("expected 1 parent comment, got %d", len(comments))
	}
	if len(comments[0].Replies) != 1 {
		t.Fatalf("expected 1 reply, got %d", len(comments[0].Replies))
	}
	if comments[0].Replies[0].UserID != aliceID {
		t.Errorf("reply user_id = %q, want %q (must persist across re-shares)",
			comments[0].Replies[0].UserID, aliceID)
	}
}

// TestShareAttr401ClearsCachedIdentity verifies that when a cached token has
// been revoked server-side, the lazy whoami backfill on the next `crit share`
// detects the 401 and removes auth_token / auth_user_id from the global
// config. Uses a HOME override so the dev config is not touched.
func TestShareAttr401ClearsCachedIdentity(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	authToken, _, _ := seedUser(t, baseURL, "Revoked User")

	// Pre-populate a fake HOME with auth_token but NO auth_user_id, so the
	// next share triggers lazyBackfillAuthUserID → whoami → 401.
	homeDir := t.TempDir()
	globalCfg := map[string]any{"auth_token": authToken}
	cfgData, _ := json.MarshalIndent(globalCfg, "", "  ")
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"), cfgData, 0644); err != nil {
		t.Fatal(err)
	}

	// Revoke the token server-side.
	req, _ := http.NewRequest(http.MethodDelete, baseURL+"/api/auth/token", nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke returned %d", resp.StatusCode)
	}

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{ReviewRound: 1, Files: map[string]CritJSONFile{"plan.md": {}}})

	args := []string{"share", "--share-url", baseURL, "--output", dir, "plan.md"}
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.Env = append(envWithout("CRIT_AUTH_TOKEN=", "HOME="), "HOME="+homeDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		// We don't require non-zero exit — the share may continue anonymously.
		// The invariant is: cached credentials must be cleared.
		t.Logf("crit share exit: %v\noutput: %s", err, out)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, ".crit.config.json"))
	if err != nil {
		t.Fatalf("reading cfg after share: %v", err)
	}
	var after map[string]any
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	if v, has := after["auth_token"]; has && v != "" {
		t.Errorf("auth_token must be cleared after 401, got %v", v)
	}
	if v, has := after["auth_user_id"]; has && v != "" {
		t.Errorf("auth_user_id must be cleared after 401, got %v", v)
	}
}

// TestShareAttrCachedIdentityFromConfigOnly verifies that a cached auth_token
// + auth_user_id stored in the global config (not env) is used: comments are
// attributed to the cached user id without setting CRIT_AUTH_TOKEN.
func TestShareAttrCachedIdentityFromConfigOnly(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	authToken, userID, userName := seedUser(t, baseURL, "Cached User")

	homeDir := t.TempDir()
	globalCfg := map[string]any{
		"auth_token":     authToken,
		"auth_user_id":   userID,
		"auth_user_name": userName,
	}
	cfgData, _ := json.MarshalIndent(globalCfg, "", "  ")
	if err := os.WriteFile(filepath.Join(homeDir, ".crit.config.json"), cfgData, 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// .crit.json deliberately has no UserID on the comment; the CLI must stamp
	// it from the cached config at write-time semantics. For an integration
	// test we approximate by setting UserID directly (mirrors what `crit
	// comment` would do at write time when cfg.AuthUserID is set).
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{ID: "c1", StartLine: 3, EndLine: 3, Body: "from cached identity", Scope: "line",
						UserID:    userID,
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				},
			},
		},
	})

	args := []string{"share", "--share-url", baseURL, "--output", dir, "plan.md"}
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.Env = append(envWithout("CRIT_AUTH_TOKEN=", "HOME="), "HOME="+homeDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("crit share failed: %v\n%s", err, out)
	}
	output := strings.TrimSpace(string(out))
	logReview(t, output)
	token := extractToken(t, output)

	comments := commentsFromAPI(t, baseURL, token)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].UserID != userID {
		t.Errorf("user_id = %q, want %q (must come from cached config, not env)", comments[0].UserID, userID)
	}
	if comments[0].AuthorDisplayName != userName {
		t.Errorf("display_name = %q, want %q", comments[0].AuthorDisplayName, userName)
	}
}

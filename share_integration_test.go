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

func critWebURL(t *testing.T) string {
	t.Helper()
	if u := os.Getenv("CRIT_WEB_URL"); u != "" {
		return u
	}
	return "http://localhost:4000"
}

func critBinary(t *testing.T) string {
	t.Helper()
	if b := os.Getenv("CRIT_BINARY"); b != "" {
		return b
	}
	// Default: built binary next to the test
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "crit")
}

// TestShareSyncIntegration exercises the full share -> review -> re-share loop.
func TestShareSyncIntegration(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	// a) Create a plan with a pre-resolved local comment
	planPath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(planPath, []byte("# Plan\n\nSection 1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	initialCJ := CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {
				Comments: []Comment{
					{
						ID: "c1", StartLine: 3, EndLine: 3,
						Body: "resolved local comment", Resolved: true, ReviewRound: 1,
						Scope:     "line",
						CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
					},
				},
			},
		},
	}
	data, _ := json.MarshalIndent(initialCJ, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, ".crit.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	// b) Share to local crit-web (first share = POST, creates review)
	cmd := exec.Command(binary, "share", "--share-url", baseURL, "plan.md")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("crit share failed: %s\n%s", err, out)
	}

	// First share output is just the URL
	shareOutput := strings.TrimSpace(string(out))
	// Extract the URL — it may be preceded by warnings on stderr
	lines := strings.Split(shareOutput, "\n")
	shareURL := lines[len(lines)-1]
	if !strings.Contains(shareURL, "/r/") {
		t.Fatalf("expected a review URL, got: %s", shareURL)
	}
	token := path.Base(shareURL)
	t.Logf("Shared to: %s (token: %s)", shareURL, token)

	// c) Simulate a web reviewer adding a new comment via seed-comment endpoint
	seedPayload, _ := json.Marshal(map[string]any{
		"file": "plan.md", "start_line": 1, "end_line": 1,
		"body": "web reviewer comment",
	})
	seedResp, err := http.Post(
		baseURL+"/api/reviews/"+token+"/seed-comment",
		"application/json", bytes.NewReader(seedPayload),
	)
	if err != nil {
		t.Fatalf("seed-comment request failed: %v", err)
	}
	if seedResp.StatusCode != http.StatusOK {
		t.Fatalf("seed-comment returned %d", seedResp.StatusCode)
	}
	seedResp.Body.Close()

	// d) Agent applies changes locally — update the plan
	if err := os.WriteFile(planPath, []byte("# Plan\n\nSection 1 (revised)\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// e) Re-share: crit share should pull web comment, push new round
	cmd2 := exec.Command(binary, "share", "--share-url", baseURL, "plan.md")
	cmd2.Dir = dir
	out2, err := cmd2.CombinedOutput()
	if err != nil {
		t.Fatalf("second crit share failed: %s\n%s", err, out2)
	}
	output2 := string(out2)
	t.Logf("Second share output: %s", output2)
	if !strings.Contains(output2, "Updated (round 2)") {
		t.Errorf("expected 'Updated (round 2)' in output, got: %s", output2)
	}

	// f) Verify crit-web state: latest file content should be updated
	docResp, err := http.Get(baseURL + "/api/reviews/" + token + "/document")
	if err != nil {
		t.Fatalf("document request failed: %v", err)
	}
	defer docResp.Body.Close()
	if docResp.StatusCode != http.StatusOK {
		t.Fatalf("document returned %d", docResp.StatusCode)
	}

	var docBody struct {
		Files []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"files"`
	}
	if err := json.NewDecoder(docResp.Body).Decode(&docBody); err != nil {
		t.Fatalf("decoding document response: %v", err)
	}
	if len(docBody.Files) == 0 {
		t.Fatal("expected at least one file in document response")
	}
	if !strings.Contains(docBody.Files[0].Content, "Section 1 (revised)") {
		t.Errorf("crit-web should have updated content, got: %s", docBody.Files[0].Content)
	}

	// g) Verify the web reviewer comment was pulled into local .crit.json
	localData, err := os.ReadFile(filepath.Join(dir, ".crit.json"))
	if err != nil {
		t.Fatalf("reading .crit.json: %v", err)
	}
	if !strings.Contains(string(localData), "web reviewer comment") {
		t.Errorf("expected web reviewer comment in local .crit.json, got: %s", string(localData))
	}

	// h) Verify export endpoint returns .crit.json-compatible shape
	exportResp, err := http.Get(baseURL + "/api/export/" + token + "/comments")
	if err != nil {
		t.Fatalf("export request failed: %v", err)
	}
	defer exportResp.Body.Close()
	if exportResp.StatusCode != http.StatusOK {
		t.Fatalf("export returned %d", exportResp.StatusCode)
	}

	var exportBody map[string]any
	if err := json.NewDecoder(exportResp.Body).Decode(&exportBody); err != nil {
		t.Fatalf("decoding export response: %v", err)
	}

	// Top-level .crit.json fields must be present
	if exportBody["review_round"] == nil {
		t.Error("export missing review_round")
	}
	if exportBody["share_url"] == nil {
		t.Error("export missing share_url")
	}
	if exportBody["delete_token"] == nil {
		t.Error("export missing delete_token")
	}

	// Comment shape must use "author" not "author_display_name"
	files, _ := exportBody["files"].(map[string]any)
	for _, fileEntry := range files {
		entry, _ := fileEntry.(map[string]any)
		comments, _ := entry["comments"].([]any)
		for _, raw := range comments {
			c, _ := raw.(map[string]any)
			if _, hasOld := c["author_display_name"]; hasOld {
				t.Error("export comment must not have author_display_name — use author instead")
			}
			if _, hasOld := c["author_identity"]; hasOld {
				t.Error("export comment must not have author_identity")
			}
			if _, hasAuthor := c["author"]; !hasAuthor {
				t.Error("export comment missing author field")
			}
		}
	}

	t.Logf("Share sync integration test passed. Review URL: %s", shareURL)
}

// seedComment is a helper for integration tests to simulate a web reviewer comment.
func seedComment(t *testing.T, baseURL, token, file, body string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"file": file, "start_line": 1, "end_line": 1, "body": body,
	})
	resp, err := http.Post(baseURL+"/api/reviews/"+token+"/seed-comment", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("seed-comment failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed-comment returned %d", resp.StatusCode)
	}
}

// reviewRoundFromAPI fetches the current review_round for a token from crit-web.
func reviewRoundFromAPI(t *testing.T, baseURL, token string) int {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/api/reviews/%s/document", baseURL, token))
	if err != nil {
		t.Fatalf("document request failed: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		ReviewRound int `json:"review_round"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding document: %v", err)
	}
	return body.ReviewRound
}

package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPreLastPushedBodySchema_ParsesCleanly verifies that a review file
// written by a pre-#446 crit version (no last_pushed_body fields) parses
// cleanly into the current CritJSON struct without losing data.
// Forward-compat probe for the schema migration discussed in issue #446.
//
// On this branch (pr-roundtrip-harness), the LastPushedBody field does not
// yet exist on Comment/Reply. The test still runs as a forward-compat probe:
// it asserts that the legacy on-disk shape parses, and that re-marshalling
// does not introduce the field before #446 lands. Once #446 ships, this
// test should also assert that LastPushedBody is empty after parsing legacy
// records (so the "empty == in-sync" rule kicks in and no spurious PATCH
// happens for legacy comments). See TODO below.
func TestPreLastPushedBodySchema_ParsesCleanly(t *testing.T) {
	const oldFile = `{
  "branch": "test",
  "base_ref": "main",
  "review_round": 1,
  "files": {
    "sample.go": {
      "status": "modified",
      "file_hash": "deadbeef",
      "comments": [
        {
          "id": "c_aaaa",
          "github_id": 12345,
          "start_line": 1,
          "end_line": 1,
          "body": "old comment",
          "author": "tomasz",
          "user_id": "00000000-0000-0000-0000-000000000001",
          "scope": "line",
          "created_at": "2025-01-01T00:00:00Z",
          "updated_at": "2025-01-01T00:00:00Z",
          "replies": [
            {
              "id": "rp_bbbb",
              "github_id": 67890,
              "body": "old reply",
              "author": "reviewer",
              "user_id": "00000000-0000-0000-0000-000000000002",
              "created_at": "2025-01-01T00:00:00Z"
            }
          ]
        }
      ]
    }
  }
}`

	var cj CritJSON
	if err := json.Unmarshal([]byte(oldFile), &cj); err != nil {
		t.Fatalf("legacy review file failed to parse: %v", err)
	}

	f, ok := cj.Files["sample.go"]
	if !ok {
		t.Fatalf("sample.go missing from parsed files: %+v", cj.Files)
	}
	if len(f.Comments) != 1 {
		t.Fatalf("expected 1 comment in sample.go, got %d", len(f.Comments))
	}
	c := f.Comments[0]
	if c.GitHubID != 12345 {
		t.Errorf("github_id lost: got %d", c.GitHubID)
	}
	if c.Body != "old comment" {
		t.Errorf("body lost: got %q", c.Body)
	}
	if c.Author != "tomasz" {
		t.Errorf("author lost: got %q", c.Author)
	}
	if len(c.Replies) != 1 {
		t.Fatalf("reply lost: %+v", c.Replies)
	}
	if c.Replies[0].GitHubID != 67890 {
		t.Errorf("reply github_id lost: got %d", c.Replies[0].GitHubID)
	}
	if c.Replies[0].Body != "old reply" {
		t.Errorf("reply body lost: got %q", c.Replies[0].Body)
	}

	// Re-marshal and verify last_pushed_body is absent for legacy-loaded
	// records. Pre-#446 this passes trivially (the field doesn't exist).
	// Post-#446 it still passes because LastPushedBody should be omitempty
	// and stay empty when not present in the source JSON. If #446 lands and
	// uses a non-omitempty tag or initializes the field on parse, this
	// assertion will fail and flag the schema bug.
	out, err := json.Marshal(&c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), `"last_pushed_body"`) {
		t.Errorf("re-marshalled legacy comment unexpectedly contains last_pushed_body: %s", out)
	}
	outR, err := json.Marshal(&c.Replies[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(outR), `"last_pushed_body"`) {
		t.Errorf("re-marshalled legacy reply unexpectedly contains last_pushed_body: %s", outR)
	}

	// TODO(#446): once the LastPushedBody field is added to Comment/Reply,
	// extend this test to assert:
	//   - c.LastPushedBody == "" after parsing the legacy file
	//   - the "empty LastPushedBody == in-sync" rule means a hypothetical
	//     edited-comment scan returns 0 PATCH candidates for this record
	// Until then, the assertions above pin the forward-compat contract
	// (no field invented during parse, no field invented on re-marshal).
}

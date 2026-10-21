//go:build integration

package share

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// subThreadsFromAPI fetches a review's comments from crit-web as raw JSON
// maps, nested sub-threads included.
func subThreadsFromAPI(t *testing.T, baseURL, token string) []map[string]any {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/api/reviews/%s/comments", baseURL, token))
	if err != nil {
		t.Fatalf("comments request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("comments returned %d", resp.StatusCode)
	}
	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatalf("decoding comments: %v", err)
	}
	return threads
}

func jsonList(v any) []map[string]any {
	items, _ := v.([]any)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func threadByExternalID(t *testing.T, threads []map[string]any, id string) map[string]any {
	t.Helper()
	for _, th := range threads {
		if th["external_id"] == id {
			return th
		}
	}
	t.Fatalf("no thread %s in %v", id, threads)
	return nil
}

// seedSubThread writes a sub-thread on crit-web, on the comment or reply
// anchorID (its crit-web id), as a web reviewer would.
func seedSubThread(t *testing.T, baseURL, token, anchorID, body, quote string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"body": body, "quote": quote})
	resp, err := http.Post(baseURL+"/api/reviews/"+token+"/seed-sub-thread/"+anchorID, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("seed-sub-thread failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed-sub-thread returned %d (crit-web without sub-threads?)", resp.StatusCode)
	}
}

func localSubThreads(t *testing.T, dir string) map[string]Comment {
	t.Helper()
	out := make(map[string]Comment)
	for _, c := range readCritJSON(t, dir).Files["plan.md"].Comments {
		if c.ParentID != "" {
			if _, dup := out[c.Body]; dup {
				t.Errorf("sub-thread %q imported twice", c.Body)
			}
			out[c.Body] = c
		}
	}
	return out
}

// TestShareSyncSubThreads covers the round trip of sub-threads: shared nested
// under their messages (an open one keeping its resolved parent), written on
// the web, fetched once into crit under the right message, and kept on their
// anchors by every re-share.
func TestShareSyncSubThreads(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n\nStep 2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{
				{ID: "c_open", StartLine: 3, EndLine: 3, Body: "expand this", Scope: "line",
					CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
					Replies: []Reply{{ID: "rp_answer", Body: "done, see step 1", Author: "agent"}}},
				{ID: "c_sub", ParentID: "rp_answer", Body: "which step?", Quote: "step 1", Scope: "line",
					CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
					Replies: []Reply{{ID: "rp_sub", Body: "the first", Author: "agent"}}},
				{ID: "c_done", StartLine: 5, EndLine: 5, Body: "resolved point", Scope: "line", Resolved: true,
					CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				{ID: "c_still_open", ParentID: "c_done", Body: "not quite", Quote: "resolved", Scope: "line",
					CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
			}},
		},
	})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	threads := subThreadsFromAPI(t, baseURL, token)
	if len(threads) != 2 {
		t.Fatalf("top-level threads = %d, want 2 (sub-threads stay nested): %v", len(threads), threads)
	}
	for _, th := range threads {
		if th["anchor_comment_id"] != nil {
			t.Errorf("a sub-thread reached the top level, where an older crit would import it: %v", th)
		}
	}
	open := threadByExternalID(t, threads, "c_open")
	answer := jsonList(open["replies"])[0]
	if answer["external_id"] != "rp_answer" {
		t.Fatalf("reply = %v, want its external_id", answer)
	}
	if subs := jsonList(answer["sub_threads"]); len(subs) != 1 || subs[0]["external_id"] != "c_sub" || subs[0]["quote"] != "step 1" {
		t.Fatalf("sub-threads of the reply = %v", subs)
	}
	done := threadByExternalID(t, threads, "c_done")
	if done["resolved"] != true || len(jsonList(done["sub_threads"])) != 1 {
		t.Errorf("resolved parent of an open sub-thread = %v", done)
	}

	// A web reviewer opens a sub-thread on the agent's answer.
	seedSubThread(t, baseURL, token, answer["id"].(string), "web sub-thread", "see step 1")

	for range 2 {
		if out, err := runCritCmd(t, binary, dir, "fetch", "--output", dir); err != nil {
			t.Fatalf("crit fetch failed: %v\n%s", err, out)
		}
	}
	subs := localSubThreads(t, dir)
	web, ok := subs["web sub-thread"]
	if !ok || web.ParentID != "rp_answer" || web.Quote != "see step 1" || web.StartLine != 0 {
		t.Fatalf("fetched web sub-thread = %+v (local sub-threads %v)", web, subs)
	}

	// Re-shares keep every sub-thread on its anchor, the web one included,
	// now under the local id crit gave it.
	for range 2 {
		critShareCmd(t, binary, baseURL, dir, "plan.md")
		threads = subThreadsFromAPI(t, baseURL, token)
		answer = jsonList(threadByExternalID(t, threads, "c_open")["replies"])[0]
		got := map[string]any{}
		for _, st := range jsonList(answer["sub_threads"]) {
			got[st["body"].(string)] = st["external_id"]
		}
		if len(got) != 2 || got["which step?"] != "c_sub" || got["web sub-thread"] != web.ID {
			t.Errorf("sub-threads of the reply after re-share = %v", got)
		}
		if out, err := runCritCmd(t, binary, dir, "fetch", "--output", dir); err != nil {
			t.Fatalf("crit fetch failed: %v\n%s", err, out)
		}
		if n := len(localSubThreads(t, dir)); n != 3 {
			t.Errorf("local sub-threads = %d, want 3", n)
		}
	}
}

// TestShareSyncSubThreadsKeepTheShareWorking holds against any crit-web, one
// that predates sub-threads included (point CRIT_WEB_URL at it): a review
// with sub-threads shares and re-shares, and crit-web lists its threads only,
// the resolved one an open sub-thread hangs from among them.
func TestShareSyncSubThreadsKeepTheShareWorking(t *testing.T) {
	baseURL := critWebURL(t)
	binary := critBinary(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n\nStep 2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestCritJSON(t, dir, CritJSON{
		ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"plan.md": {Comments: []Comment{
				{ID: "c_open", StartLine: 3, EndLine: 3, Body: "expand this", Scope: "line",
					CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
					Replies: []Reply{{ID: "rp_answer", Body: "done", Author: "agent"}}},
				{ID: "c_sub", ParentID: "rp_answer", Body: "how?", Quote: "done", Scope: "line",
					CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				{ID: "c_done", StartLine: 5, EndLine: 5, Body: "resolved point", Scope: "line", Resolved: true,
					CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
				{ID: "c_still_open", ParentID: "c_done", Body: "not quite", Quote: "resolved", Scope: "line",
					CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"},
			}},
		},
	})

	output := critShareCmd(t, binary, baseURL, dir, "plan.md")
	logReview(t, output)
	token := extractToken(t, output)

	check := func() {
		t.Helper()
		comments := commentsFromAPI(t, baseURL, token)
		got := map[string]webComment{}
		for _, c := range comments {
			got[c.ExternalID] = c
		}
		if len(comments) != 2 || got["c_open"].StartLine != 3 || got["c_done"].StartLine != 5 {
			t.Fatalf("web comments = %+v, want the 2 threads on their lines", comments)
		}
		if !got["c_done"].Resolved || got["c_open"].Resolved || len(got["c_open"].Replies) != 1 {
			t.Errorf("web threads = %+v", comments)
		}
	}
	check()

	if err := os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Plan\n\nStep 1\n\nStep 2\n\nStep 3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	critShareCmd(t, binary, baseURL, dir, "plan.md")
	check()
}

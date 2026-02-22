# Agent Auto-Notification Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Eliminate the manual copy-paste step when finishing a review. Both the main `crit` command and `crit go` gain a `--wait` flag that blocks until the reviewer clicks Finish, then prints the review prompt to stdout so the agent can act on it automatically.

**Architecture:** Add a `reviewDone chan ReviewResult` to the Server struct. New `GET /api/await-review` endpoint long-polls on this channel. `handleFinish` signals the channel after writing files. Both `crit <file> --wait` (first round) and `crit go --wait <port>` (subsequent rounds) connect to `/api/await-review` and print the prompt to stdout (status messages to stderr). Frontend detects `agent_notified: true` in the finish response and shows "Sent to agent" instead of "Paste to clipboard". Works universally with any agent (Claude Code, Cline, OpenCode, Cursor) since the agent just runs a shell command and reads stdout.

**Tech Stack:** Go stdlib only — `net/http`, `sync`, `context`, channels. No new dependencies.

**User experience:**

First round (agent starts crit with --wait):
```
1. Agent runs: crit plan.md --wait
   → starts server, opens browser, blocks until Finish clicked
2. User reviews in browser, clicks Finish
3. crit prints prompt to stdout
4. Agent reads stdout, acts on changes
```

Subsequent rounds (fully automatic):
```
5. Agent runs `crit go --wait <port>`
   → signals round-complete, then blocks waiting for review
6. User reviews round 2 in crit, clicks Finish
7. `crit go --wait` prints prompt to stdout
8. Agent reads it and continues working
9. Repeat from 5
```

Fallback (user starts crit without --wait):
```
1. User opens crit: `crit plan.md`
2. User reviews, clicks Finish
3. Prompt copied to clipboard, user pastes to agent (one-time)
4. Agent works, then runs `crit go --wait <port>` — automatic from here on
```

---

### Task 1: Add ReviewResult type and reviewDone channel to Server

**Files:**
- Modify: `crit/server.go`

**Step 1: Write the failing test**

Add to `crit/server_test.go`:

```go
func TestAwaitReview_ReturnsPromptWhenFinished(t *testing.T) {
	s, doc := newTestServer(t)
	doc.AddComment(1, 1, "fix this")

	// Start await-review in background
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest("GET", "/api/await-review", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		done <- w
	}()

	// Give goroutine time to connect
	time.Sleep(50 * time.Millisecond)

	// Finish the review
	finishReq := httptest.NewRequest("POST", "/api/finish", nil)
	finishW := httptest.NewRecorder()
	s.ServeHTTP(finishW, finishReq)

	// Check finish response has agent_notified
	var finishResp map[string]interface{}
	json.Unmarshal(finishW.Body.Bytes(), &finishResp)
	if finishResp["agent_notified"] != true {
		t.Errorf("expected agent_notified=true, got %v", finishResp["agent_notified"])
	}

	// Check await-review returns the prompt
	w := <-done
	if w.Code != 200 {
		t.Fatalf("await-review status = %d", w.Code)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["prompt"] == "" {
		t.Error("expected non-empty prompt from await-review")
	}
	if resp["review_file"] == "" {
		t.Error("expected non-empty review_file from await-review")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd crit && go test -run TestAwaitReview_ReturnsPromptWhenFinished -v`
Expected: FAIL — no `/api/await-review` route, no `agent_notified` field

**Step 3: Implement ReviewResult and channel on Server**

In `crit/server.go`, add the type and modify the Server struct:

```go
// ReviewResult is sent from handleFinish to awaiting agents.
type ReviewResult struct {
	Prompt     string `json:"prompt"`
	ReviewFile string `json:"review_file"`
}
```

Add to the `Server` struct:

```go
type Server struct {
	doc        *Document
	mux        *http.ServeMux
	port       int
	shareURL   string
	status     *Status
	reviewDone chan ReviewResult // signals await-review when finish is clicked
}
```

Initialize in `NewServer`:

```go
s := &Server{
	doc:        doc,
	mux:        http.NewServeMux(),
	port:       port,
	shareURL:   shareURL,
	reviewDone: make(chan ReviewResult, 1),
}
```

Register the new route:

```go
s.mux.HandleFunc("/api/await-review", s.handleAwaitReview)
```

**Step 4: Implement handleAwaitReview**

```go
func (s *Server) handleAwaitReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	select {
	case result := <-s.reviewDone:
		writeJSON(w, result)
	case <-r.Context().Done():
		// Client disconnected
		http.Error(w, "Client disconnected", http.StatusRequestTimeout)
	}
}
```

**Step 5: Modify handleFinish to signal the channel**

In `handleFinish`, after generating the prompt, try to send on the channel:

```go
reviewFile := s.doc.reviewFilePath()
comments := s.doc.GetComments()
prompt := ""
if len(comments) > 0 {
	prompt = fmt.Sprintf(
		"Address review comments in %s. "+
			"Mark resolved in %s (set \"resolved\": true, optionally \"resolution_note\" and \"resolution_lines\"). "+
			"When done run: `crit go --wait %d`",
		reviewFile, s.doc.commentsFilePath(), s.port)
}

// Notify waiting agent (non-blocking)
agentNotified := false
select {
case s.reviewDone <- ReviewResult{Prompt: prompt, ReviewFile: reviewFile}:
	agentNotified = true
default:
}

writeJSON(w, map[string]interface{}{
	"status":         "finished",
	"review_file":    reviewFile,
	"prompt":         prompt,
	"agent_notified": agentNotified,
})
```

**Step 6: Run test to verify it passes**

Run: `cd crit && go test -run TestAwaitReview_ReturnsPromptWhenFinished -v`
Expected: PASS

**Step 7: Commit**

```bash
git add crit/server.go crit/server_test.go
git commit -m "feat: add await-review endpoint for agent notification"
```

---

### Task 2: Add more server tests for edge cases

**Files:**
- Modify: `crit/server_test.go`

**Step 1: Write edge case tests**

```go
func TestAwaitReview_NoComments(t *testing.T) {
	s, _ := newTestServer(t)

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest("GET", "/api/await-review", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		done <- w
	}()

	time.Sleep(50 * time.Millisecond)

	finishReq := httptest.NewRequest("POST", "/api/finish", nil)
	finishW := httptest.NewRecorder()
	s.ServeHTTP(finishW, finishReq)

	var finishResp map[string]interface{}
	json.Unmarshal(finishW.Body.Bytes(), &finishResp)
	if finishResp["agent_notified"] != true {
		t.Errorf("expected agent_notified=true even with no comments")
	}

	w := <-done
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["prompt"] != "" {
		t.Errorf("expected empty prompt with no comments, got %q", resp["prompt"])
	}
}

func TestFinish_NoAgentWaiting(t *testing.T) {
	s, doc := newTestServer(t)
	doc.AddComment(1, 1, "fix")

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["agent_notified"] != false {
		t.Errorf("expected agent_notified=false when no agent waiting")
	}
}

func TestAwaitReview_ClientDisconnect(t *testing.T) {
	s, _ := newTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/api/await-review", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		s.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel() // simulate disconnect

	select {
	case <-done:
		// Handler returned after cancel — good
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after context cancel")
	}
}

func TestAwaitReview_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/await-review", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("expected 405, got %d", w.Code)
	}
}
```

**Step 2: Run all tests**

Run: `cd crit && go test -run TestAwaitReview -v && go test -run TestFinish_NoAgentWaiting -v`
Expected: ALL PASS

**Step 3: Commit**

```bash
git add crit/server_test.go
git commit -m "test: add edge case tests for await-review and agent notification"
```

---

### Task 3: Add agent_waiting to config endpoint

**Files:**
- Modify: `crit/server.go`
- Modify: `crit/server_test.go`

**Step 1: Write the failing test**

```go
func TestConfig_ShowsAgentWaiting(t *testing.T) {
	s, doc := newTestServer(t)
	doc.AddComment(1, 1, "fix")

	// Before agent connects: agent_waiting should be false
	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["agent_waiting"] != false {
		t.Errorf("expected agent_waiting=false initially")
	}

	// Start await-review (agent connects)
	go func() {
		awaitReq := httptest.NewRequest("GET", "/api/await-review", nil)
		awaitW := httptest.NewRecorder()
		s.ServeHTTP(awaitW, awaitReq)
	}()

	time.Sleep(50 * time.Millisecond)

	// After agent connects: agent_waiting should be true
	req2 := httptest.NewRequest("GET", "/api/config", nil)
	w2 := httptest.NewRecorder()
	s.ServeHTTP(w2, req2)
	var resp2 map[string]interface{}
	json.Unmarshal(w2.Body.Bytes(), &resp2)
	if resp2["agent_waiting"] != true {
		t.Errorf("expected agent_waiting=true after agent connects")
	}

	// Finish to unblock the awaiting goroutine
	finishReq := httptest.NewRequest("POST", "/api/finish", nil)
	s.ServeHTTP(httptest.NewRecorder(), finishReq)
}
```

**Step 2: Run test to verify it fails**

Run: `cd crit && go test -run TestConfig_ShowsAgentWaiting -v`
Expected: FAIL — no `agent_waiting` field in config

**Step 3: Add agentWaiting tracking**

Add to Server struct:

```go
type Server struct {
	doc          *Document
	mux          *http.ServeMux
	port         int
	shareURL     string
	status       *Status
	reviewDone   chan ReviewResult
	agentWaiting atomic.Bool
}
```

Import `sync/atomic` at the top.

In `handleAwaitReview`, set the flag:

```go
func (s *Server) handleAwaitReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.agentWaiting.Store(true)
	defer s.agentWaiting.Store(false)

	select {
	case result := <-s.reviewDone:
		writeJSON(w, result)
	case <-r.Context().Done():
		http.Error(w, "Client disconnected", http.StatusRequestTimeout)
	}
}
```

In `handleConfig`, add the field. Find the existing `handleConfig` and add `"agent_waiting"`:

```go
writeJSON(w, map[string]interface{}{
	"share_url":     s.shareURL,
	"hosted_url":    hostedURL,
	"delete_token":  deleteToken,
	"agent_waiting": s.agentWaiting.Load(),
})
```

**Step 4: Run test to verify it passes**

Run: `cd crit && go test -run TestConfig_ShowsAgentWaiting -v`
Expected: PASS

**Step 5: Run all existing tests to check for regressions**

Run: `cd crit && go test ./... -v`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add crit/server.go crit/server_test.go
git commit -m "feat: track agent_waiting state and expose in config"
```

---

### Task 4: Add `--wait` flag to both `crit` and `crit go`

**Files:**
- Modify: `crit/main.go`

The `--wait` flag works in two places:
- **`crit <file> --wait`** (first round): after starting the server, connects to `GET /api/await-review` on the local server. When Finish is clicked, prints prompt to stdout. Server keeps running.
- **`crit go --wait <port>`** (subsequent rounds): POST round-complete, then GET await-review, print prompt to stdout.

**Step 1: Write the failing test**

This is a CLI integration, so we'll test the HTTP client logic by extracting it into a testable function. Add to a new test file `crit/go_cmd_test.go`:

```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGoWait_ReceivesPrompt(t *testing.T) {
	prompt := "Address review comments in plan.review.md."
	reviewFile := "plan.review.md"

	// Mock server that handles round-complete then await-review
	roundCompleteCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/round-complete":
			roundCompleteCalled = true
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case "/api/await-review":
			json.NewEncoder(w).Encode(ReviewResult{Prompt: prompt, ReviewFile: reviewFile})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	result, err := doGoWait(srv.URL)
	if err != nil {
		t.Fatalf("doGoWait error: %v", err)
	}
	if !roundCompleteCalled {
		t.Error("expected round-complete to be called")
	}
	if result.Prompt != prompt {
		t.Errorf("prompt = %q, want %q", result.Prompt, prompt)
	}
	if result.ReviewFile != reviewFile {
		t.Errorf("review_file = %q, want %q", result.ReviewFile, reviewFile)
	}
}

func TestGoWait_NoComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/round-complete":
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		case "/api/await-review":
			json.NewEncoder(w).Encode(ReviewResult{Prompt: "", ReviewFile: "plan.review.md"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	result, err := doGoWait(srv.URL)
	if err != nil {
		t.Fatalf("doGoWait error: %v", err)
	}
	if result.Prompt != "" {
		t.Errorf("expected empty prompt, got %q", result.Prompt)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd crit && go test -run TestGoWait -v`
Expected: FAIL — `doGoWait` function does not exist

**Step 3: Implement doGoWait and wire it into main**

Add `doGoWait` function (can go in `main.go` or a new `go_cmd.go`):

```go
// doGoWait signals round-complete and waits for the review to finish.
// Returns the review result with the prompt for the agent.
func doGoWait(baseURL string) (ReviewResult, error) {
	// Signal round complete
	resp, err := http.Post(baseURL+"/api/round-complete", "application/json", nil)
	if err != nil {
		return ReviewResult{}, fmt.Errorf("could not reach crit: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return ReviewResult{}, fmt.Errorf("round-complete returned status %d", resp.StatusCode)
	}
	fmt.Fprintln(os.Stderr, "Round complete — waiting for review…")

	// Wait for review to finish
	resp, err = http.Get(baseURL + "/api/await-review")
	if err != nil {
		return ReviewResult{}, fmt.Errorf("error waiting for review: %w", err)
	}
	defer resp.Body.Close()

	var result ReviewResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ReviewResult{}, fmt.Errorf("error reading review result: %w", err)
	}
	return result, nil
}
```

Modify the `crit go` subcommand handling in `main.go`:

```go
if len(os.Args) >= 2 && os.Args[1] == "go" {
	goFlags := flag.NewFlagSet("go", flag.ExitOnError)
	wait := goFlags.Bool("wait", false, "Wait for review to finish and print prompt")
	goFlags.BoolVar(wait, "w", false, "Wait for review to finish and print prompt")
	goFlags.Parse(os.Args[2:])

	port := "3000"
	if goFlags.NArg() > 0 {
		port = goFlags.Arg(0)
	}
	baseURL := "http://localhost:" + port

	if *wait {
		result, err := doGoWait(baseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if result.Prompt != "" {
			fmt.Println(result.Prompt)
		}
		os.Exit(0)
	}

	// Original non-wait behavior
	resp, err := http.Post(baseURL+"/api/round-complete", "application/json", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not reach crit on port %s: %v\n", port, err)
		os.Exit(1)
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		fmt.Println("Round complete — crit will reload.")
	} else {
		fmt.Fprintf(os.Stderr, "Unexpected status: %d\n", resp.StatusCode)
		os.Exit(1)
	}
	os.Exit(0)
}
```

Also add `--wait`/`-w` flag to the main `crit <file>` command. When set, after starting the server, connect to `GET /api/await-review` on the local server in a goroutine. When the review is done, print the prompt to stdout. The server keeps running so subsequent `crit go --wait` calls work.

```go
// In main(), after server starts and browser opens:
if *waitFlag {
	go func() {
		// Wait for the server to be ready, then long-poll for review completion
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/await-review", port))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error waiting for review: %v\n", err)
			return
		}
		defer resp.Body.Close()
		var result ReviewResult
		json.NewDecoder(resp.Body).Decode(&result)
		if result.Prompt != "" {
			fmt.Println(result.Prompt)
		}
	}()
}
```

**Step 4: Run test to verify it passes**

Run: `cd crit && go test -run TestGoWait -v`
Expected: PASS

**Step 5: Run all tests**

Run: `cd crit && go test ./... -v`
Expected: ALL PASS

**Step 6: Commit**

```bash
git add crit/main.go crit/go_cmd_test.go
git commit -m "feat: add --wait flag to crit go for automatic agent notification"
```

---

### Task 5: Update finish prompt to include `--wait` flag

**Files:**
- Modify: `crit/server.go`
- Modify: `crit/server_test.go`

**Step 1: Write the failing test**

```go
func TestFinish_PromptIncludesWaitFlag(t *testing.T) {
	s, doc := newTestServer(t)
	doc.AddComment(1, 1, "fix this")

	req := httptest.NewRequest("POST", "/api/finish", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.Contains(resp["prompt"], "--wait") {
		t.Errorf("prompt should include --wait flag, got: %s", resp["prompt"])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd crit && go test -run TestFinish_PromptIncludesWaitFlag -v`
Expected: FAIL — prompt says `crit go %d` without `--wait`

**Step 3: Update the prompt template**

In `handleFinish`, change the prompt format:

```go
prompt = fmt.Sprintf(
	"Address review comments in %s. "+
		"Mark resolved in %s (set \"resolved\": true, optionally \"resolution_note\" and \"resolution_lines\"). "+
		"When done run: `crit go --wait %d`",
	reviewFile, s.doc.commentsFilePath(), s.port)
```

**Step 4: Run test to verify it passes**

Run: `cd crit && go test -run TestFinish_PromptIncludesWaitFlag -v`
Expected: PASS

**Step 5: Run all tests (some existing tests may need prompt assertion updates)**

Run: `cd crit && go test ./... -v`

If any existing tests check the exact prompt string and fail, update them to expect `--wait` in the prompt.

**Step 6: Commit**

```bash
git add crit/server.go crit/server_test.go
git commit -m "feat: include --wait flag in finish prompt for automatic review loop"
```

---

### Task 6: Update frontend to show "Sent to agent" when agent is notified

**Files:**
- Modify: `crit/frontend/index.html`

**Step 1: No automated test for this (frontend is vanilla JS with no test framework). Manual verification.**

**Step 2: Update the finish button click handler**

Find the existing finish handler (around line 2954-2983) and update to handle `agent_notified`:

```javascript
document.getElementById('finishBtn').addEventListener('click', async function() {
  if (uiState !== 'reviewing') return;

  try {
    const resp = await fetch('/api/finish', { method: 'POST' });
    const data = await resp.json();
    const hasComments = !!data.prompt;
    const prompt = data.prompt || 'I reviewed the plan, no feedback, good to go!';

    document.getElementById('waitingPrompt').textContent = prompt;

    if (data.agent_notified) {
      // Agent is waiting — feedback was sent directly
      document.getElementById('waitingMessage').innerHTML =
        'Review feedback sent to your agent.';
      document.getElementById('waitingClipboard').textContent = '';
    } else if (hasComments) {
      document.getElementById('waitingMessage').innerHTML =
        'Paste the prompt below to your agent, then wait for <strong>' + escapeHtml(fileName) + '</strong> to be updated.';
      const clipEl = document.getElementById('waitingClipboard');
      clipEl.textContent = '\u2713 Copied to clipboard';
      clipEl.classList.remove('clipboard-confirm');
      void clipEl.offsetWidth;
      clipEl.classList.add('clipboard-confirm');
    } else {
      document.getElementById('waitingMessage').textContent =
        'You can close this browser tab, or leave it open for another round.';
      document.getElementById('waitingClipboard').textContent = '';
    }

    if (!data.agent_notified) {
      try { await navigator.clipboard.writeText(prompt); } catch (_) {}
    }
  } catch (_) {}

  setUIState('waiting');
});
```

Key changes:
- Check `data.agent_notified` first — if true, show "Review feedback sent to your agent."
- Don't copy to clipboard if agent was notified (no need)
- Still show the prompt text in `waitingPrompt` for reference
- Fall through to existing behavior if no agent waiting

**Step 3: Optionally show agent connection indicator**

Poll `/api/config` periodically (or on finish) to check `agent_waiting`. When true, the Finish button could show a subtle indicator. This is optional polish and can be added later.

**Step 4: Manual test**

1. Run `crit test-plan.md --port 3001`
2. Add a comment in the browser
3. In another terminal: `./crit go --wait 3001`
4. Click "Finish Review" in the browser
5. Verify: browser shows "Review feedback sent to your agent."
6. Verify: terminal shows the prompt on stdout

**Step 5: Commit**

```bash
git add crit/frontend/index.html
git commit -m "feat: show 'sent to agent' in UI when agent is auto-notified"
```

---

### Task 7: Update crit-web document-renderer.js for parity (if applicable)

**Files:**
- Check: `crit-web/assets/js/document-renderer.js`

**Step 1: Check if the finish flow exists in crit-web**

The crit-web review page is read-only (viewing shared reviews). There is no "Finish" button or agent notification flow in crit-web. **This task is likely a no-op.** Verify by checking the crit-web templates.

**Step 2: If no finish flow in crit-web, skip this task**

The frontend parity rule only applies to the review rendering surface (markdown rendering, comments display, theme system). The agent notification feature is crit-local only.

**Step 3: Commit (only if changes were needed)**

---

### Task 8: Update CLAUDE.md documentation

**Files:**
- Modify: `crit/CLAUDE.md`

**Step 1: Add agent notification to the API Endpoints section**

Add under existing endpoints:

```markdown
- `GET  /api/await-review` — long-polls until review is finished, returns `{prompt, review_file}` (used by `crit go --wait`)
```

**Step 2: Add to the CLI section**

```markdown
./crit go --wait 3000                             # Signal round complete + wait for review, print prompt to stdout
```

**Step 3: Add agent notification section**

After the "Multi-Round Review" section, add:

```markdown
## Agent Auto-Notification

When `crit go --wait <port>` is used instead of `crit go <port>`, the CLI blocks after signaling round-complete and waits for the reviewer to click Finish. The review prompt is then printed to stdout, allowing the agent to read it directly without manual copy-paste.

- Status messages go to stderr, prompt goes to stdout
- If there are no comments, stdout is empty (agent should continue normally)
- The frontend shows "Review feedback sent to your agent" instead of "Paste to clipboard" when an agent is waiting
- `GET /api/config` includes `agent_waiting: true/false` to indicate whether an agent is connected
- Works with any agent that can run shell commands: Claude Code, Cline, OpenCode, etc.
```

**Step 4: Commit**

```bash
git add crit/CLAUDE.md
git commit -m "docs: add agent auto-notification to CLAUDE.md"
```

---

### Task 9: Update integration commands

**Files:**
- Modify: `crit/integrations/claude-code/crit.md`
- Modify: `crit/integrations/claude-code/CLAUDE.md`
- Modify: `crit/integrations/cursor/crit-command.md`
- Modify: `crit/integrations/cursor/crit.mdc`
- Modify: `crit/integrations/cline/crit.md`
- Modify: `crit/integrations/windsurf/crit.md`
- Modify: `crit/integrations/github-copilot/crit.prompt.md`
- Modify: `crit/integrations/github-copilot/copilot-instructions.md`
- Modify: `crit/integrations/aider/CONVENTIONS.md`

**Step 1: Update all integration files**

- Change `crit <file>` → `crit <file> --wait` where the agent starts crit (so first round is automatic)
- Change `crit go <port>` → `crit go --wait <port>` in all files (so subsequent rounds are automatic)
- Update instructions to explain the agent reads stdout instead of waiting for user to say "go"

**Step 2: Commit**

```bash
git add crit/integrations/
git commit -m "feat: update integrations to use --wait for automatic review loop"
```

---

### Task 10: End-to-end manual test

**No code changes. Manual verification of the full flow.**

**Step 1: Build**

```bash
cd crit && go build -o crit .
```

**Step 2: Start crit**

```bash
./crit test-plan.md --port 3001
```

**Step 3: Round 1 (manual paste — existing behavior)**

1. Open browser, add a comment
2. Click Finish
3. Verify prompt is copied to clipboard
4. Verify `agent_notified: false` (no agent waiting)

**Step 4: Simulate agent work**

```bash
# Pretend to be an agent: make an edit to test-plan.md
echo "# edited" >> test-plan.md
```

**Step 5: Round 2 (auto-notification — new behavior)**

```bash
# In a separate terminal:
./crit go --wait 3001
# Should print "Round complete — waiting for review…" to stderr
```

1. Browser should transition to round 2
2. Add a comment in the browser
3. Click Finish
4. Verify browser shows "Review feedback sent to your agent."
5. Verify terminal prints the prompt to stdout
6. Verify `crit go --wait` exits with code 0

**Step 6: Round 2 with no comments**

Repeat step 5 but don't add any comments before clicking Finish.
- Browser should show "You can close this browser tab..."
- Terminal should exit with no stdout output

---

### Task 11: Add `crit wait <port>` subcommand

> **Note:** This task was added after end-to-end testing revealed a fundamental limitation in the `crit <file> --wait` approach.

**Background — why `crit <file> --wait` doesn't work cleanly:**

When `crit <file> --wait` is used, the process starts the HTTP server AND launches a background goroutine that long-polls `/api/await-review`. When the reviewer clicks Finish, the goroutine prints the prompt to stdout — but the **server keeps running**. The process never exits on its own.

This is a problem for agents that use blocking shell execution (e.g. Claude Code's Bash tool, which waits for the subprocess to exit before returning). If the agent runs `crit plan.md --wait` as a foreground Bash call, it blocks indefinitely — the shell call never returns.

**Solution: `crit wait <port>`**

A new `crit wait <port>` subcommand that:
- Only does the long-poll (`GET /api/await-review`)
- Prints the prompt to stdout when review is done
- Exits cleanly with code 0

No server, no round-complete signal. The agent starts crit in the background separately, then uses `crit wait` to block until the first review is done.

**Updated UX for round 1:**

```
# Agent starts crit in background, then waits for first review:
crit plan.md --no-open --port 3001 &    # start server in background
crit wait 3001                          # block until Finish clicked, print prompt, exit
```

For Claude Code (which has run_in_background):
```
# Start crit with run_in_background: true, parse port from output
# Then run `crit wait <port>` as a regular blocking Bash call
```

Round 2+ is unchanged — `crit go --wait <port>` still handles those.

**Files to create/modify:**
- Modify: `crit/main.go` — add `crit wait` subcommand before main flag parsing
- Modify: `crit/go_cmd_test.go` — add `TestWait_ReceivesPrompt` test
- Modify: `crit/CLAUDE.md` — document `crit wait`
- Modify: `crit/integrations/claude-code/crit.md` — use run_in_background + crit wait
- Modify: `crit/integrations/claude-code/CLAUDE.md` — update round 1 flow
- Modify: `crit/integrations/cursor/crit-command.md` — use & + crit wait
- Modify: `crit/integrations/cursor/crit.mdc` — same
- Modify: `crit/integrations/github-copilot/crit.prompt.md` — use & + crit wait
- Modify: `crit/integrations/github-copilot/copilot-instructions.md` — same
- Modify: `crit/integrations/cline/crit.md` — use & + crit wait
- Modify: `crit/integrations/windsurf/crit.md` — use & + crit wait
- Modify: `crit/integrations/aider/CONVENTIONS.md` — use & + crit wait

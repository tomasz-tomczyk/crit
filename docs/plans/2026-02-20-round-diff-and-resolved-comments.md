# Round Diff View & Resolved Comments Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable multi-round review with diff visibility, agent round-completion signaling, and resolved comment tracking so the reviewer can see exactly what changed and which comments were addressed.

**Architecture:** Three connected features built incrementally: (1) The Go backend stores the previous round's content and comments in memory, adds a `POST /api/round-complete` endpoint for agents to signal they're done editing, and batches file-change SSE events into rounds. (2) The `.comments.json` format gains `resolved`, `resolution_note`, and `resolution_lines` fields that the agent writes directly. (3) The frontend adds a side-by-side diff toggle, shows an edit counter in the waiting modal, and renders resolved comments as collapsed cards mapped to new line positions.

**Tech Stack:** Go (backend), vanilla JS (frontend), no new dependencies. Diff computation uses a simple line-level longest common subsequence (LCS) algorithm implemented in Go (~50 lines) — the backend computes the diff and serves it via `GET /api/diff`, so the frontend just renders the result.

---

### Task 0: Create feature branch

**Step 1: Create and switch to feature branch**

```bash
git checkout -b feature/round-diff-resolved-comments
```

**Step 2: Verify**

```bash
git branch --show-current
```
Expected: `feature/round-diff-resolved-comments`

---

### Task 1: Store previous round content in Document

The Document struct needs to remember the content from the start of each round so we can compute a diff when the round ends. Only the immediately previous round is stored — `PreviousContent` and `PreviousComments` are overwritten each time `ReloadFile()` is called. So in round 3, you can diff against round 2 but not round 1. This keeps the design simple and memory-bounded.

**Files:**
- Modify: `document.go` — add `PreviousContent` and `PreviousComments` fields to Document struct
- Test: `document_test.go` — add tests for previous content preservation

**Step 1: Write the failing test**

In `document_test.go`, add:

```go
func TestReloadFile_PreservesPreviousContent(t *testing.T) {
	doc := newTestDoc(t, "original line 1\noriginal line 2")
	doc.AddComment(1, 1, "fix this")

	// Modify the file
	if err := os.WriteFile(doc.FilePath, []byte("modified line 1\nnew line 2\nnew line 3"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := doc.ReloadFile(); err != nil {
		t.Fatal(err)
	}

	if doc.PreviousContent != "original line 1\noriginal line 2" {
		t.Errorf("PreviousContent = %q, want original content", doc.PreviousContent)
	}
	if len(doc.PreviousComments) != 1 {
		t.Errorf("PreviousComments len = %d, want 1", len(doc.PreviousComments))
	}
	if doc.PreviousComments[0].Body != "fix this" {
		t.Errorf("PreviousComments[0].Body = %q, want 'fix this'", doc.PreviousComments[0].Body)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./... -run TestReloadFile_PreservesPreviousContent -v`
Expected: FAIL — `doc.PreviousContent` and `doc.PreviousComments` don't exist

**Step 3: Write minimal implementation**

In `document.go`, add fields to the Document struct:

```go
type Document struct {
	// ... existing fields ...
	PreviousContent  string    // content from the previous round (empty on first round)
	PreviousComments []Comment // comments from the previous round
}
```

In `ReloadFile()`, before clearing Content/Comments, save them:

```go
func (d *Document) ReloadFile() error {
	data, err := os.ReadFile(d.FilePath)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	d.mu.Lock()
	// Save previous round state before overwriting
	d.PreviousContent = d.Content
	d.PreviousComments = make([]Comment, len(d.Comments))
	copy(d.PreviousComments, d.Comments)

	d.Content = string(data)
	d.FileHash = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	d.Comments = []Comment{}
	d.nextID = 1
	d.staleNotice = ""
	d.mu.Unlock()

	os.Remove(d.commentsFilePath())
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./... -run TestReloadFile_PreservesPreviousContent -v`
Expected: PASS

**Step 5: Run all tests to verify no regressions**

Run: `go test ./... -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add document.go document_test.go
git commit -m "Store previous round content and comments on file reload"
```

---

### Task 2: Add round-complete API endpoint and `crit go` CLI subcommand

The agent calls `POST /api/round-complete` when it's done editing the file. This tells crit to treat the current file state as the new round. For a cleaner developer experience, we also add a `crit go <PORT>` subcommand that wraps this API call — so the finish prompt says `crit go 3000` instead of a raw `curl` command.

**Files:**
- Modify: `document.go` — add `pendingEdits` counter, `roundComplete` channel, `IncrementEdits()`, `WaitForRoundComplete()` methods
- Modify: `server.go` — add `handleRoundComplete` handler and route
- Modify: `main.go` — add `go` subcommand that POSTs to `http://localhost:<port>/api/round-complete`
- Test: `server_test.go` — add tests for the new endpoint

**Step 1: Write the failing test**

In `server_test.go`, add:

```go
func TestRoundComplete(t *testing.T) {
	s, _ := newTestServer(t)

	req := httptest.NewRequest("POST", "/api/round-complete", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want ok", resp["status"])
	}
}

func TestRoundComplete_MethodNotAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/round-complete", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./... -run TestRoundComplete -v`
Expected: FAIL — 404 (route doesn't exist)

**Step 3: Implement the endpoint**

In `document.go`, add fields and methods:

```go
// Add to Document struct:
	pendingEdits  int           // number of file changes detected since last round-complete
	roundComplete chan struct{} // signaled when agent calls round-complete
```

Initialize `roundComplete` in `NewDocument`:
```go
roundComplete: make(chan struct{}, 1),
```

Add methods:
```go
func (d *Document) IncrementEdits() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pendingEdits++
}

func (d *Document) GetPendingEdits() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.pendingEdits
}

func (d *Document) SignalRoundComplete() {
	d.mu.Lock()
	d.pendingEdits = 0
	d.mu.Unlock()
	select {
	case d.roundComplete <- struct{}{}:
	default:
	}
}

func (d *Document) RoundCompleteChan() <-chan struct{} {
	return d.roundComplete
}
```

In `server.go`, add the handler and route:

```go
// In NewServer, add route:
mux.HandleFunc("/api/round-complete", s.handleRoundComplete)

// Handler:
func (s *Server) handleRoundComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.doc.SignalRoundComplete()
	writeJSON(w, map[string]string{"status": "ok"})
}
```

Also add the route to `newTestServer` in `server_test.go`:
```go
mux.HandleFunc("/api/round-complete", s.handleRoundComplete)
```

**Step 4: Add `crit go` subcommand**

In `main.go`, before the existing flag parsing / server startup, check if the first argument is `go`:

```go
if len(os.Args) >= 2 && os.Args[1] == "go" {
	port := "3000" // default
	if len(os.Args) >= 3 {
		port = os.Args[2]
	}
	resp, err := http.Post("http://localhost:"+port+"/api/round-complete", "application/json", nil)
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

This is a simple early-exit path — no flag parsing needed. The agent runs `crit go 3000` (or just `crit go` for default port) and gets immediate feedback.

**Step 5: Run tests to verify they pass**

Run: `go test ./... -v`
Expected: All PASS

**Step 6: Build and manually test**

```bash
go build -o crit .
./crit --no-open --port 3000 test-plan.md &
./crit go 3000
# Expected: "Round complete — crit will reload."
```

**Step 7: Commit**

```bash
git add document.go server.go server_test.go main.go
git commit -m "Add POST /api/round-complete endpoint and crit go subcommand"
```

---

### Task 3: Batch file changes into rounds

Change `WatchFile` to count edits instead of immediately triggering a round. The new round is only triggered when the agent calls `POST /api/round-complete`. The SSE `file-changed` event now includes the edit count. The frontend waiting modal shows the edit count and only transitions to the new round on `round-complete`.

**Files:**
- Modify: `document.go` — change `WatchFile` to count edits and wait for `roundComplete` signal; add new SSE event type `round-ready`
- Modify: `document.go` — add `edits-detected` SSE event type for live edit counting
- Test: `document_test.go` — add test for edit counting

**Step 1: Write the failing test**

In `document_test.go`:

```go
func TestEditCounting(t *testing.T) {
	doc := newTestDoc(t, "original")
	doc.IncrementEdits()
	doc.IncrementEdits()
	if doc.GetPendingEdits() != 2 {
		t.Errorf("pending edits = %d, want 2", doc.GetPendingEdits())
	}
	doc.SignalRoundComplete()
	if doc.GetPendingEdits() != 0 {
		t.Errorf("pending edits after round-complete = %d, want 0", doc.GetPendingEdits())
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./... -run TestEditCounting -v`
Expected: FAIL — methods don't exist yet (they will after Task 2, so run this after Task 2)

**Step 3: Implement the WatchFile changes**

Replace the `WatchFile` function in `document.go`:

```go
func (d *Document) WatchFile(stop <-chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			data, err := os.ReadFile(d.FilePath)
			if err != nil {
				continue
			}
			hash := fmt.Sprintf("sha256:%x", sha256.Sum256(data))

			d.mu.RLock()
			changed := hash != d.FileHash
			d.mu.RUnlock()

			if changed {
				if err := d.ReloadFile(); err != nil {
					fmt.Fprintf(os.Stderr, "Error reloading file: %v\n", err)
					continue
				}
				d.IncrementEdits()

				// Notify frontend of edit detection (for counter in waiting modal)
				d.notify(SSEEvent{
					Type:     "edit-detected",
					Filename: d.FileName,
					Content:  fmt.Sprintf("%d", d.GetPendingEdits()),
				})
			}
		case <-d.roundComplete:
			// Agent signaled round complete — send the full file-changed event
			d.mu.RLock()
			event := SSEEvent{
				Type:     "file-changed",
				Filename: d.FileName,
				Content:  d.Content,
			}
			d.mu.RUnlock()

			d.notify(event)
		}
	}
}
```

**Step 4: Run all tests**

Run: `go test ./... -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add document.go document_test.go
git commit -m "Batch file changes into rounds with edit counting"
```

---

### Task 4: Add previous content and comments to /api/config

The frontend needs the previous round's content and comments to render the diff and resolved comments.

**Files:**
- Modify: `server.go` — add `GET /api/previous-round` endpoint
- Test: `server_test.go` — test the new endpoint

**Step 1: Write the failing test**

In `server_test.go`:

```go
func TestGetPreviousRound_Empty(t *testing.T) {
	s, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/previous-round", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["content"] != "" {
		t.Errorf("expected empty content for first round, got %q", resp["content"])
	}
}

func TestGetPreviousRound_AfterReload(t *testing.T) {
	s, doc := newTestServer(t)
	doc.AddComment(1, 1, "fix this")

	// Simulate file change
	os.WriteFile(doc.FilePath, []byte("modified content"), 0644)
	doc.ReloadFile()

	req := httptest.NewRequest("GET", "/api/previous-round", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp struct {
		Content  string    `json:"content"`
		Comments []Comment `json:"comments"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Content != "line1\nline2\nline3\n" {
		t.Errorf("previous content = %q", resp.Content)
	}
	if len(resp.Comments) != 1 || resp.Comments[0].Body != "fix this" {
		t.Errorf("previous comments = %+v", resp.Comments)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./... -run TestGetPreviousRound -v`
Expected: FAIL — 404

**Step 3: Implement the endpoint**

In `server.go`:

```go
// In NewServer, add route:
mux.HandleFunc("/api/previous-round", s.handlePreviousRound)

// Handler:
func (s *Server) handlePreviousRound(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.doc.mu.RLock()
	resp := map[string]interface{}{
		"content":  s.doc.PreviousContent,
		"comments": s.doc.PreviousComments,
	}
	s.doc.mu.RUnlock()
	writeJSON(w, resp)
}
```

Add route to `newTestServer` as well.

**Step 4: Run tests**

Run: `go test ./... -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add server.go server_test.go
git commit -m "Add GET /api/previous-round endpoint"
```

---

### Task 5: Extend .comments.json format for resolved comments

Add `resolved`, `resolution_note`, and `resolution_lines` fields to the Comment struct. The agent writes these directly into `.comments.json`. When the file changes and comments are reloaded, resolved comments are preserved.

**Why both `.review.md` and `.comments.json`?** The `.review.md` gives the LLM human-readable context — the original plan text with comments interleaved at the right locations, so the LLM understands *what* each comment refers to without needing to cross-reference line numbers. The `.comments.json` gives structured data that's easy to programmatically modify (set `resolved: true`, add `resolution_lines`). The finish prompt references both: read `.review.md` to understand the comments in context, modify `.comments.json` to mark them resolved.

**Files:**
- Modify: `document.go` — extend Comment struct, update `loadComments` to read resolved comments from previous round
- Test: `document_test.go` — test resolved comment loading

**Step 1: Write the failing test**

In `document_test.go`:

```go
func TestLoadComments_WithResolved(t *testing.T) {
	doc := newTestDoc(t, "line1\nline2")
	doc.AddComment(1, 1, "fix this")

	// Manually write a comments file with resolved fields
	cf := CommentsFile{
		File:     doc.FileName,
		FileHash: doc.FileHash,
		Comments: []Comment{
			{
				ID:              "c1",
				StartLine:       1,
				EndLine:         1,
				Body:            "fix this",
				Resolved:        true,
				ResolutionNote:  "Refactored the function",
				ResolutionLines: []int{3, 4, 5},
			},
		},
	}
	data, _ := json.MarshalIndent(cf, "", "  ")
	os.WriteFile(doc.commentsFilePath(), data, 0644)

	// Reload document
	doc2, err := NewDocument(doc.FilePath, doc.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	comments := doc2.GetComments()
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if !comments[0].Resolved {
		t.Error("expected comment to be resolved")
	}
	if comments[0].ResolutionNote != "Refactored the function" {
		t.Errorf("resolution note = %q", comments[0].ResolutionNote)
	}
	if len(comments[0].ResolutionLines) != 3 {
		t.Errorf("resolution lines = %v", comments[0].ResolutionLines)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./... -run TestLoadComments_WithResolved -v`
Expected: FAIL — `Resolved`, `ResolutionNote`, `ResolutionLines` fields don't exist

**Step 3: Implement**

In `document.go`, extend Comment struct:

```go
type Comment struct {
	ID              string `json:"id"`
	StartLine       int    `json:"start_line"`
	EndLine         int    `json:"end_line"`
	Body            string `json:"body"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	Resolved        bool   `json:"resolved,omitempty"`
	ResolutionNote  string `json:"resolution_note,omitempty"`
	ResolutionLines []int  `json:"resolution_lines,omitempty"`
}
```

No changes needed to `loadComments` — the JSON unmarshalling will pick up the new fields automatically.

**Step 4: Run tests**

Run: `go test ./... -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add document.go document_test.go
git commit -m "Add resolved comment fields to Comment struct"
```

---

### Task 6: Update output.go — filter resolved comments, add agent instructions footer

Two changes to `GenerateReviewMD`: (1) Skip comments where `Resolved == true` — they've already been addressed and don't belong in the review output. (2) Append an agent instructions footer explaining how to mark comments resolved and signal round-complete.

**Files:**
- Modify: `output.go` — filter resolved comments, add footer with JSON file reference and `crit go` instructions
- Modify: `document.go` — update `writeReviewMD` to pass JSON path
- Modify: `server.go` — update the finish prompt to mention `crit go` and resolved comments
- Test: `output_test.go` — test footer presence and resolved comment filtering

**Step 1: Write the failing tests**

In `output_test.go`:

```go
func TestGenerateReviewMD_IncludesJSONReference(t *testing.T) {
	content := "line one"
	comments := []Comment{
		{ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix this"},
	}
	result := GenerateReviewMD(content, comments, ".test.md.comments.json")

	if !strings.Contains(result, ".test.md.comments.json") {
		t.Error("review MD should reference the comments JSON file")
	}
	if !strings.Contains(result, "resolution_lines") {
		t.Error("review MD should explain the resolved fields")
	}
	if !strings.Contains(result, "crit go") {
		t.Error("review MD should mention crit go command")
	}
}

func TestGenerateReviewMD_SkipsResolvedComments(t *testing.T) {
	content := "line one\nline two"
	comments := []Comment{
		{ID: "c1", StartLine: 1, EndLine: 1, Body: "Fix this", Resolved: true},
		{ID: "c2", StartLine: 2, EndLine: 2, Body: "And this"},
	}
	result := GenerateReviewMD(content, comments, "")

	if strings.Contains(result, "Fix this") {
		t.Error("resolved comment should not appear in review MD")
	}
	if !strings.Contains(result, "And this") {
		t.Error("unresolved comment should appear in review MD")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./... -run TestGenerateReviewMD -v`
Expected: FAIL — `GenerateReviewMD` doesn't accept the extra parameter yet

**Step 3: Implement**

Update `GenerateReviewMD` signature to accept the JSON file path, filter resolved comments:

```go
func GenerateReviewMD(content string, comments []Comment, commentsJSONPath string) string {
```

At the start of the function, filter out resolved comments:

```go
	var activeComments []Comment
	for _, c := range comments {
		if !c.Resolved {
			activeComments = append(activeComments, c)
		}
	}
```

Use `activeComments` instead of `comments` in the existing content+comments loop.

After the main loop, append the agent instructions footer:

```go
	// Agent instructions footer
	result.WriteString("\n\n---\n\n")
	result.WriteString("## Agent Instructions\n\n")
	result.WriteString(fmt.Sprintf("After addressing the comments above, mark each as resolved in `%s` by setting `\"resolved\": true` on the comment object. ", commentsJSONPath))
	result.WriteString("You may also add `\"resolution_note\": \"description\"` and `\"resolution_lines\": [line numbers]` pointing to the new/changed lines in the updated file. ")
	result.WriteString("When all edits are complete, signal the reviewer by running:\n\n")
	result.WriteString("```bash\ncrit go $PORT\n```\n")

	return result.String()
```

Update `writeReviewMD` in `document.go` to pass the JSON path:

```go
func (d *Document) writeReviewMD(comments []Comment) {
	if len(comments) == 0 {
		os.Remove(d.reviewFilePath())
		return
	}
	reviewContent := GenerateReviewMD(d.Content, comments, d.commentsFilePath())
	if err := os.WriteFile(d.reviewFilePath(), []byte(reviewContent), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing review file: %v\n", err)
	}
}
```

Update all existing callers of `GenerateReviewMD` in tests to pass the new parameter (empty string is fine for older tests).

**Step 4: Run tests**

Run: `go test ./... -v`
Expected: All PASS (after updating existing test call sites)

**Step 5: Commit**

```bash
git add output.go output_test.go document.go
git commit -m "Filter resolved comments from review MD and add agent instructions footer"
```

---

### Task 7: Frontend — edit counter in waiting modal

When the user clicks "Finish Review", the waiting modal stays open and shows a live counter of file edits detected via the new `edit-detected` SSE event.

**Files:**
- Modify: `frontend/index.html` — update SSE handler for new `edit-detected` event, update waiting modal HTML to show counter, update `file-changed` handler to only trigger on `round-complete`

**Step 1: Add the edit counter element to the waiting modal**

In the HTML `#waitingOverlay` section, add an edit counter element after the spinner:

```html
<p class="waiting-edits" id="waitingEdits"></p>
```

Add CSS:
```css
.waiting-edits {
  font-size: 13px;
  color: var(--fg-muted);
  margin-top: 8px;
}
```

**Step 2: Add SSE handler for `edit-detected`**

In `connectSSE()`, add a new event listener:

```javascript
source.addEventListener('edit-detected', function(e) {
  try {
    const data = JSON.parse(e.data);
    const count = parseInt(data.content, 10);
    const el = document.getElementById('waitingEdits');
    if (el && uiState === 'waiting') {
      el.textContent = 'Your agent made ' + count + ' edit' + (count === 1 ? '' : 's');
    }
  } catch (err) {
    console.error('Error handling edit-detected:', err);
  }
});
```

**Step 3: Clear the edit counter when entering waiting state**

In `setUIState('waiting')`:
```javascript
document.getElementById('waitingEdits').textContent = '';
```

**Step 4: Clear the edit counter when returning to reviewing state**

In `setUIState('reviewing')`:
```javascript
document.getElementById('waitingEdits').textContent = '';
```

**Step 5: Build and manually test**

Run: `go build -o crit . && ./crit test-plan.md`

Verify: Click Finish Review, manually edit the file, confirm counter appears in modal.

**Step 6: Commit**

```bash
git add frontend/index.html
git commit -m "Show live edit counter in waiting modal"
```

---

### Task 8: Backend — line-level diff computation and API endpoint

Implement a line-level LCS diff algorithm in Go and expose it via `GET /api/diff`. The backend computes the diff between the previous round's content and the current content, returning a JSON array of diff entries. This keeps the frontend simple — it just renders what the server provides.

**Files:**
- Create: `diff.go` — `ComputeLineDiff(oldContent, newContent string) []DiffEntry` function
- Create: `diff_test.go` — tests for the diff algorithm
- Modify: `server.go` — add `GET /api/diff` endpoint
- Modify: `server_test.go` — test the diff endpoint

**Step 1: Write the failing test for the diff algorithm**

In `diff_test.go`:

```go
package main

import (
	"testing"
)

func TestComputeLineDiff_BasicChanges(t *testing.T) {
	old := "a\nb\nc"
	new := "a\nx\nc\nd"
	diff := ComputeLineDiff(old, new)

	expected := []DiffEntry{
		{Type: "unchanged", OldLine: 1, NewLine: 1, Text: "a"},
		{Type: "removed", OldLine: 2, Text: "b"},
		{Type: "added", NewLine: 2, Text: "x"},
		{Type: "unchanged", OldLine: 3, NewLine: 3, Text: "c"},
		{Type: "added", NewLine: 4, Text: "d"},
	}

	if len(diff) != len(expected) {
		t.Fatalf("diff len = %d, want %d\ndiff: %+v", len(diff), len(expected), diff)
	}
	for i, e := range expected {
		if diff[i].Type != e.Type || diff[i].Text != e.Text {
			t.Errorf("diff[%d] = %+v, want %+v", i, diff[i], e)
		}
	}
}

func TestComputeLineDiff_EmptyOld(t *testing.T) {
	diff := ComputeLineDiff("", "a\nb")
	if len(diff) != 2 {
		t.Fatalf("diff len = %d, want 2", len(diff))
	}
	if diff[0].Type != "added" || diff[1].Type != "added" {
		t.Errorf("expected all added, got %+v", diff)
	}
}

func TestComputeLineDiff_EmptyNew(t *testing.T) {
	diff := ComputeLineDiff("a\nb", "")
	if len(diff) != 2 {
		t.Fatalf("diff len = %d, want 2", len(diff))
	}
	if diff[0].Type != "removed" || diff[1].Type != "removed" {
		t.Errorf("expected all removed, got %+v", diff)
	}
}

func TestComputeLineDiff_Identical(t *testing.T) {
	diff := ComputeLineDiff("a\nb\nc", "a\nb\nc")
	for _, e := range diff {
		if e.Type != "unchanged" {
			t.Errorf("expected all unchanged, got %+v", diff)
			break
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./... -run TestComputeLineDiff -v`
Expected: FAIL — `ComputeLineDiff` and `DiffEntry` don't exist

**Step 3: Implement the diff algorithm**

In `diff.go`:

```go
package main

import "strings"

type DiffEntry struct {
	Type    string `json:"type"`
	OldLine int    `json:"old_line,omitempty"`
	NewLine int    `json:"new_line,omitempty"`
	Text    string `json:"text"`
}

func ComputeLineDiff(oldContent, newContent string) []DiffEntry {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	// Handle empty content edge cases
	if oldContent == "" {
		oldLines = []string{}
	}
	if newContent == "" {
		newLines = []string{}
	}

	m, n := len(oldLines), len(newLines)

	// Build LCS table
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if oldLines[i-1] == newLines[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to build diff
	var result []DiffEntry
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			result = append([]DiffEntry{{Type: "unchanged", OldLine: i, NewLine: j, Text: newLines[j-1]}}, result...)
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			result = append([]DiffEntry{{Type: "added", NewLine: j, Text: newLines[j-1]}}, result...)
			j--
		} else {
			result = append([]DiffEntry{{Type: "removed", OldLine: i, Text: oldLines[i-1]}}, result...)
			i--
		}
	}
	return result
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./... -run TestComputeLineDiff -v`
Expected: All PASS

**Step 5: Write the failing test for the API endpoint**

In `server_test.go`:

```go
func TestGetDiff_NoPreviousRound(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/diff", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var resp struct {
		Entries []DiffEntry `json:"entries"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Entries) != 0 {
		t.Errorf("expected empty diff entries for first round, got %d", len(resp.Entries))
	}
}

func TestGetDiff_AfterReload(t *testing.T) {
	s, doc := newTestServer(t)

	os.WriteFile(doc.FilePath, []byte("modified line 1\nnew line"), 0644)
	doc.ReloadFile()

	req := httptest.NewRequest("GET", "/api/diff", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)

	var resp struct {
		Entries []DiffEntry `json:"entries"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Entries) == 0 {
		t.Error("expected non-empty diff entries after reload")
	}
}
```

**Step 6: Implement the endpoint**

In `server.go`, add the handler and route:

```go
// In NewServer, add route:
mux.HandleFunc("/api/diff", s.handleDiff)

// Handler:
func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.doc.mu.RLock()
	prev := s.doc.PreviousContent
	curr := s.doc.Content
	s.doc.mu.RUnlock()

	var entries []DiffEntry
	if prev != "" {
		entries = ComputeLineDiff(prev, curr)
	}
	if entries == nil {
		entries = []DiffEntry{}
	}
	writeJSON(w, map[string]interface{}{
		"entries": entries,
	})
}
```

Add route to `newTestServer` as well.

**Step 7: Run all tests**

Run: `go test ./... -v`
Expected: All PASS

**Step 8: Commit**

```bash
git add diff.go diff_test.go server.go server_test.go
git commit -m "Add line-level LCS diff algorithm and GET /api/diff endpoint"
```

---

### Task 9: Frontend — side-by-side diff panel

Add a toggle button in the header and a side-by-side diff view panel that shows changes between the previous round and current round.

**Files:**
- Modify: `frontend/index.html` — add diff toggle button, diff panel HTML, CSS, and rendering logic

**Step 1: Add the diff toggle button to the header**

In `.header-right`, before the theme pill, add:
```html
<button class="btn btn-sm" id="diffToggle" style="display:none">Diff</button>
```

The button starts hidden and appears only when there is a previous round to diff against.

**Step 2: Add the diff panel HTML**

After `<div id="shareNotice"></div>`, add:
```html
<div id="diffPanel" class="diff-panel" style="display:none">
  <div class="diff-header">
    <span>Changes from previous round</span>
    <button class="btn btn-sm" id="diffClose">Close</button>
  </div>
  <div class="diff-content" id="diffContent"></div>
</div>
```

**Step 3: Add diff panel CSS**

```css
.diff-panel {
  max-width: 1200px;
  margin: 16px auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
  background: var(--bg-primary);
}
.diff-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 16px;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border);
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-secondary);
}
.diff-content {
  display: grid;
  grid-template-columns: 1fr 1fr;
  font-family: var(--font-mono);
  font-size: 13px;
  line-height: 1.5;
  overflow-x: auto;
}
.diff-line {
  padding: 1px 12px;
  white-space: pre-wrap;
  word-break: break-all;
}
.diff-line-num {
  display: inline-block;
  width: 3em;
  text-align: right;
  margin-right: 8px;
  color: var(--fg-dimmed);
  user-select: none;
}
.diff-line.added {
  background: rgba(158, 206, 106, 0.1);
  color: var(--green);
}
.diff-line.removed {
  background: rgba(247, 118, 142, 0.1);
  color: var(--red);
  text-decoration: line-through;
}
.diff-line.unchanged {
  color: var(--fg-muted);
}
.diff-side {
  border-right: 1px solid var(--border);
  overflow-x: auto;
}
.diff-side:last-child {
  border-right: none;
}
.diff-side-header {
  padding: 6px 12px;
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-muted);
  background: var(--bg-tertiary);
  border-bottom: 1px solid var(--border);
}
```

**Step 4: Add the rendering logic**

The diff is fetched from the backend (`GET /api/diff`) which returns pre-computed diff entries. The frontend just renders them.

```javascript
let previousComments = [];
let diffEntries = []; // cached from /api/diff

function renderDiffPanel() {
  if (!diffEntries.length) return;

  const leftHtml = ['<div class="diff-side"><div class="diff-side-header">Previous</div>'];
  const rightHtml = ['<div class="diff-side"><div class="diff-side-header">Current</div>'];

  for (const entry of diffEntries) {
    const escaped = escapeHtml(entry.text);
    switch (entry.type) {
      case 'removed':
        leftHtml.push(`<div class="diff-line removed"><span class="diff-line-num">${entry.old_line}</span>${escaped}</div>`);
        rightHtml.push(`<div class="diff-line">&nbsp;</div>`);
        break;
      case 'added':
        leftHtml.push(`<div class="diff-line">&nbsp;</div>`);
        rightHtml.push(`<div class="diff-line added"><span class="diff-line-num">${entry.new_line}</span>${escaped}</div>`);
        break;
      case 'unchanged':
        leftHtml.push(`<div class="diff-line unchanged"><span class="diff-line-num">${entry.old_line}</span>${escaped}</div>`);
        rightHtml.push(`<div class="diff-line unchanged"><span class="diff-line-num">${entry.new_line}</span>${escaped}</div>`);
        break;
    }
  }

  leftHtml.push('</div>');
  rightHtml.push('</div>');

  document.getElementById('diffContent').innerHTML = leftHtml.join('') + rightHtml.join('');
}
```

**Step 5: Wire up the toggle button and close button**

```javascript
document.getElementById('diffToggle').addEventListener('click', function() {
  const panel = document.getElementById('diffPanel');
  if (panel.style.display === 'none') {
    renderDiffPanel();
    panel.style.display = '';
  } else {
    panel.style.display = 'none';
  }
});

document.getElementById('diffClose').addEventListener('click', function() {
  document.getElementById('diffPanel').style.display = 'none';
});
```

**Step 6: Fetch diff and previous round data on file-changed**

In the `file-changed` SSE handler, after updating content, fetch the diff from the backend and previous round comments:

```javascript
// After parseAndRender in file-changed handler:
try {
  const [diffResp, prevResp] = await Promise.all([
    fetch('/api/diff').then(r => r.json()),
    fetch('/api/previous-round').then(r => r.json()),
  ]);
  diffEntries = diffResp.entries || [];
  previousComments = prevResp.comments || [];
  if (diffEntries.length) {
    document.getElementById('diffToggle').style.display = '';
  }
} catch (_) {}
```

Note: The `file-changed` handler needs to become `async` for this `await` to work.

**Step 7: Hide diff panel on mobile**

In the mobile CSS:
```css
#diffToggle, #diffPanel {
  display: none !important;
}
```

**Step 8: Build and manually test**

Run: `go build -o crit . && ./crit test-plan.md`

**Step 9: Commit**

```bash
git add frontend/index.html
git commit -m "Add side-by-side diff panel with toggle"
```

---

### Task 10: Frontend — resolved comments rendering

Display resolved comments from the previous round as collapsed cards at their mapped positions (using `resolution_lines`).

**Files:**
- Modify: `frontend/index.html` — add resolved comment rendering in `renderDocument()`

**Step 1: Add CSS for resolved comments**

```css
.resolved-comment {
  max-width: 840px;
  margin: 4px auto 4px 48px;
  padding: 6px 12px;
  background: var(--bg-tertiary);
  border-left: 3px solid var(--green);
  border-radius: 4px;
  font-size: 12px;
  color: var(--fg-muted);
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 8px;
}
.resolved-comment:hover {
  background: var(--bg-hover);
}
.resolved-comment .resolved-check {
  color: var(--green);
  font-weight: 600;
}
.resolved-comment .resolved-body {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}
.resolved-comment .resolved-note {
  font-style: italic;
  color: var(--fg-dimmed);
}
.resolved-comment.expanded {
  flex-direction: column;
  align-items: flex-start;
}
.resolved-comment.expanded .resolved-body {
  white-space: normal;
  overflow: visible;
}
```

**Step 2: Build a resolved comments map by target line**

In `renderDocument()`, before the block loop, build a map of resolved comments keyed by their `resolution_lines`:

```javascript
function buildResolvedMap() {
  const map = {};
  for (const c of previousComments) {
    if (!c.resolved || !c.resolution_lines || c.resolution_lines.length === 0) continue;
    // Place the resolved comment after the last resolution line
    const targetLine = Math.max(...c.resolution_lines);
    if (!map[targetLine]) map[targetLine] = [];
    map[targetLine].push(c);
  }
  return map;
}
```

**Step 3: Render resolved comments after the relevant blocks**

In `renderDocument()`, after rendering a block's regular comments, check the resolved map and render any resolved comments whose target line falls within the block:

```javascript
function createResolvedElement(comment) {
  const el = document.createElement('div');
  el.className = 'resolved-comment';
  el.innerHTML = `
    <span class="resolved-check">\u2713</span>
    <span class="resolved-body">${escapeHtml(comment.body)}</span>
    ${comment.resolution_note ? `<span class="resolved-note">${escapeHtml(comment.resolution_note)}</span>` : ''}
  `;
  el.addEventListener('click', function() {
    el.classList.toggle('expanded');
  });
  return el;
}
```

**Step 4: Build and manually test**

Create a test `.comments.json` with resolved comments and verify they render correctly.

**Step 5: Commit**

```bash
git add frontend/index.html
git commit -m "Render resolved comments as collapsed cards at mapped positions"
```

---

### Task 11: Update the finish prompt and CLAUDE.md

Update the finish prompt to include the round-complete curl command with the actual port, and update CLAUDE.md to document the new features.

**Files:**
- Modify: `server.go` — add `port` field to Server, update finish prompt
- Modify: `main.go` — pass port to NewServer
- Modify: `CLAUDE.md` — document new endpoints and features

**Step 1: Add port to Server**

In `server.go`:
```go
type Server struct {
	// ... existing fields ...
	port int
}
```

Update `NewServer` to accept port:
```go
func NewServer(doc *Document, frontendFS embed.FS, shareURL string, currentVersion string, port int) *Server {
	s := &Server{doc: doc, shareURL: shareURL, currentVersion: currentVersion, port: port}
```

In `main.go`, pass `addr.Port` to `NewServer`:
```go
srv := NewServer(doc, frontendFS, *shareURL, version, addr.Port)
```

**Step 2: Update the finish prompt**

In `handleFinish`:
```go
if len(s.doc.GetComments()) > 0 {
	prompt = fmt.Sprintf(
		"I've left review comments in %s — please address each comment and update the plan accordingly. "+
			"Mark each resolved comment in %s by setting \"resolved\": true (optionally add \"resolution_note\" and \"resolution_lines\" pointing to relevant lines in the updated file). "+
			"When done, run: crit go %d",
		reviewFile, s.doc.commentsFilePath(), s.port)
}
```

**Step 3: Update CLAUDE.md**

Add to the API Endpoints section:
```
- `POST /api/round-complete` — agent signals all edits are done; triggers new round in the browser
- `GET  /api/previous-round` — returns previous round's content and comments for diff rendering
```

Add a new section:
```
## Multi-Round Review

When the agent runs `crit go <PORT>` (or calls `POST /api/round-complete`), the browser transitions to a new review round:
- A side-by-side diff panel (toggle in header) shows what changed since the previous round
- Previous comments marked as `resolved: true` in `.comments.json` appear as collapsed green cards at their `resolution_lines` positions
- The waiting modal shows a live count of file edits while the agent is working
```

**Step 4: Run all tests, fix any that break due to NewServer signature change**

Run: `go test ./... -v`

Update `newTestServer` in `server_test.go` if needed (it creates Server directly, may not need port).

**Step 5: Commit**

```bash
git add server.go main.go CLAUDE.md server_test.go
git commit -m "Update finish prompt with round-complete instructions and port"
```

---

### Task 12: Port diff panel and resolved comments to crit-web

Per the CLAUDE.md frontend parity rule, the review page in `crit-web` must stay in sync with the local frontend.

**Files:**
- Modify: `crit-web/assets/js/document-renderer.js` — port `computeLineDiff`, resolved comment rendering
- Modify: `crit-web/assets/css/app.css` — port diff panel and resolved comment CSS

**Note:** crit-web supports full commenting (not read-only), so the diff and resolved comment features will eventually need to be ported including the round-complete flow for hosted reviews. However, we're deferring the full crit-web adaptation until we're happy with the local flow. For now, add TODO comments documenting the parity gap and what needs to be ported.

**Step 1: Add TODO comment to document-renderer.js**

```javascript
// TODO: Port round-diff and resolved-comments features from crit local's index.html
// crit-web supports full commenting (not read-only), so this needs:
// - Diff panel rendering (fetch from backend or compute client-side)
// - Resolved comment cards at mapped positions
// - Round-complete flow for hosted reviews (agent signals completion)
// - Updated finish prompt with JSON file reference for hosted reviews
// See crit/frontend/index.html: renderDiffPanel(), createResolvedElement()
// See crit/diff.go: ComputeLineDiff()
```

**Step 2: Commit**

```bash
git add crit-web/assets/js/document-renderer.js
git commit -m "Add parity TODO for diff panel and resolved comments in crit-web"
```

---

### Task 13: Final integration test

Do a full manual walkthrough of the end-to-end flow.

**Steps:**
1. `go build -o crit . && ./crit test-plan.md`
2. Open the browser, leave a few comments on different lines
3. Click "Finish Review" — verify the prompt mentions round-complete and the JSON file
4. In a terminal, edit `test-plan.md` a few times — verify the waiting modal shows the edit count
5. Read the `.comments.json`, add `"resolved": true`, `"resolution_note": "Fixed"`, `"resolution_lines": [new line numbers]` to each comment
6. Run `crit go PORT` (or `curl -X POST http://localhost:PORT/api/round-complete`)
7. Verify: the browser transitions to round 2, the Diff button appears, clicking it shows side-by-side diff, resolved comments appear as collapsed green cards at the mapped positions
8. Verify the diff panel is hidden on mobile viewport

**After verification:**
```bash
git add -A && git commit -m "Integration verification complete"
```

---

## Summary of Changes

| File | Changes |
|---|---|
| `document.go` | `PreviousContent`, `PreviousComments` fields; `pendingEdits` counter; `roundComplete` channel; `IncrementEdits()`, `GetPendingEdits()`, `SignalRoundComplete()`, `RoundCompleteChan()` methods; `Comment` struct gains `Resolved`, `ResolutionNote`, `ResolutionLines` fields; `WatchFile` batches edits, sends `edit-detected` events; `ReloadFile` preserves previous state |
| `diff.go` | `DiffEntry` struct; `ComputeLineDiff(oldContent, newContent)` LCS algorithm |
| `server.go` | `port` field; `POST /api/round-complete` handler; `GET /api/previous-round` handler; `GET /api/diff` handler; updated finish prompt |
| `output.go` | `GenerateReviewMD` filters resolved comments, gains agent instructions footer with `crit go` and resolved comment docs |
| `main.go` | `crit go <PORT>` subcommand; passes port to `NewServer` |
| `frontend/index.html` | `edit-detected` SSE handler; edit counter in waiting modal; side-by-side diff panel with toggle (renders server-computed diff); resolved comment rendering as collapsed cards; mobile CSS hiding |
| `CLAUDE.md` | Documents new endpoints and multi-round workflow |
| Test files | Tests for diff algorithm, previous content preservation, round-complete endpoint, edit counting, resolved comments, previous-round endpoint, diff endpoint |

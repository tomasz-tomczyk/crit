# CLI Status Output Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add polished terminal status output to the crit CLI matching the marketing homepage preview — showing round summaries, finish confirmations, agent waiting state, edit detection counts, and diff readiness with ANSI colors.

**Architecture:** New `status.go` with a `Status` struct wrapping `io.Writer` for testable formatted output. ANSI color support with `NO_COLOR` env var and terminal detection. Status methods called from existing event points in `server.go` (finish handler) and `document.go` (WatchFile round-complete handler). Document gains `lastRoundEdits` field to preserve edit count across `SignalRoundComplete`'s reset.

**Tech Stack:** Go stdlib (`fmt`, `io`, `os`), ANSI escape codes, no new dependencies.

**Target output** (matching marketing screenshot):
```
$ crit plan.md
  Listening on http://localhost:3247
→ Round 1: 3 comments added
→ Finish review — prompt copied ✓
→ Waiting for agent…
→ File updated (8 edits detected)
→ Round 2: diff ready — 2 resolved, 1 open
```

---

### Task 1: Create status.go with formatting functions

**Files:**
- Create: `crit/status.go`
- Create: `crit/status_test.go`

**Step 1: Write the failing tests**

Create `crit/status_test.go`:

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func testStatus() (*Status, *bytes.Buffer) {
	var buf bytes.Buffer
	return &Status{w: &buf, color: false}, &buf
}

func TestStatusListening(t *testing.T) {
	s, buf := testStatus()
	s.Listening("http://localhost:3247")
	want := "  Listening on http://localhost:3247\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusRoundFinished_WithComments(t *testing.T) {
	s, buf := testStatus()
	s.RoundFinished(1, 3, true)
	want := "→ Round 1: 3 comments added\n→ Finish review — prompt copied ✓\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusRoundFinished_SingleComment(t *testing.T) {
	s, buf := testStatus()
	s.RoundFinished(2, 1, true)
	want := "→ Round 2: 1 comment added\n→ Finish review — prompt copied ✓\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusRoundFinished_NoComments(t *testing.T) {
	s, buf := testStatus()
	s.RoundFinished(1, 0, false)
	want := "→ Finish review\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusWaitingForAgent(t *testing.T) {
	s, buf := testStatus()
	s.WaitingForAgent()
	want := "→ Waiting for agent…\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusFileUpdated(t *testing.T) {
	s, buf := testStatus()
	s.FileUpdated(8)
	want := "→ File updated (8 edits detected)\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusFileUpdated_Singular(t *testing.T) {
	s, buf := testStatus()
	s.FileUpdated(1)
	want := "→ File updated (1 edit detected)\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusFileUpdated_Zero(t *testing.T) {
	s, buf := testStatus()
	s.FileUpdated(0)
	if got := buf.String(); got != "" {
		t.Errorf("expected no output for 0 edits, got %q", got)
	}
}

func TestStatusRoundReady_ResolvedAndOpen(t *testing.T) {
	s, buf := testStatus()
	s.RoundReady(2, 2, 1)
	want := "→ Round 2: diff ready — 2 resolved, 1 open\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusRoundReady_AllResolved(t *testing.T) {
	s, buf := testStatus()
	s.RoundReady(2, 3, 0)
	want := "→ Round 2: diff ready — 3 resolved\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusRoundReady_NoneResolved(t *testing.T) {
	s, buf := testStatus()
	s.RoundReady(3, 0, 2)
	want := "→ Round 3: diff ready — 2 open\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusRoundReady_NoPreviousComments(t *testing.T) {
	s, buf := testStatus()
	s.RoundReady(2, 0, 0)
	want := "→ Round 2: diff ready\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStatusColor_IncludesAnsiCodes(t *testing.T) {
	var buf bytes.Buffer
	s := &Status{w: &buf, color: true}
	s.Listening("http://localhost:3247")
	out := buf.String()
	if !strings.Contains(out, "\033[2m") {
		t.Error("expected dim ANSI code in colored output")
	}
	if !strings.Contains(out, "\033[0m") {
		t.Error("expected reset ANSI code in colored output")
	}
}

func TestStatusColor_GreenInRoundReady(t *testing.T) {
	var buf bytes.Buffer
	s := &Status{w: &buf, color: true}
	s.RoundReady(2, 2, 1)
	out := buf.String()
	if !strings.Contains(out, "\033[32m") {
		t.Error("expected green ANSI code for resolved count")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd crit && go test ./... -run TestStatus -v`
Expected: FAIL — `status.go` doesn't exist yet

**Step 3: Implement status.go**

Create `crit/status.go`:

```go
package main

import (
	"fmt"
	"io"
	"os"
)

const (
	ansiDim   = "\033[2m"
	ansiGreen = "\033[32m"
	ansiReset = "\033[0m"
)

// Status handles formatted terminal output for the crit review lifecycle.
type Status struct {
	w     io.Writer
	color bool
}

func newStatus(w io.Writer) *Status {
	color := true
	if os.Getenv("NO_COLOR") != "" {
		color = false
	} else if f, ok := w.(*os.File); ok {
		fi, err := f.Stat()
		if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
			color = false
		}
	} else {
		// Not a file (e.g. bytes.Buffer in tests) — no color
		color = false
	}
	return &Status{w: w, color: color}
}

func (s *Status) dim(text string) string {
	if s.color {
		return ansiDim + text + ansiReset
	}
	return text
}

func (s *Status) green(text string) string {
	if s.color {
		return ansiGreen + text + ansiReset
	}
	return text
}

func (s *Status) arrow() string {
	return s.dim("→")
}

// Listening prints the server URL on startup.
func (s *Status) Listening(url string) {
	fmt.Fprintf(s.w, "  %s\n", s.dim("Listening on "+url))
}

// RoundFinished prints the round summary and finish confirmation.
func (s *Status) RoundFinished(round, commentCount int, hasPrompt bool) {
	if commentCount > 0 {
		noun := "comments"
		if commentCount == 1 {
			noun = "comment"
		}
		fmt.Fprintf(s.w, "%s Round %d: %d %s added\n", s.arrow(), round, commentCount, noun)
	}
	if hasPrompt {
		fmt.Fprintf(s.w, "%s Finish review — prompt copied %s\n", s.arrow(), s.green("✓"))
	} else {
		fmt.Fprintf(s.w, "%s Finish review\n", s.arrow())
	}
}

// WaitingForAgent prints the waiting state.
func (s *Status) WaitingForAgent() {
	fmt.Fprintf(s.w, "%s %s\n", s.arrow(), s.dim("Waiting for agent…"))
}

// FileUpdated prints the edit detection summary. Skips output for 0 edits.
func (s *Status) FileUpdated(editCount int) {
	if editCount == 0 {
		return
	}
	noun := "edits"
	if editCount == 1 {
		noun = "edit"
	}
	fmt.Fprintf(s.w, "%s %s\n", s.arrow(), s.dim(fmt.Sprintf("File updated (%d %s detected)", editCount, noun)))
}

// RoundReady prints the new round summary with resolved/open counts.
func (s *Status) RoundReady(round, resolved, open int) {
	line := fmt.Sprintf("Round %d: diff ready", round)
	if resolved > 0 && open > 0 {
		line += " — " + s.green(fmt.Sprintf("%d resolved", resolved)) + fmt.Sprintf(", %d open", open)
	} else if resolved > 0 {
		line += " — " + s.green(fmt.Sprintf("%d resolved", resolved))
	} else if open > 0 {
		line += fmt.Sprintf(" — %d open", open)
	}
	fmt.Fprintf(s.w, "%s %s\n", s.arrow(), line)
}
```

**Step 4: Run tests to verify they pass**

Run: `cd crit && go test ./... -run TestStatus -v`
Expected: All PASS

**Step 5: Commit**

```bash
cd crit && git add status.go status_test.go
git commit -m "feat: add status output formatting for CLI lifecycle events"
```

---

### Task 2: Add lastRoundEdits field and getters to Document

**Files:**
- Modify: `crit/document.go` (Document struct, SignalRoundComplete, new getter methods)
- Modify: `crit/document_test.go` (new tests)

**Step 1: Write the failing tests**

Add to bottom of `crit/document_test.go`:

```go
func TestGetReviewRound(t *testing.T) {
	doc := newTestDoc(t, "hello")
	if got := doc.GetReviewRound(); got != 1 {
		t.Errorf("initial round = %d, want 1", got)
	}
}

func TestSignalRoundComplete_PreservesEditCount(t *testing.T) {
	doc := newTestDoc(t, "hello")
	doc.IncrementEdits()
	doc.IncrementEdits()
	doc.IncrementEdits()
	doc.SignalRoundComplete()
	if got := doc.GetLastRoundEdits(); got != 3 {
		t.Errorf("lastRoundEdits = %d, want 3", got)
	}
	if got := doc.GetPendingEdits(); got != 0 {
		t.Errorf("pendingEdits = %d, want 0 after round complete", got)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd crit && go test ./... -run "TestGetReviewRound|TestSignalRoundComplete_Preserves" -v`
Expected: FAIL — methods don't exist

**Step 3: Add field and methods to Document**

In `crit/document.go`, add `lastRoundEdits` and `status` fields to the Document struct. After line 59 (`pendingEdits int`), add:

```go
	lastRoundEdits   int           // pendingEdits captured at last round-complete
	status           *Status       // optional terminal status output
```

Add getter methods (after existing `GetPendingEdits`):

```go
func (d *Document) GetLastRoundEdits() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastRoundEdits
}

func (d *Document) GetReviewRound() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.reviewRound
}
```

Modify `SignalRoundComplete` to capture edits before reset — change:

```go
// OLD (line 247-248):
	d.mu.Lock()
	d.pendingEdits = 0
```

To:

```go
// NEW:
	d.mu.Lock()
	d.lastRoundEdits = d.pendingEdits
	d.pendingEdits = 0
```

**Step 4: Run all tests**

Run: `cd crit && go test ./... -v`
Expected: All PASS (existing + new)

**Step 5: Commit**

```bash
cd crit && git add document.go document_test.go
git commit -m "feat: add GetReviewRound, GetLastRoundEdits, preserve edit count on round complete"
```

---

### Task 3: Wire up status output at all integration points

**Files:**
- Modify: `crit/main.go` (lines 119-121: startup output, plus create/attach Status instance)
- Modify: `crit/server.go` (lines 15-24: add status field to Server; lines 269-294: handleFinish)
- Modify: `crit/document.go` (lines 440-456: WatchFile roundComplete case)

**Step 1: Add status field to Server struct**

In `crit/server.go`, add `status *Status` to the Server struct (after `port int`):

```go
type Server struct {
	doc            *Document
	mux            *http.ServeMux
	assets         fs.FS
	shareURL       string
	currentVersion string
	latestVersion  string
	versionMu      sync.RWMutex
	port           int
	status         *Status
}
```

**Step 2: Replace startup output in main.go**

In `crit/main.go`, replace lines 119-121:

```go
// OLD:
	url := fmt.Sprintf("http://localhost:%d", addr.Port)
	fmt.Printf("Crit serving %s\n", filepath.Base(absPath))
	fmt.Printf("Open %s in your browser\n", url)

// NEW:
	status := newStatus(os.Stdout)
	srv.status = status
	doc.status = status

	url := fmt.Sprintf("http://localhost:%d", addr.Port)
	status.Listening(url)
```

Remove the `"path/filepath"` import if it's now unused. Check: `filepath.Abs` is used on line 72, `filepath.Dir` on line 87, so `filepath` is still needed. But `filepath.Base` on old line 120 was its only other use — verify the import is still needed for the other calls. (It is, so keep it.)

**Step 3: Replace handleFinish status output in server.go**

In `crit/server.go`, replace the `handleFinish` method entirely:

```go
func (s *Server) handleFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.doc.WriteFiles()

	reviewFile := s.doc.reviewFilePath()
	comments := s.doc.GetComments()
	prompt := ""
	if len(comments) > 0 {
		prompt = fmt.Sprintf(
			"Address review comments in %s. "+
				"Mark resolved in %s (set \"resolved\": true, optionally \"resolution_note\" and \"resolution_lines\"). "+
				"When done run: `crit go %d`",
			reviewFile, s.doc.commentsFilePath(), s.port)
	}

	writeJSON(w, map[string]string{
		"status":      "finished",
		"review_file": reviewFile,
		"prompt":      prompt,
	})

	if s.status != nil {
		round := s.doc.GetReviewRound()
		s.status.RoundFinished(round, len(comments), len(comments) > 0)
		if len(comments) > 0 {
			s.status.WaitingForAgent()
		}
	}
}
```

**Step 4: Add status output to WatchFile roundComplete handler in document.go**

In `crit/document.go`, replace the `case <-d.roundComplete:` block in `WatchFile` (lines 440-455):

```go
		case <-d.roundComplete:
			// Read captured edit count (set by SignalRoundComplete before reset)
			d.mu.RLock()
			edits := d.lastRoundEdits
			d.mu.RUnlock()

			// Load agent's resolved comments from .comments.json before cleanup
			d.loadResolvedComments()
			os.Remove(d.commentsFilePath())
			os.Remove(d.reviewFilePath())

			// Count resolved and open comments from previous round
			d.mu.RLock()
			resolved, open := 0, 0
			for _, c := range d.PreviousComments {
				if c.Resolved {
					resolved++
				} else {
					open++
				}
			}
			round := d.reviewRound
			event := SSEEvent{
				Type:     "file-changed",
				Filename: d.FileName,
				Content:  d.Content,
			}
			d.mu.RUnlock()

			// Terminal status output
			if d.status != nil {
				d.status.FileUpdated(edits)
				d.status.RoundReady(round, resolved, open)
			}

			d.notify(event)
```

**Step 5: Run all tests**

Run: `cd crit && go test ./... -v`
Expected: All PASS

**Step 6: Run linter**

Run: `cd crit && gofmt -l .`
Expected: No output (all formatted)

**Step 7: Build**

Run: `cd crit && go build -o crit .`
Expected: Clean build

**Step 8: Commit**

```bash
cd crit && git add main.go server.go document.go
git commit -m "feat: wire up polished CLI status output for review lifecycle"
```

---

### Task 4: Manual end-to-end verification

**Files:** None — verification only

**Step 1: Start crit with test file**

Run: `cd crit && ./crit test-plan.md`

Verify: Terminal shows `  Listening on http://localhost:<port>` (dimmed text, no "Crit serving" or "Open in browser" lines)

**Step 2: Add a comment and finish review**

In the browser: select some lines, add a comment, click "Finish Review"

Verify terminal output:
```
→ Round 1: 1 comment added
→ Finish review — prompt copied ✓
→ Waiting for agent…
```

**Step 3: Edit the file and signal round complete**

In another terminal: edit `test-plan.md` (add a line), then run `./crit go <port>`

Verify terminal output:
```
→ File updated (1 edit detected)
→ Round 2: diff ready — …
```

**Step 4: Ctrl+C to stop**

Verify: Still shows `Shutting down...` and the prompt text (unchanged shutdown behavior)

---

## Summary of changes

| File | Change |
|------|--------|
| `crit/status.go` | **New.** `Status` struct with `Listening`, `RoundFinished`, `WaitingForAgent`, `FileUpdated`, `RoundReady` methods. ANSI color support with `NO_COLOR` detection. |
| `crit/status_test.go` | **New.** 14 test cases covering all status formats, singular/plural, edge cases, and color output. |
| `crit/document.go` | Add `lastRoundEdits` and `status` fields to Document. Add `GetReviewRound()` and `GetLastRoundEdits()` getters. Capture `pendingEdits` in `SignalRoundComplete` before reset. Add status output in `WatchFile` roundComplete handler. |
| `crit/document_test.go` | Add tests for `GetReviewRound` and `SignalRoundComplete` edit count preservation. |
| `crit/server.go` | Add `status` field to Server. Replace `handleFinish` printf with status calls showing round summary + waiting. |
| `crit/main.go` | Create `Status` instance, attach to Server and Document. Replace 2-line startup output with `status.Listening(url)`. |

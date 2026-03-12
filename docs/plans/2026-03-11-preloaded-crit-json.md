# Pre-loaded .crit.json & GitHub PR Sync

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable three new workflows: (1) AI agents generate `.crit.json` that crit loads on startup, (2) `crit pull` fetches GitHub PR review comments into `.crit.json`, (3) `crit push` posts `.crit.json` comments to a GitHub PR. Together these make crit a local-first review tool that syncs bidirectionally with GitHub.

**Architecture:** Six changes: (1) relax `loadCritJSON()` hash check, (2) `crit pull` to fetch PR comments (new `github.go`), (3) `crit push` to post comments to PR, (4) `crit comment` CLI for agents to add comments without writing JSON, (5) AI review skill teaching agents to use `crit comment`, (6) author display and color-coding in the frontend. Pull/push shell out to `gh` CLI. Comment is pure Go, no dependencies.

**Tech Stack:** Go (backend), `gh` CLI (GitHub API for pull/push only), Markdown (skill file)

**Execution order:** Chunks 1 → 2 → 4 → 3 → 5 → 6 → 7 → 8. Build `crit comment` (Chunk 4) before `crit push` (Chunk 3) so we can dogfood: create a draft PR for this branch, use `crit comment` to add local comments, then test `crit push` by pushing them to our own PR. Similarly, test `crit pull` by posting comments on the draft PR in GitHub and pulling them down.

---

## Chunk 1: Relax hash check in loadCritJSON

### Task 1: Write failing test for hashless .crit.json loading

**Files:**
- Modify: `session_test.go`

The current `TestSession_LoadCritJSON` test creates a .crit.json WITH matching hashes. We need a test that creates a .crit.json WITHOUT `file_hash` fields and verifies comments still load.

- [ ] **Step 1: Write the failing test**

Add this test after the existing `TestSession_LoadCritJSON` (around line 335):

```go
func TestSession_LoadCritJSON_NoHash(t *testing.T) {
	s := newTestSession(t)

	// Write a .crit.json without file_hash fields (simulating agent-generated review)
	cj := `{
		"branch": "test",
		"base_ref": "",
		"updated_at": "2025-01-01T00:00:00Z",
		"review_round": 1,
		"files": {
			"plan.md": {
				"status": "added",
				"comments": [
					{
						"id": "c1",
						"start_line": 1,
						"end_line": 1,
						"body": "agent review comment",
						"created_at": "2025-01-01T00:00:00Z",
						"resolved": false
					}
				]
			}
		}
	}`
	if err := os.WriteFile(s.critJSONPath(), []byte(cj), 0644); err != nil {
		t.Fatalf("write .crit.json: %v", err)
	}

	s.loadCritJSON()

	comments := s.GetComments("plan.md")
	if len(comments) != 1 {
		t.Fatalf("expected 1 loaded comment, got %d", len(comments))
	}
	if comments[0].Body != "agent review comment" {
		t.Errorf("Body = %q, want %q", comments[0].Body, "agent review comment")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/tomasztomczyk/Server/side/crit-mono/crit && go test -run TestSession_LoadCritJSON_NoHash -v`

Expected: FAIL — `expected 1 loaded comment, got 0` (hash mismatch causes comments to be skipped)

- [ ] **Step 3: Commit the failing test**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit
git add session_test.go
git commit -m "test: add failing test for loading .crit.json without file_hash"
```

---

### Task 2: Write failing test for wrong-hash .crit.json loading

**Files:**
- Modify: `session_test.go`

Also test the case where `file_hash` is present but wrong (file was edited after the agent wrote .crit.json). Comments should still load — the user explicitly put the file there.

- [ ] **Step 1: Write the failing test**

```go
func TestSession_LoadCritJSON_MismatchedHash(t *testing.T) {
	s := newTestSession(t)

	// Write a .crit.json with a stale/wrong file_hash
	cj := `{
		"branch": "test",
		"base_ref": "",
		"updated_at": "2025-01-01T00:00:00Z",
		"review_round": 1,
		"files": {
			"plan.md": {
				"status": "added",
				"file_hash": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
				"comments": [
					{
						"id": "c1",
						"start_line": 1,
						"end_line": 1,
						"body": "stale hash comment",
						"created_at": "2025-01-01T00:00:00Z",
						"resolved": false
					}
				]
			}
		}
	}`
	if err := os.WriteFile(s.critJSONPath(), []byte(cj), 0644); err != nil {
		t.Fatalf("write .crit.json: %v", err)
	}

	s.loadCritJSON()

	comments := s.GetComments("plan.md")
	if len(comments) != 1 {
		t.Fatalf("expected 1 loaded comment, got %d", len(comments))
	}
	if comments[0].Body != "stale hash comment" {
		t.Errorf("Body = %q, want %q", comments[0].Body, "stale hash comment")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/tomasztomczyk/Server/side/crit-mono/crit && go test -run TestSession_LoadCritJSON_MismatchedHash -v`

Expected: FAIL — `expected 1 loaded comment, got 0`

- [ ] **Step 3: Commit the failing test**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit
git add session_test.go
git commit -m "test: add failing test for loading .crit.json with mismatched hash"
```

---

### Task 3: Remove hash check from loadCritJSON

**Files:**
- Modify: `session.go:689-718` (`loadCritJSON` function)

The fix: remove the `if cf.FileHash == f.FileHash` guard. Always load comments if the path matches. The hash served as a staleness check, but it causes more harm than good — it silently discards valid agent-generated reviews.

The hash is still WRITTEN to `.crit.json` by `WriteFiles()` (via `CritJSONFile.FileHash`). This is fine — it's useful metadata. We just stop using it as a gate for loading.

- [ ] **Step 1: Modify loadCritJSON to remove the hash check**

In `session.go`, change the `loadCritJSON` function. The current code (lines 703-717):

```go
// Restore comments for files that match by path and hash
for _, f := range s.Files {
    if cf, ok := cj.Files[f.Path]; ok {
        if cf.FileHash == f.FileHash {
            f.Comments = cf.Comments
            for _, c := range f.Comments {
                id := 0
                _, _ = fmt.Sscanf(c.ID, "c%d", &id)
                if id >= f.nextID {
                    f.nextID = id + 1
                }
            }
        }
    }
}
```

Replace with (remove the hash check, un-nest the inner block):

```go
// Restore comments for files that match by path
for _, f := range s.Files {
    if cf, ok := cj.Files[f.Path]; ok {
        f.Comments = cf.Comments
        for _, c := range f.Comments {
            id := 0
            _, _ = fmt.Sscanf(c.ID, "c%d", &id)
            if id >= f.nextID {
                f.nextID = id + 1
            }
        }
    }
}
```

- [ ] **Step 2: Run both new tests to verify they pass**

Run: `cd /Users/tomasztomczyk/Server/side/crit-mono/crit && go test -run "TestSession_LoadCritJSON_NoHash|TestSession_LoadCritJSON_MismatchedHash" -v`

Expected: Both PASS

- [ ] **Step 3: Run all tests to verify no regressions**

Run: `cd /Users/tomasztomczyk/Server/side/crit-mono/crit && go test ./... -v`

Expected: All PASS. The existing `TestSession_LoadCritJSON` test still passes because it sets matching hashes — removing the check is strictly more permissive.

- [ ] **Step 4: Run linters**

Run: `cd /Users/tomasztomczyk/Server/side/crit-mono/crit && gofmt -l . && golangci-lint run ./...`

Expected: Clean

- [ ] **Step 5: Commit**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit
git add session.go
git commit -m "feat: load .crit.json comments without requiring file_hash match

Previously, loadCritJSON only restored comments when the file_hash in
.crit.json matched the current file's hash. This prevented AI agents
from programmatically generating .crit.json review files, since they
would need to compute the exact same sha256 hash format.

Remove the hash gate so comments load for any file that matches by path.
The hash is still written to .crit.json as useful metadata."
```

---

## Chunk 2: `crit pull` — fetch GitHub PR comments to .crit.json

### Task 4: Write `crit pull` subcommand

**Files:**
- Create: `github.go` — all GitHub/`gh` CLI interaction logic
- Modify: `main.go` — add subcommand dispatch

This adds `crit pull [--pr <number>]` which:
1. Detects the current PR (from branch name via `gh pr view`)
2. Fetches all review comments via `gh api`
3. Maps them to `.crit.json` format (GitHub's `line`/`start_line` fields map directly to crit's `start_line`/`end_line`)
4. Writes `.crit.json` to the repo root

**Design decisions:**
- Shell out to `gh` via `exec.Command` — no GitHub API library needed, `gh` handles auth
- GitHub review comments use `path`, `line` (end line), `start_line` (optional, for multi-line), and `side` ("RIGHT" for new code, "LEFT" for old code). We only import RIGHT-side comments since crit shows the current file state.
- PR-level review body comments (not attached to a line) are skipped — crit only supports inline comments. **Future improvement:** LEFT-side comments (on deleted lines) could be supported by mapping them to the nearest surviving line or displaying them as file-level comments.
- Author is stored as a structured `author` field on Comment (not prefixed into body). This enables color-coding in the frontend.
- **Prerequisite**: Add `Author string \`json:"author,omitempty"\`` field to the `Comment` struct in `session.go` (after `Body`). This is a backwards-compatible addition — existing `.crit.json` files without `author` will unmarshal with an empty string.

- [ ] **Step 1: Create `github.go` with helper functions**

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ghComment represents a GitHub PR review comment from the API.
type ghComment struct {
	ID        int    `json:"id"`
	Path      string `json:"path"`
	Line      int    `json:"line"`       // end line in the diff (RIGHT side = new file line)
	StartLine int    `json:"start_line"` // start line for multi-line comments (0 if single-line)
	Side      string `json:"side"`       // "RIGHT" or "LEFT"
	Body      string `json:"body"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt string `json:"created_at"`
}

// requireGH checks that the gh CLI is installed and authenticated.
func requireGH() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not found. Install it: https://cli.github.com")
	}
	cmd := exec.Command("gh", "auth", "status")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh is not authenticated. Run: gh auth login")
	}
	return nil
}

// detectPR returns the PR number for the current branch.
// If prFlag is non-zero, it's used directly.
func detectPR(prFlag int) (int, error) {
	if prFlag > 0 {
		return prFlag, nil
	}
	out, err := exec.Command("gh", "pr", "view", "--json", "number", "--jq", ".number").Output()
	if err != nil {
		return 0, fmt.Errorf("no PR found for current branch. Use --pr <number> or push your branch first")
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("unexpected PR number: %s", string(out))
	}
	return n, nil
}

// fetchPRComments fetches all review comments for a PR.
func fetchPRComments(prNumber int) ([]ghComment, error) {
	// Use gh api with pagination to get all comments
	out, err := exec.Command("gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/comments", prNumber),
		"--paginate",
		"--jq", ".",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("fetching PR comments: %w", err)
	}

	var comments []ghComment
	if err := json.Unmarshal(out, &comments); err != nil {
		// gh --paginate with --jq outputs concatenated arrays, try line-by-line
		// Actually, gh api --paginate concatenates JSON arrays properly
		return nil, fmt.Errorf("parsing PR comments: %w", err)
	}
	return comments, nil
}

// mergeGHComments appends GitHub PR comments into an existing CritJSON.
// Only includes RIGHT-side comments (comments on the new version of the file).
// Merges with existing comments — does not replace them.
func mergeGHComments(cj *CritJSON, comments []ghComment) int {
	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now
	added := 0

	// Group comments by file path, filter to RIGHT side only
	for _, gc := range comments {
		if gc.Line == 0 {
			continue // skip PR-level comments not attached to a line
		}
		if gc.Side == "LEFT" {
			continue // skip comments on deleted/old lines
		}

		cf, ok := cj.Files[gc.Path]
		if !ok {
			cf = CritJSONFile{
				Status:   "modified",
				Comments: []Comment{},
			}
		}

		startLine := gc.StartLine
		if startLine == 0 {
			startLine = gc.Line // single-line comment
		}

		// Generate next ID based on existing comments
		nextID := 1
		for _, c := range cf.Comments {
			id := 0
			_, _ = fmt.Sscanf(c.ID, "c%d", &id)
			if id >= nextID {
				nextID = id + 1
			}
		}

		cf.Comments = append(cf.Comments, Comment{
			ID:        fmt.Sprintf("c%d", nextID),
			StartLine: startLine,
			EndLine:   gc.Line,
			Body:      gc.Body,
			Author:    gc.User.Login,
			CreatedAt: gc.CreatedAt,
			UpdatedAt: now,
		})
		cj.Files[gc.Path] = cf
		added++
	}

	return added
}

// writeCritJSON writes a CritJSON to the repo root.
func writeCritJSON(cj CritJSON) error {
	root, err := RepoRoot()
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling .crit.json: %w", err)
	}

	path := root + "/.crit.json"
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing .crit.json: %w", err)
	}
	return nil
}

// critJSONToGHComments converts .crit.json comments to GitHub review comment format.
// Returns the list of comments suitable for the GitHub "create review" API.
func critJSONToGHComments(cj CritJSON) []map[string]any {
	var result []map[string]any
	for path, cf := range cj.Files {
		for _, c := range cf.Comments {
			if c.Resolved {
				continue // don't post resolved comments
			}
			comment := map[string]any{
				"path": path,
				"line": c.EndLine,
				"side": "RIGHT",
				"body": c.Body,
			}
			if c.StartLine != c.EndLine {
				comment["start_line"] = c.StartLine
				comment["start_side"] = "RIGHT"
			}
			result = append(result, comment)
		}
	}
	return result
}

// createGHReview posts a review with inline comments to a GitHub PR.
func createGHReview(prNumber int, comments []map[string]any) error {
	review := map[string]any{
		"event":    "COMMENT",
		"body":     "Review from crit",
		"comments": comments,
	}

	data, err := json.Marshal(review)
	if err != nil {
		return fmt.Errorf("marshaling review: %w", err)
	}

	cmd := exec.Command("gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/reviews", prNumber),
		"--method", "POST",
		"--input", "-",
	)
	cmd.Stdin = strings.NewReader(string(data))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("creating review: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Run `gofmt` and fix any formatting**

Run: `cd /Users/tomasztomczyk/Server/side/crit-mono/crit && gofmt -w github.go`

- [ ] **Step 3: Commit**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit
git add github.go
git commit -m "feat: add GitHub PR integration helpers

Adds github.go with functions for:
- Detecting PR number from current branch via gh CLI
- Fetching PR review comments via GitHub API
- Converting between GitHub comment format and .crit.json format
- Posting reviews back to GitHub PRs

Requires gh CLI to be installed and authenticated."
```

---

### Task 5: Wire up `crit pull` subcommand in main.go

**Files:**
- Modify: `main.go:34-60` (subcommand dispatch area)

Add the `crit pull` subcommand alongside the existing `crit go` and `crit install` handlers.

- [ ] **Step 1: Add `crit pull` handler in main.go**

Add this block after the `crit install` handler (after line 83) and before the `flag.Int` line (line 85):

```go
// Handle "crit pull [--pr <number>]" subcommand — fetch GitHub PR comments to .crit.json
if len(os.Args) >= 2 && os.Args[1] == "pull" {
	if err := requireGH(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	prFlag := 0
	for i, arg := range os.Args[2:] {
		if arg == "--pr" && i+1 < len(os.Args[2:])-1 {
			n, err := strconv.Atoi(os.Args[2+i+1+1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: --pr requires a number\n")
				os.Exit(1)
			}
			prFlag = n
		}
	}

	prNumber, err := detectPR(prFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	comments, err := fetchPRComments(prNumber)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	branch := CurrentBranch()
	baseRef, _ := MergeBase(DefaultBranch())

	cj := ghCommentsToCritJSON(comments, branch, baseRef)

	if len(cj.Files) == 0 {
		fmt.Printf("No inline comments found on PR #%d\n", prNumber)
		os.Exit(0)
	}

	if err := writeCritJSON(cj); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	total := 0
	for _, cf := range cj.Files {
		total += len(cf.Comments)
	}
	fmt.Printf("Pulled %d comments from PR #%d into .crit.json\n", total, prNumber)
	fmt.Println("Run 'crit' to view them in the browser.")
	os.Exit(0)
}
```

**Note:** The `--pr` flag parsing above is intentionally simple (no flag library for subcommands). Read the existing `os.Args` patterns in main.go and match the style. An alternative — if the arg parsing gets messy — is to just use positional: `crit pull` or `crit pull 34`. Pick whichever matches the existing style better. Suggested approach: positional is simpler and matches `crit go <port>`:

```go
// Handle "crit pull [pr-number]" subcommand
if len(os.Args) >= 2 && os.Args[1] == "pull" {
	if err := requireGH(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	prFlag := 0
	if len(os.Args) >= 3 {
		n, err := strconv.Atoi(os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Usage: crit pull [pr-number]\n")
			os.Exit(1)
		}
		prFlag = n
	}

	prNumber, err := detectPR(prFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ghComments, err := fetchPRComments(prNumber)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Load existing .crit.json or create new
	root, err := RepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: not in a git repository\n")
		os.Exit(1)
	}
	var cj CritJSON
	if data, err := os.ReadFile(root + "/.crit.json"); err == nil {
		json.Unmarshal(data, &cj)
	}
	if cj.Files == nil {
		cj.Files = make(map[string]CritJSONFile)
		cj.Branch = CurrentBranch()
		cj.BaseRef, _ = MergeBase(DefaultBranch())
		cj.ReviewRound = 1
	}

	added := mergeGHComments(&cj, ghComments)

	if added == 0 {
		fmt.Printf("No new inline comments found on PR #%d\n", prNumber)
		os.Exit(0)
	}

	if err := writeCritJSON(cj); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Pulled %d comments from PR #%d into .crit.json\n", added, prNumber)
	fmt.Println("Run 'crit' to view them in the browser.")
	os.Exit(0)
}
```

- [ ] **Step 2: Add `strconv` to imports if not already present**

Check the imports in `main.go`. Currently it does not import `strconv`. Add it.

- [ ] **Step 3: Update `printHelp()` to include `pull`**

In the help text in `main.go`, add the new subcommand:

```go
// In the Usage section:
crit pull [pr-number]         Fetch GitHub PR review comments to .crit.json
```

- [ ] **Step 4: Run tests and linters**

Run: `cd /Users/tomasztomczyk/Server/side/crit-mono/crit && go build . && gofmt -l . && golangci-lint run ./...`

Expected: Builds clean, no lint errors

- [ ] **Step 5: Commit**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit
git add main.go
git commit -m "feat: add 'crit pull' subcommand to fetch PR comments

crit pull [pr-number] fetches GitHub PR review comments and writes
them to .crit.json. If no PR number is given, auto-detects from the
current branch. Requires gh CLI."
```

---

## Chunk 3: `crit push` — post .crit.json comments to GitHub PR

### Task 6: Wire up `crit push` subcommand in main.go

**Files:**
- Modify: `main.go` (add subcommand dispatch)

Adds `crit push [--dry-run] [pr-number]` which:
1. Reads `.crit.json` from repo root
2. Filters to unresolved comments only
3. With `--dry-run`: prints what would be posted without actually calling GitHub
4. Without `--dry-run`: creates a GitHub PR review with all comments attached as inline review comments
5. Uses the GitHub "create review" API which posts all comments atomically as a single review

- [ ] **Step 1: Add `crit push` handler in main.go**

Add this block after the `crit pull` handler:

```go
// Handle "crit push [--dry-run] [pr-number]" subcommand — post .crit.json comments to GitHub PR
if len(os.Args) >= 2 && os.Args[1] == "push" {
	if err := requireGH(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	prFlag := 0
	dryRun := false
	for _, arg := range os.Args[2:] {
		if arg == "--dry-run" {
			dryRun = true
			continue
		}
		n, err := strconv.Atoi(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Usage: crit push [--dry-run] [pr-number]\n")
			os.Exit(1)
		}
		prFlag = n
	}

	prNumber, err := detectPR(prFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Read .crit.json
	root, err := RepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: not in a git repository\n")
		os.Exit(1)
	}
	data, err := os.ReadFile(root + "/.crit.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: no .crit.json found. Run a crit review first.\n")
		os.Exit(1)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid .crit.json: %v\n", err)
		os.Exit(1)
	}

	ghComments := critJSONToGHComments(cj)
	if len(ghComments) == 0 {
		fmt.Println("No unresolved comments to push.")
		os.Exit(0)
	}

	if dryRun {
		fmt.Printf("Would post %d comments to PR #%d:\n\n", len(ghComments), prNumber)
		for _, c := range ghComments {
			path := c["path"].(string)
			line := c["line"].(int)
			body := c["body"].(string)
			if sl, ok := c["start_line"]; ok {
				fmt.Printf("  %s:%d-%d\n", path, sl.(int), line)
			} else {
				fmt.Printf("  %s:%d\n", path, line)
			}
			fmt.Printf("    %s\n\n", body)
		}
		os.Exit(0)
	}

	fmt.Printf("Pushing %d comments to PR #%d...\n", len(ghComments), prNumber)
	if err := createGHReview(prNumber, ghComments); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Posted %d review comments to PR #%d\n", len(ghComments), prNumber)
	os.Exit(0)
}
```

- [ ] **Step 2: Update `printHelp()` to include `push`**

```go
// In the Usage section:
crit push [--dry-run] [pr-number]  Post .crit.json comments to a GitHub PR
```

- [ ] **Step 3: Run tests and linters**

Run: `cd /Users/tomasztomczyk/Server/side/crit-mono/crit && go build . && gofmt -l . && golangci-lint run ./...`

Expected: Builds clean, no lint errors

- [ ] **Step 4: Commit**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit
git add main.go
git commit -m "feat: add 'crit push' subcommand to post comments to PR

crit push [--dry-run] [pr-number] reads .crit.json and posts all
unresolved comments as a GitHub PR review. --dry-run previews what
would be posted without calling GitHub. Requires gh CLI."
```

---

### Task 7: Write tests for github.go

**Files:**
- Create: `github_test.go`

Test the pure conversion functions (no `gh` CLI needed). The functions that shell out to `gh` are tested via integration/manual testing — unit tests cover the data transformation logic.

- [ ] **Step 1: Write tests for `ghCommentsToCritJSON`**

```go
package main

import (
	"testing"
)

func TestMergeGHComments_BasicConversion(t *testing.T) {
	comments := []ghComment{
		{
			ID:   1,
			Path: "main.go",
			Line: 10,
			Side: "RIGHT",
			Body: "Fix this bug",
			User: struct {
				Login string `json:"login"`
			}{Login: "reviewer1"},
			CreatedAt: "2025-01-01T00:00:00Z",
		},
		{
			ID:        2,
			Path:      "main.go",
			Line:      25,
			StartLine: 20,
			Side:      "RIGHT",
			Body:      "This whole block needs refactoring",
			User: struct {
				Login string `json:"login"`
			}{Login: "reviewer2"},
			CreatedAt: "2025-01-01T00:00:00Z",
		},
	}

	cj := CritJSON{Branch: "feature-branch", BaseRef: "abc123", ReviewRound: 1, Files: make(map[string]CritJSONFile)}
	mergeGHComments(&cj, comments)

	if cj.Branch != "feature-branch" {
		t.Errorf("Branch = %q, want %q", cj.Branch, "feature-branch")
	}
	if cj.BaseRef != "abc123" {
		t.Errorf("BaseRef = %q, want %q", cj.BaseRef, "abc123")
	}

	cf, ok := cj.Files["main.go"]
	if !ok {
		t.Fatal("expected main.go in files")
	}
	if len(cf.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(cf.Comments))
	}

	// Single-line comment: StartLine should equal Line
	c1 := cf.Comments[0]
	if c1.StartLine != 10 || c1.EndLine != 10 {
		t.Errorf("c1 lines = %d-%d, want 10-10", c1.StartLine, c1.EndLine)
	}
	if c1.Body != "Fix this bug" {
		t.Errorf("c1 body = %q, want %q", c1.Body, "Fix this bug")
	}
	if c1.Author != "reviewer1" {
		t.Errorf("c1 author = %q, want %q", c1.Author, "reviewer1")
	}

	// Multi-line comment: StartLine from GitHub
	c2 := cf.Comments[1]
	if c2.StartLine != 20 || c2.EndLine != 25 {
		t.Errorf("c2 lines = %d-%d, want 20-25", c2.StartLine, c2.EndLine)
	}
	if c2.Author != "reviewer2" {
		t.Errorf("c2 author = %q, want %q", c2.Author, "reviewer2")
	}
}

func TestMergeGHComments_FiltersLeftSide(t *testing.T) {
	comments := []ghComment{
		{ID: 1, Path: "old.go", Line: 5, Side: "LEFT", Body: "old code comment"},
		{ID: 2, Path: "new.go", Line: 5, Side: "RIGHT", Body: "new code comment"},
	}

	cj := CritJSON{Branch: "branch", BaseRef: "base", ReviewRound: 1, Files: make(map[string]CritJSONFile)}
	mergeGHComments(&cj, comments)

	if _, ok := cj.Files["old.go"]; ok {
		t.Error("LEFT-side comment should be filtered out")
	}
	if _, ok := cj.Files["new.go"]; !ok {
		t.Error("RIGHT-side comment should be included")
	}
}

func TestMergeGHComments_SkipsNoLineComments(t *testing.T) {
	comments := []ghComment{
		{ID: 1, Path: "main.go", Line: 0, Side: "RIGHT", Body: "PR-level comment"},
	}

	cj := CritJSON{Branch: "branch", BaseRef: "base", ReviewRound: 1, Files: make(map[string]CritJSONFile)}
	mergeGHComments(&cj, comments)

	if len(cj.Files) != 0 {
		t.Error("comments with Line=0 should be skipped")
	}
}

func TestCritJSONToGHComments_BasicConversion(t *testing.T) {
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"auth.go": {
				Comments: []Comment{
					{ID: "c1", StartLine: 10, EndLine: 10, Body: "single line"},
					{ID: "c2", StartLine: 20, EndLine: 25, Body: "multi line"},
					{ID: "c3", StartLine: 30, EndLine: 30, Body: "resolved", Resolved: true},
				},
			},
		},
	}

	comments := critJSONToGHComments(cj)

	if len(comments) != 2 {
		t.Fatalf("expected 2 comments (resolved filtered), got %d", len(comments))
	}

	c1 := comments[0]
	if c1["path"] != "auth.go" || c1["line"] != 10 || c1["side"] != "RIGHT" {
		t.Errorf("c1 = %v", c1)
	}
	// Single-line: no start_line field
	if _, ok := c1["start_line"]; ok {
		t.Error("single-line comment should not have start_line")
	}

	c2 := comments[1]
	if c2["start_line"] != 20 || c2["line"] != 25 {
		t.Errorf("c2 = %v", c2)
	}
}

func TestMergeGHComments_PreservesExistingComments(t *testing.T) {
	// Simulate an existing .crit.json with local comments
	cj := CritJSON{
		Branch: "feature", BaseRef: "abc", ReviewRound: 1,
		Files: map[string]CritJSONFile{
			"main.go": {
				Status: "modified",
				Comments: []Comment{
					{ID: "c1", StartLine: 5, EndLine: 5, Body: "existing local comment", CreatedAt: "2025-01-01T00:00:00Z"},
				},
			},
		},
	}

	// Pull adds a new GH comment to the same file
	ghComments := []ghComment{
		{ID: 100, Path: "main.go", Line: 20, Side: "RIGHT", Body: "new from GH",
			User: struct{ Login string `json:"login"` }{Login: "reviewer"},
			CreatedAt: "2025-01-02T00:00:00Z"},
	}

	added := mergeGHComments(&cj, ghComments)
	if added != 1 {
		t.Errorf("added = %d, want 1", added)
	}

	cf := cj.Files["main.go"]
	if len(cf.Comments) != 2 {
		t.Fatalf("expected 2 comments (1 existing + 1 new), got %d", len(cf.Comments))
	}
	// Existing comment preserved
	if cf.Comments[0].Body != "existing local comment" {
		t.Errorf("existing comment body = %q", cf.Comments[0].Body)
	}
	// New comment appended
	if cf.Comments[1].Body != "new from GH" {
		t.Errorf("new comment body = %q", cf.Comments[1].Body)
	}
	if cf.Comments[1].Author != "reviewer" {
		t.Errorf("new comment author = %q", cf.Comments[1].Author)
	}
	// ID should be c2 (next after existing c1)
	if cf.Comments[1].ID != "c2" {
		t.Errorf("new comment ID = %q, want c2", cf.Comments[1].ID)
	}
}

func TestCritJSONToGHComments_SkipsResolved(t *testing.T) {
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"main.go": {
				Comments: []Comment{
					{ID: "c1", StartLine: 1, EndLine: 1, Body: "done", Resolved: true},
				},
			},
		},
	}

	comments := critJSONToGHComments(cj)
	if len(comments) != 0 {
		t.Errorf("expected 0 comments, got %d", len(comments))
	}
}

func TestCritJSONToGHComments_BodyNotPrefixedWithAuthor(t *testing.T) {
	// When pushing, the body should be the raw comment text.
	// Author info is NOT prepended — GitHub shows the poster's username natively.
	cj := CritJSON{
		Files: map[string]CritJSONFile{
			"main.go": {
				Comments: []Comment{
					{ID: "c1", StartLine: 10, EndLine: 10, Body: "fix this", Author: "reviewer1"},
				},
			},
		},
	}

	comments := critJSONToGHComments(cj)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0]["body"] != "fix this" {
		t.Errorf("body = %q, want %q (should not include author prefix)", comments[0]["body"], "fix this")
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd /Users/tomasztomczyk/Server/side/crit-mono/crit && go test -run "TestMergeGH|TestCritJSON" -v`

Expected: All PASS

- [ ] **Step 3: Run full test suite**

Run: `cd /Users/tomasztomczyk/Server/side/crit-mono/crit && go test ./... -v`

Expected: All PASS

- [ ] **Step 4: Commit**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit
git add github_test.go
git commit -m "test: add unit tests for GitHub PR comment conversion

Tests mergeGHComments and critJSONToGHComments conversion logic:
- Basic single/multi-line comment mapping
- LEFT-side comment filtering
- PR-level (no line) comment filtering
- Resolved comment filtering on push"
```

---

## Chunk 4: `crit comment` — CLI for adding comments to .crit.json

### Task 8: Add `addCommentToCritJSON` function to github.go

**Files:**
- Modify: `github.go` (add the comment-appending logic)

This function reads an existing `.crit.json` (or creates one from scratch), appends a comment to the specified file, and writes it back. It handles ID generation, timestamp, and git metadata automatically.

- [ ] **Step 1: Add the function to github.go**

```go
// addCommentToCritJSON appends a comment to .crit.json for the given file and line range.
// Creates .crit.json if it doesn't exist. Appends to existing comments if it does.
func addCommentToCritJSON(filePath string, startLine, endLine int, body string) error {
	root, err := RepoRoot()
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}

	critPath := root + "/.crit.json"

	// Load existing or create new
	var cj CritJSON
	if data, err := os.ReadFile(critPath); err == nil {
		if err := json.Unmarshal(data, &cj); err != nil {
			return fmt.Errorf("invalid existing .crit.json: %w", err)
		}
	} else {
		branch := CurrentBranch()
		baseRef, _ := MergeBase(DefaultBranch())
		cj = CritJSON{
			Branch:      branch,
			BaseRef:     baseRef,
			ReviewRound: 1,
			Files:       make(map[string]CritJSONFile),
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	cj.UpdatedAt = now

	// Get or create the file entry
	cf, ok := cj.Files[filePath]
	if !ok {
		cf = CritJSONFile{
			Status:   "modified",
			Comments: []Comment{},
		}
	}

	// Generate next ID
	nextID := 1
	for _, c := range cf.Comments {
		id := 0
		_, _ = fmt.Sscanf(c.ID, "c%d", &id)
		if id >= nextID {
			nextID = id + 1
		}
	}

	cf.Comments = append(cf.Comments, Comment{
		ID:        fmt.Sprintf("c%d", nextID),
		StartLine: startLine,
		EndLine:   endLine,
		Body:      body,
		CreatedAt: now,
		UpdatedAt: now,
	})
	cj.Files[filePath] = cf

	data, err := json.MarshalIndent(cj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling .crit.json: %w", err)
	}
	return os.WriteFile(critPath, data, 0644)
}

// clearCritJSON removes .crit.json from the repo root.
func clearCritJSON() error {
	root, err := RepoRoot()
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}
	critPath := root + "/.crit.json"
	if err := os.Remove(critPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
```

- [ ] **Step 2: Run `gofmt`**

Run: `cd /Users/tomasztomczyk/Server/side/crit-mono/crit && gofmt -w github.go`

- [ ] **Step 3: Commit**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit
git add github.go
git commit -m "feat: add addCommentToCritJSON and clearCritJSON helpers

Functions for appending comments to .crit.json via CLI. Creates the
file if it doesn't exist, appends to existing comments if it does.
Auto-generates comment IDs and timestamps."
```

---

### Task 9: Wire up `crit comment` subcommand in main.go

**Files:**
- Modify: `main.go` (add subcommand dispatch)

The syntax:
```
crit comment <path>:<line> <body>           # single line
crit comment <path>:<start>-<end> <body>    # line range
crit comment --clear                        # remove all comments
```

The path is relative to repo root. The body is everything after the location arg, joined by spaces (so no quoting needed for simple comments, but quoting works too).

- [ ] **Step 1: Add `crit comment` handler in main.go**

Add this block after the `crit push` handler, before the `flag.Int` line:

```go
// Handle "crit comment <path>:<line[-end]> <body>" subcommand
if len(os.Args) >= 2 && os.Args[1] == "comment" {
	// Handle --clear flag
	if len(os.Args) >= 3 && os.Args[2] == "--clear" {
		if err := clearCritJSON(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Cleared .crit.json")
		os.Exit(0)
	}

	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "Usage: crit comment <path>:<line[-end]> <body>")
		fmt.Fprintln(os.Stderr, "       crit comment --clear")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  crit comment main.go:42 'Fix this bug'")
		fmt.Fprintln(os.Stderr, "  crit comment src/auth.go:10-25 'This block needs refactoring'")
		os.Exit(1)
	}

	// Parse <path>:<line[-end]>
	loc := os.Args[2]
	colonIdx := strings.LastIndex(loc, ":")
	if colonIdx < 0 {
		fmt.Fprintf(os.Stderr, "Error: invalid location %q — expected <path>:<line[-end]>\n", loc)
		os.Exit(1)
	}
	filePath := loc[:colonIdx]
	lineSpec := loc[colonIdx+1:]

	var startLine, endLine int
	if dashIdx := strings.Index(lineSpec, "-"); dashIdx >= 0 {
		s, err := strconv.Atoi(lineSpec[:dashIdx])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid start line in %q\n", loc)
			os.Exit(1)
		}
		e, err := strconv.Atoi(lineSpec[dashIdx+1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid end line in %q\n", loc)
			os.Exit(1)
		}
		startLine, endLine = s, e
	} else {
		n, err := strconv.Atoi(lineSpec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid line number in %q\n", loc)
			os.Exit(1)
		}
		startLine, endLine = n, n
	}

	// Body is all remaining args joined
	body := strings.Join(os.Args[3:], " ")

	if err := addCommentToCritJSON(filePath, startLine, endLine, body); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Added comment on %s:%s\n", filePath, lineSpec)
	os.Exit(0)
}
```

- [ ] **Step 2: Update `printHelp()` to include `comment`**

Add to the Usage section:

```
crit comment <path>:<line> <body>  Add a review comment to .crit.json
crit comment --clear               Remove all comments from .crit.json
```

- [ ] **Step 3: Run tests and linters**

Run: `cd /Users/tomasztomczyk/Server/side/crit-mono/crit && go build . && gofmt -l . && golangci-lint run ./...`

Expected: Builds clean, no lint errors

- [ ] **Step 4: Commit**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit
git add main.go
git commit -m "feat: add 'crit comment' subcommand for programmatic comments

crit comment <path>:<line[-end]> <body> appends an inline comment
to .crit.json without needing to construct JSON manually. Handles
ID generation, timestamps, and git metadata automatically.

crit comment --clear removes .crit.json entirely."
```

---

### Task 10: Write tests for `addCommentToCritJSON`

**Files:**
- Modify: `github_test.go`

- [ ] **Step 1: Write the tests**

Add to `github_test.go`:

```go
func TestAddCommentToCritJSON_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()

	// Temporarily override RepoRoot — addCommentToCritJSON calls RepoRoot()
	// so we need to run this in a real git repo. Set up a temp git repo.
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	// Change to temp dir so RepoRoot() finds it
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	err := addCommentToCritJSON("main.go", 10, 15, "Fix this bug")
	if err != nil {
		t.Fatalf("addCommentToCritJSON: %v", err)
	}

	// Read and verify
	data, err := os.ReadFile(dir + "/.crit.json")
	if err != nil {
		t.Fatalf("read .crit.json: %v", err)
	}
	var cj CritJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	cf, ok := cj.Files["main.go"]
	if !ok {
		t.Fatal("expected main.go in files")
	}
	if len(cf.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(cf.Comments))
	}
	if cf.Comments[0].ID != "c1" {
		t.Errorf("ID = %q, want c1", cf.Comments[0].ID)
	}
	if cf.Comments[0].StartLine != 10 || cf.Comments[0].EndLine != 15 {
		t.Errorf("lines = %d-%d, want 10-15", cf.Comments[0].StartLine, cf.Comments[0].EndLine)
	}
	if cf.Comments[0].Body != "Fix this bug" {
		t.Errorf("Body = %q", cf.Comments[0].Body)
	}
}

func TestAddCommentToCritJSON_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	// Add two comments
	if err := addCommentToCritJSON("main.go", 1, 1, "First"); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := addCommentToCritJSON("main.go", 20, 20, "Second"); err != nil {
		t.Fatalf("second add: %v", err)
	}

	data, _ := os.ReadFile(dir + "/.crit.json")
	var cj CritJSON
	json.Unmarshal(data, &cj)

	cf := cj.Files["main.go"]
	if len(cf.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(cf.Comments))
	}
	if cf.Comments[0].ID != "c1" || cf.Comments[1].ID != "c2" {
		t.Errorf("IDs = %q, %q — want c1, c2", cf.Comments[0].ID, cf.Comments[1].ID)
	}
}

func TestAddCommentToCritJSON_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	if err := exec.Command("git", "init", dir).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	addCommentToCritJSON("main.go", 1, 1, "Comment on main")
	addCommentToCritJSON("auth.go", 5, 10, "Comment on auth")

	data, _ := os.ReadFile(dir + "/.crit.json")
	var cj CritJSON
	json.Unmarshal(data, &cj)

	if len(cj.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(cj.Files))
	}
	if _, ok := cj.Files["main.go"]; !ok {
		t.Error("missing main.go")
	}
	if _, ok := cj.Files["auth.go"]; !ok {
		t.Error("missing auth.go")
	}
}
```

- [ ] **Step 2: Add `os/exec` to the test imports if not already present**

The test file needs `"os/exec"` for `exec.Command("git", "init", ...)`.

- [ ] **Step 3: Run tests**

Run: `cd /Users/tomasztomczyk/Server/side/crit-mono/crit && go test -run "TestAddComment" -v`

Expected: All PASS

- [ ] **Step 4: Commit**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit
git add github_test.go
git commit -m "test: add unit tests for addCommentToCritJSON

Tests creating new .crit.json, appending to existing, and
multiple files. Uses temp git repos for RepoRoot() detection."
```

---

## Chunk 5: Crit comment skill for AI agents

### Task 11: Add crit-comment instructions to each integration

**Files:**
- Create: `integrations/claude-code/crit-comment.md` (skill with YAML frontmatter)
- Create: `integrations/cursor/crit-comment.md` (plain markdown)
- Create: `integrations/cline/crit-comment.md` (plain markdown)
- Create: `integrations/aider/crit-comment.md` (plain markdown)
- Create: `integrations/windsurf/crit-comment.md` (plain markdown)
- Create: `integrations/github-copilot/crit-comment.md` (plain markdown)

Each integration gets its own copy of the crit-comment instructions, following that integration's existing format conventions. The content is the same — just the comment API, not a full review workflow. Users compose this with their own review skills: "review this code and leave comments using crit comment."

- [ ] **Step 1: Write the Claude Code skill file (with YAML frontmatter)**

Create `integrations/claude-code/crit-comment.md`:

```markdown
---
description: "Leave inline review comments on code using crit comment CLI"
allowed-tools: Bash(crit:*), Read
---

# Leaving Comments with Crit

Use `crit comment` to add inline review comments to `.crit.json`. Comments are displayed in crit's browser UI for interactive review.

## Syntax

```bash
# Single line comment
crit comment <path>:<line> '<body>'

# Multi-line comment (range)
crit comment <path>:<start>-<end> '<body>'
```

## Examples

```bash
crit comment src/auth.go:42 'Missing null check on user.session — will panic if session expired'
crit comment src/handler.go:15-28 'This error is swallowed silently. The catch block returns ok but the caller expects an error on failure.'
crit comment src/db.go:103 'Consider using a prepared statement here to avoid SQL injection'
```

## Rules

- **Paths** are relative to repo root
- **Line numbers** reference the file as it exists on disk (1-indexed), not diff line numbers
- **Body** is everything after the location argument — use single quotes to avoid shell interpretation
- **Comments are appended** — calling `crit comment` multiple times adds to the list, never replaces
- **No setup needed** — `crit comment` creates `.crit.json` automatically if it doesn't exist

## After commenting

Once all comments are posted, launch crit so the user can browse them:

```bash
crit
```

Tell the user: **"Review comments are ready in crit. Browse them, then click 'Finish Review' when done."**
```

- [ ] **Step 2: Write the plain markdown version for other integrations**

Create the same content without YAML frontmatter for `integrations/cursor/crit-comment.md`, `integrations/cline/crit-comment.md`, `integrations/aider/crit-comment.md`, `integrations/windsurf/crit-comment.md`, and `integrations/github-copilot/crit-comment.md`. Same content, just drop the `---` frontmatter block:

```markdown
# Leaving Comments with Crit

Use `crit comment` to add inline review comments to `.crit.json`. Comments are displayed in crit's browser UI for interactive review.

[... same Syntax, Examples, Rules, After commenting sections as above ...]
```

- [ ] **Step 3: Verify the Claude Code skill file has valid YAML frontmatter**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit && head -4 integrations/claude-code/crit-comment.md
```

Expected: Shows `---`, `description:`, `allowed-tools:`, `---`

- [ ] **Step 4: Commit**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit
git add integrations/*/crit-comment.md
git commit -m "feat: add crit-comment instructions to all integrations

Teaches AI agents how to leave inline review comments via 'crit comment'
CLI. Claude Code version includes YAML frontmatter (skill format),
others use plain markdown. Composable with any review workflow."
```

---

## Chunk 6: Author display and color-coding in frontend

### Task 12: Add `author` field to Comment struct

**Files:**
- Modify: `session.go`

- [ ] **Step 1: Add Author field to Comment struct**

In `session.go`, add `Author` after `Body` in the `Comment` struct:

```go
type Comment struct {
	ID              string `json:"id"`
	StartLine       int    `json:"start_line"`
	EndLine         int    `json:"end_line"`
	Side            string `json:"side,omitempty"`
	Body            string `json:"body"`
	Author          string `json:"author,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	Resolved        bool   `json:"resolved,omitempty"`
	ResolutionNote  string `json:"resolution_note,omitempty"`
	ResolutionLines any    `json:"resolution_lines,omitempty"`
	CarriedForward  bool   `json:"carried_forward,omitempty"`
}
```

This is backwards-compatible — existing `.crit.json` files without `author` will unmarshal with an empty string and omit the field when marshaling.

- [ ] **Step 2: Commit**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit
git add session.go
git commit -m "feat: add optional author field to Comment struct

Backwards-compatible addition — existing .crit.json files without
author will work as before. Used by crit pull to store GitHub usernames."
```

---

### Task 13: Display author name and color-code comments in frontend

**Files:**
- Modify: `frontend/app.js` — author badge rendering, color assignment
- Modify: `frontend/style.css` — author badge styles

**Design:**
- When a comment has an `author` field, show an author badge/pill in the comment header (before the line reference)
- Assign each unique author a deterministic color from a small palette using a string hash
- Colors should work in both light and dark themes (use semi-transparent backgrounds with solid text)
- No author = no badge (backwards-compatible with existing comments)

- [ ] **Step 1: Add author color utility and update comment rendering in app.js**

Add a color palette and hash function near the top of app.js (after the state variables):

```javascript
// Author color-coding for multi-reviewer comments
const AUTHOR_COLORS = [
  { bg: 'rgba(74, 144, 217, 0.15)', border: 'rgba(74, 144, 217, 0.4)', text: '#4a90d9' },
  { bg: 'rgba(217, 74, 74, 0.15)', border: 'rgba(217, 74, 74, 0.4)', text: '#d94a4a' },
  { bg: 'rgba(74, 180, 100, 0.15)', border: 'rgba(74, 180, 100, 0.4)', text: '#4ab464' },
  { bg: 'rgba(217, 166, 74, 0.15)', border: 'rgba(217, 166, 74, 0.4)', text: '#d9a64a' },
  { bg: 'rgba(155, 74, 217, 0.15)', border: 'rgba(155, 74, 217, 0.4)', text: '#9b4ad9' },
  { bg: 'rgba(74, 195, 195, 0.15)', border: 'rgba(74, 195, 195, 0.4)', text: '#4ac3c3' },
];

function authorColor(name) {
  let hash = 0;
  for (const ch of name) hash = ((hash << 5) - hash + ch.charCodeAt(0)) | 0;
  return AUTHOR_COLORS[Math.abs(hash) % AUTHOR_COLORS.length];
}
```

Then in the comment rendering function (where `headerLeft` is built), add the author badge before the line reference:

```javascript
if (comment.author) {
  const authorBadge = document.createElement('span');
  authorBadge.className = 'comment-author-badge';
  const colors = authorColor(comment.author);
  authorBadge.style.cssText = `background:${colors.bg};border-color:${colors.border};color:${colors.text}`;
  authorBadge.textContent = '@' + comment.author;
  headerLeft.appendChild(authorBadge);
}
headerLeft.appendChild(lineRef);
```

The author badge goes before `lineRef` in `headerLeft` so the visual order is: `@username | Line 42 | 2m ago`.

- [ ] **Step 2: Add author badge styles to style.css**

```css
.comment-author-badge {
  font-size: 11px;
  font-weight: 600;
  padding: 1px 7px;
  border-radius: 10px;
  border: 1px solid;
  white-space: nowrap;
}
```

- [ ] **Step 3: Commit**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit
git add frontend/app.js frontend/style.css
git commit -m "feat: display author name with color-coded badges on comments

When a comment has an author field (set by crit pull from GitHub),
show a colored @username badge in the comment header. Each unique
author gets a deterministic color from a 6-color palette. Comments
without an author (locally created) show no badge."
```

---

## Chunk 7: E2E tests for multi-author comment rendering

### Task 14: Add Playwright tests for author badges

**Files:**
- Modify: `e2e/tests/comments.spec.ts` (add author badge tests to existing comment tests)

Tests use the existing git-mode fixture. We add comments with `author` field via the API, then verify the frontend renders author badges with correct text and color-coding.

- [ ] **Step 1: Add author badge tests**

Add to `comments.spec.ts`:

```typescript
test('displays author badge when comment has author field', async ({ page, request }) => {
  // Add a comment with author via API
  const resp = await request.post(`/api/file/comments?path=${testFilePath}`, {
    data: { start_line: 1, end_line: 1, body: 'Test comment', author: 'reviewer1' }
  });
  expect(resp.ok()).toBeTruthy();

  await loadPage(page);
  // Verify author badge appears
  const badge = page.locator('.comment-author-badge');
  await expect(badge).toBeVisible();
  await expect(badge).toHaveText('@reviewer1');
});

test('does not display author badge when comment has no author', async ({ page, request }) => {
  const resp = await request.post(`/api/file/comments?path=${testFilePath}`, {
    data: { start_line: 1, end_line: 1, body: 'Local comment' }
  });
  expect(resp.ok()).toBeTruthy();

  await loadPage(page);
  await expect(page.locator('.comment-author-badge')).toHaveCount(0);
});

test('color-codes different authors distinctly', async ({ page, request }) => {
  // Add comments from two different authors
  await request.post(`/api/file/comments?path=${testFilePath}`, {
    data: { start_line: 1, end_line: 1, body: 'Comment A', author: 'alice' }
  });
  await request.post(`/api/file/comments?path=${testFilePath}`, {
    data: { start_line: 2, end_line: 2, body: 'Comment B', author: 'bob' }
  });

  await loadPage(page);
  const badges = page.locator('.comment-author-badge');
  await expect(badges).toHaveCount(2);

  // Same author should get same color, different authors may get different colors
  // Just verify both have inline styles (color assignment works)
  await expect(badges.nth(0)).toHaveAttribute('style', /background:/);
  await expect(badges.nth(1)).toHaveAttribute('style', /background:/);
});
```

**Note:** The `author` field must be accepted by the `POST /api/file/comments` endpoint. This requires the backend to pass through the `author` field from the request body to the Comment struct. Add this to Task 12 if not already handled — the server's `addComment` handler in `server.go` unmarshals the request body into a struct, so the field will flow through automatically since Comment already has `Author`.

- [ ] **Step 2: Run E2E tests**

Run: `cd /Users/tomasztomczyk/Server/side/crit-mono/crit/e2e && npx playwright test tests/comments.spec.ts`

Expected: All pass including new author tests

- [ ] **Step 3: Commit**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit
git add e2e/tests/comments.spec.ts
git commit -m "test: add E2E tests for multi-author comment rendering

Tests author badge visibility, absence when no author, and
color-coding for different authors."
```

---

## Chunk 8: README updates

### Task 15: Update README with new features and gh dependency

**Files:**
- Modify: `README.md`

Document the new subcommands (`crit pull`, `crit push`, `crit comment`), the `gh` CLI dependency for pull/push, and the author display feature.

- [ ] **Step 1: Add new subcommands to CLI reference in README**

Add to the CLI usage section:

```
crit comment <path>:<line[-end]> <body>    Add a review comment to .crit.json
crit comment --clear                       Remove all comments from .crit.json
crit pull [pr-number]                      Fetch GitHub PR review comments to .crit.json
crit push [--dry-run] [pr-number]          Post .crit.json comments to a GitHub PR
```

- [ ] **Step 2: Add GitHub sync section**

Add a section explaining the GitHub sync workflow:

```markdown
## GitHub PR Sync

Crit can sync review comments bidirectionally with GitHub PRs. Requires the [GitHub CLI](https://cli.github.com) (`gh`) to be installed and authenticated.

### Pull comments from a PR

```bash
crit pull              # auto-detects PR from current branch
crit pull 42           # explicit PR number
```

### Push comments to a PR

```bash
crit push              # auto-detects PR from current branch
crit push --dry-run    # preview without posting
crit push 42           # explicit PR number
```

### Adding comments programmatically

```bash
crit comment src/auth.go:42 'Missing null check'
crit comment src/handler.go:15-28 'Error handling issue'
```
```

- [ ] **Step 3: Commit**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit
git add README.md
git commit -m "docs: add GitHub sync and crit comment to README

Documents crit pull/push/comment subcommands, gh CLI dependency,
and the GitHub PR sync workflow."
```

---

## Future: E2E test for pre-loaded .crit.json

An E2E test for pre-loaded `.crit.json` (Chunk 1) would need its own Playwright project with a dedicated port and fixture, since adding `.crit.json` to the shared git fixture pollutes other tests. The unit tests cover the `loadCritJSON` behavior. A dedicated E2E project can be added later if needed.

## Future: LEFT-side comment support

GitHub PR comments on deleted/old lines (`side: "LEFT"`) are currently skipped by `crit pull`. A future enhancement could map these to the nearest surviving line or display them as file-level annotations.

---

## Summary

| Task | What | Risk |
|------|------|------|
| 1-2 | Failing tests for hashless/mismatched loading | None — tests only |
| 3 | Remove hash check in `loadCritJSON` | Low — strictly more permissive, existing tests still pass |
| 4 | Create `github.go` with PR comment helpers | None — new file |
| 5 | Wire `crit pull` subcommand | Low — new subcommand, no existing behavior changes |
| 6 | Wire `crit push` subcommand (with `--dry-run`) | Low — new subcommand, no existing behavior changes |
| 7 | Unit tests for GitHub conversion functions | None — tests only |
| 8 | Add `addCommentToCritJSON` / `clearCritJSON` to github.go | None — new functions |
| 9 | Wire `crit comment` subcommand | Low — new subcommand, no existing behavior changes |
| 10 | Unit tests for `addCommentToCritJSON` | None — tests only |
| 11 | Crit comment instructions for all integrations | None — new files, no code changes |
| 12 | Add `author` field to Comment struct | Low — backwards-compatible `omitempty` addition |
| 13 | Author display + color-coding in frontend | Low — additive UI, no existing behavior changes |
| 14 | E2E tests for multi-author comment rendering | None — tests only |
| 15 | README updates for new features | None — docs only |

**Execution order:** 1 → 2 → 3 → 4 → 5 → 7 → 8 → 9 → 10 → 12 → 13 → 6 → 11 → 14 → 15

**Dogfooding:** After `crit comment` (Task 9) is built, create a draft PR for this branch. Use `crit comment` to add test comments locally, then test `crit push` (Task 6) by pushing them to the PR. Test `crit pull` by adding comments in GitHub's UI and pulling them down.

**Dependencies:** `gh` CLI must be installed and authenticated (for `pull`/`push` only). `crit comment` has no external dependencies.

**CLI surface after this plan:**
```
crit                                       Auto-detect changed files via git
crit <file|dir> [...]                      Review specific files or directories
crit go [port]                             Signal round-complete to a running crit instance
crit comment <path>:<line[-end]> <body>    Add a review comment to .crit.json
crit comment --clear                       Remove all comments from .crit.json
crit pull [pr-number]                      Fetch GitHub PR review comments to .crit.json
crit push [--dry-run] [pr-number]          Post .crit.json comments to a GitHub PR
crit install <agent>                       Install integration files for an AI coding tool
crit help                                  Show this help message
```

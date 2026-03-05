# Server-side Syntax Highlighting with Chroma

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace frontend highlight.js with server-side Chroma (Go) for GitHub-quality syntax highlighting in diff views.

**Architecture:** The Go server uses Chroma to highlight full file contents into per-line HTML arrays. Diff lines (`DiffLine`) gain an `HTML` field with pre-highlighted markup. The file API returns `highlighted_lines` for spacer expansion. The frontend removes hljs-based highlighting for diffs and reads pre-rendered HTML from the API. hljs is kept only for markdown fenced code blocks (markdown-it `highlight` callback).

**Tech Stack:** Go (Chroma v2), vanilla JS frontend, CSS (Chroma GitHub theme classes replace hljs classes)

---

### Task 1: Add Chroma dependency and create highlight.go

**Files:**
- Modify: `go.mod`
- Create: `highlight.go`
- Create: `highlight_test.go`

**Step 1: Add the Chroma dependency**

```bash
cd /Users/tomasztomczyk/Server/side/crit-mono/crit/.worktrees/syntax-highlight
go get github.com/alecthomas/chroma/v2
```

**Step 2: Write tests for the highlighter**

Create `highlight_test.go`:

```go
package main

import (
	"testing"
)

func TestHighlightLines_Go(t *testing.T) {
	content := "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	lines := HighlightLines(content, "main.go")
	if lines == nil {
		t.Fatal("expected non-nil lines for Go file")
	}
	// 1-indexed: lines[0] is nil, lines[1] = first line
	if lines[0] != nil {
		t.Error("lines[0] should be nil (1-indexed)")
	}
	if len(lines) < 6 {
		t.Fatalf("expected at least 6 entries (nil + 5 lines), got %d", len(lines))
	}
	// First line should contain highlighted spans
	if lines[1] == nil || *lines[1] == "package main" {
		t.Error("expected highlighted HTML for line 1, got plain text or nil")
	}
	// Check it contains span tags (Chroma output)
	if lines[1] != nil && !containsSpan(*lines[1]) {
		t.Errorf("line 1 should contain <span> tags, got: %s", *lines[1])
	}
}

func TestHighlightLines_Elixir(t *testing.T) {
	content := "defmodule Foo do\n  def bar, do: :ok\nend\n"
	lines := HighlightLines(content, "lib/foo.ex")
	if lines == nil {
		t.Fatal("expected non-nil lines for Elixir file")
	}
	// Should highlight atoms, keywords differently
	if lines[2] == nil {
		t.Fatal("expected highlighted line 2")
	}
	if !containsSpan(*lines[2]) {
		t.Errorf("line 2 should contain <span> tags, got: %s", *lines[2])
	}
}

func TestHighlightLines_UnknownExtension(t *testing.T) {
	content := "some random content\n"
	lines := HighlightLines(content, "file.xyz123")
	if lines != nil {
		t.Error("expected nil for unknown file type")
	}
}

func TestHighlightLines_EmptyContent(t *testing.T) {
	lines := HighlightLines("", "main.go")
	if lines != nil {
		t.Error("expected nil for empty content")
	}
}

func TestHighlightLine_SingleLine(t *testing.T) {
	html := HighlightLine("fmt.Println(\"hello\")", "main.go")
	if html == "fmt.Println(\"hello\")" {
		t.Error("expected highlighted HTML, got plain text")
	}
}

func containsSpan(s string) bool {
	return len(s) > 0 && (len(s) != len(stripTags(s)))
}

func stripTags(s string) string {
	result := ""
	inTag := false
	for _, c := range s {
		if c == '<' {
			inTag = true
		} else if c == '>' {
			inTag = false
		} else if !inTag {
			result += string(c)
		}
	}
	return result
}
```

**Step 3: Run tests to verify they fail**

```bash
go test -run TestHighlight -v
```

Expected: compilation error — `HighlightLines` and `HighlightLine` not defined.

**Step 4: Implement highlight.go**

Create `highlight.go`:

```go
package main

import (
	"html"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// chromaFormatter is a shared HTML formatter configured for inline CSS-class output.
var chromaFormatter = chromahtml.New(
	chromahtml.WithClasses(true),
	chromahtml.PreventSurroundingPre(true),
)

// chromaStyle is the highlighting style (we use CSS classes, so the style choice
// only matters for the generated stylesheet — token colors come from our CSS).
var chromaStyle = styles.Get("github-dark")

// HighlightLines highlights the full file content and returns a 1-indexed slice
// of per-line HTML strings. Returns nil if the file type is not recognized or content is empty.
// result[0] = nil (unused), result[1] = highlighted HTML for line 1, etc.
func HighlightLines(content, filename string) []*string {
	if content == "" {
		return nil
	}

	lexer := lexers.Match(filename)
	if lexer == nil {
		return nil
	}
	lexer = chroma.Coalesce(lexer)

	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return nil
	}

	var buf strings.Builder
	if err := chromaFormatter.Format(&buf, chromaStyle, iterator); err != nil {
		return nil
	}

	highlighted := buf.String()
	rawLines := strings.Split(highlighted, "\n")

	// Build 1-indexed result
	result := make([]*string, len(rawLines)+1)
	for i, line := range rawLines {
		l := line
		result[i+1] = &l
	}

	return result
}

// HighlightLine highlights a single line of code. Used as a fallback for
// old-side diff lines not in the current file content.
// Returns HTML-escaped content if the language is not recognized.
func HighlightLine(content, filename string) string {
	lexer := lexers.Match(filename)
	if lexer == nil {
		return html.EscapeString(content)
	}
	lexer = chroma.Coalesce(lexer)

	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return html.EscapeString(content)
	}

	var buf strings.Builder
	if err := chromaFormatter.Format(&buf, chromaStyle, iterator); err != nil {
		return html.EscapeString(content)
	}

	return buf.String()
}
```

**Step 5: Run tests to verify they pass**

```bash
go test -run TestHighlight -v
```

Expected: all PASS.

**Step 6: Commit**

```bash
git add go.mod go.sum highlight.go highlight_test.go
git commit -m "feat: add Chroma-based syntax highlighting module"
```

---

### Task 2: Add HTML field to DiffLine and populate it server-side

**Files:**
- Modify: `git.go:29-34` (DiffLine struct)
- Modify: `session.go` (file loading, snapshot methods)

**Step 1: Add HTML field to DiffLine**

In `git.go`, add `HTML` field to `DiffLine`:

```go
type DiffLine struct {
	Type    string `json:"type"`              // "context", "add", "del"
	Content string `json:"content"`
	OldNum  int    `json:"old_num,omitempty"`  // 0 if add
	NewNum  int    `json:"new_num,omitempty"`  // 0 if del
	HTML    string `json:"html,omitempty"`     // pre-highlighted HTML from Chroma
}
```

Note: The JSON tags need to match what the frontend expects. Check the existing tags — currently DiffLine has no JSON tags since it's serialized as part of the hunk map. The `json:"-"` on `DiffHunks` in FileEntry means the hunks are manually serialized via `GetFileDiffSnapshot`. The DiffHunk/DiffLine structs are serialized directly via `writeJSON`. Check if they already have JSON tags — they don't currently. Add them.

Actually, looking at `git.go:19-34`, the structs have no JSON tags. But the frontend reads `line.Type`, `line.Content`, `line.OldNum`, `line.NewNum` — this means Go's default JSON encoding uses the field names as-is (PascalCase). The frontend uses PascalCase: `line.Type`, `line.Content`, `line.OldNum`, `line.NewNum`. So the new field will be `line.HTML` in JSON by default. That works.

**Step 2: Create a helper to attach highlighting to diff hunks**

Add to `highlight.go`:

```go
// HighlightDiffHunks attaches pre-highlighted HTML to each DiffLine in the hunks.
// highlightedLines is a 1-indexed array from HighlightLines (for the new-side file content).
// filename is used for per-line fallback highlighting of old-side lines.
func HighlightDiffHunks(hunks []DiffHunk, highlightedLines []*string, filename string) {
	for i := range hunks {
		for j := range hunks[i].Lines {
			line := &hunks[i].Lines[j]
			switch line.Type {
			case "add", "context":
				// Use pre-highlighted cache for new-side lines
				if highlightedLines != nil && line.NewNum > 0 && line.NewNum < len(highlightedLines) && highlightedLines[line.NewNum] != nil {
					line.HTML = *highlightedLines[line.NewNum]
				} else {
					line.HTML = HighlightLine(line.Content, filename)
				}
			case "del":
				// Old-side lines: highlight individually (not in current file content)
				line.HTML = HighlightLine(line.Content, filename)
			}
		}
	}
}
```

**Step 3: Call highlighting when loading files in session.go**

In `session.go`, after diff hunks are computed for code files, call `HighlightDiffHunks`. Find the places where `DiffHunks` is assigned:

1. Around line 165: `fe.DiffHunks = FileDiffUnifiedNewFile(fe.Content)` — new files
2. Around line 171: `fe.DiffHunks = hunks` — existing files with diffs
3. Around line 285: `fe.DiffHunks = hunks` — round-complete reload
4. Around line 785: `r.entry.DiffHunks = r.hunks` — scoped diff reload

After each assignment, add:
```go
hlLines := HighlightLines(fe.Content, fe.Path)
HighlightDiffHunks(fe.DiffHunks, hlLines, fe.Path)
```

Also add `HighlightedLines []*string` to `FileEntry` and store it for spacer expansion:
```go
fe.HighlightedLines = hlLines
```

**Step 4: Include highlighted_lines in GetFileSnapshot**

In `session.go` `GetFileSnapshot` (around line 1217), add:
```go
return map[string]any{
    "path":              f.Path,
    "status":            f.Status,
    "file_type":         f.FileType,
    "content":           f.Content,
    "highlighted_lines": f.HighlightedLines,
}, true
```

**Step 5: Run all Go tests**

```bash
go test ./... -v
```

Expected: all PASS (existing tests should still work since HTML is additive).

**Step 6: Commit**

```bash
git add git.go session.go highlight.go
git commit -m "feat: populate highlighted HTML on diff lines and file snapshots"
```

---

### Task 3: Generate Chroma CSS theme and add to theme.css

**Files:**
- Modify: `frontend/theme.css`

**Step 1: Generate Chroma CSS for dark and light themes**

Write a small Go program or use `chroma` CLI to generate CSS classes. Alternatively, write the CSS by hand based on Chroma's GitHub Dark and GitHub Light styles. The key Chroma/Pygments CSS classes are:

- `.chroma` — container
- `.kr` / `.k` / `.kd` / `.kn` / `.kp` — keywords (different subtypes!)
- `.nf` / `.nb` / `.nc` / `.nn` / `.no` — names (function, builtin, class, namespace, constant)
- `.s` / `.s1` / `.s2` / `.sa` / `.ss` — strings, atoms/symbols
- `.c` / `.c1` / `.cm` — comments
- `.m` / `.mi` / `.mf` — numbers
- `.o` / `.ow` — operators
- `.p` — punctuation
- `.err` — errors

This gives much finer granularity than hljs's `.hljs-keyword`, `.hljs-string`, etc.

**Step 2: Replace hljs theme blocks in theme.css**

Remove all the `.hljs*` rules (lines ~211-279 in theme.css). Replace with Chroma classes for:
- Default (dark, system-dark)
- `[data-theme="dark"]`
- `@media (prefers-color-scheme: light) html:not([data-theme])`
- `[data-theme="light"]`

Use `github-dark` colors for dark mode and `github` colors for light mode from Chroma's built-in styles.

Keep a minimal `.hljs` base rule for markdown code blocks (which still use hljs).

**Step 3: Verify build still works**

```bash
go build -o crit .
```

**Step 4: Commit**

```bash
git add frontend/theme.css
git commit -m "feat: replace hljs theme CSS with Chroma CSS classes"
```

---

### Task 4: Update frontend to use server-side highlighting for diffs

**Files:**
- Modify: `frontend/app.js`

**Step 1: Store highlighted_lines from file API response**

In the file loading code (around line 80-130 in app.js), after fetching file data, store `highlighted_lines`:

```javascript
f.highlightedLines = fileData.highlighted_lines || null;
```

Remove the `preHighlightFile` call (line 109):
```javascript
// REMOVE: f.highlightCache = preHighlightFile(f);
```

**Step 2: Update unified diff renderer to use line.HTML**

In `renderDiffUnified` (around line 2215), change:
```javascript
// OLD:
const hlLine = highlightDiffLine(line.Content, line.Type === 'del' ? line.OldNum : line.NewNum, line.Type === 'del' ? 'old' : '', file.highlightCache, file.lang);
contentEl.innerHTML = hlLine;

// NEW:
contentEl.innerHTML = line.HTML || escapeHtml(line.Content);
```

**Step 3: Update split diff renderer to use line.HTML**

In `makeSplitRow` (around lines 2356 and 2394), change:
```javascript
// OLD (left side, ~2356):
leftContent.innerHTML = highlightDiffLine(left.content, left.num, 'old', file.highlightCache, file.lang);

// NEW:
leftContent.innerHTML = left.html || escapeHtml(left.content);

// OLD (right side, ~2394):
rightContent.innerHTML = highlightDiffLine(right.content, right.num, right.type === 'del' ? 'old' : '', file.highlightCache, file.lang);

// NEW:
rightContent.innerHTML = right.html || escapeHtml(right.content);
```

Note: the split diff renderer passes `{ num, content, type }` objects to `makeSplitRow`. These are built from `line` objects. Update the call sites to also pass `html`:
```javascript
// Around line 2279 (context):
{ num: line.OldNum, content: line.Content, type: 'context', html: line.HTML },
{ num: line.NewNum, content: line.Content, type: 'context', html: line.HTML },

// Around line 2305 (del/add):
del ? { num: del.OldNum, content: del.Content, type: 'del', html: del.HTML } : null,
add ? { num: add.NewNum, content: add.Content, type: 'add', html: add.HTML } : null,
```

**Step 4: Update spacer expansion to include HTML**

In `renderDiffSpacer` (around line 2078), the spacer creates context lines from `file.content`. Update to include highlighted HTML from `file.highlightedLines`:

```javascript
// OLD:
contextLines.push({ Type: 'context', Content: text, OldNum: oldLineNum, NewNum: newLineNum });

// NEW:
var hlHtml = (file.highlightedLines && file.highlightedLines[newLineNum]) || null;
contextLines.push({ Type: 'context', Content: text, OldNum: oldLineNum, NewNum: newLineNum, HTML: hlHtml });
```

**Step 5: Update buildCodeLineBlocks to use server-side highlighting**

In `buildCodeLineBlocks` (around line 414):
```javascript
// OLD:
if (file.highlightCache && file.highlightCache[lineNum]) {
    html = '<code class="hljs">' + file.highlightCache[lineNum] + '</code>';
} else {
    html = '<code class="hljs">' + escapeHtml(lines[i] || '') + '</code>';
}

// NEW:
if (file.highlightedLines && file.highlightedLines[lineNum]) {
    html = '<code class="chroma">' + file.highlightedLines[lineNum] + '</code>';
} else {
    html = '<code>' + escapeHtml(lines[i] || '') + '</code>';
}
```

**Step 6: Remove dead code**

Delete:
- `preHighlightFile()` function (lines 329-345)
- `highlightDiffLine()` function (lines 349-361)
- The `f.highlightCache` assignment (line 109)
- The `f.lang` assignment can stay (used elsewhere) or remove if no longer needed

**Step 7: Verify build**

```bash
go build -o crit . && echo "OK"
```

**Step 8: Commit**

```bash
git add frontend/app.js
git commit -m "feat: use server-side Chroma highlighting in diff views"
```

---

### Task 5: Remove hljs scripts from index.html (keep only highlight.min.js for markdown)

**Files:**
- Modify: `frontend/index.html`

**Step 1: Remove language pack scripts**

Remove lines 123-135 (all `hljs-*.min.js` scripts). Keep `highlight.min.js` (line 122) for markdown fenced code blocks.

Actually — we could keep all language packs since markdown code blocks benefit from them. But the main highlight.min.js core already includes common languages. Check: if we keep `highlight.min.js` for markdown code blocks, we should keep the language packs too since a markdown file might have ```elixir fenced blocks.

**Decision: Keep highlight.min.js and all hljs-*.min.js for markdown code blocks.** The binary size savings from removing them is minimal vs the quality regression for markdown fenced code blocks.

Alternative: If we want to fully remove hljs later, we can add a `/api/highlight` endpoint. But that's a follow-up task.

**Step 2: No changes needed to index.html for now**

Since we're keeping hljs for markdown code blocks, all script tags stay.

**Step 3: Commit (skip if no changes)**

---

### Task 6: Update E2E tests

**Files:**
- Modify: `e2e/tests/syntax-highlighting.spec.ts`

**Step 1: Update test descriptions**

The existing tests check for `<span>` elements in diff content. Chroma also produces `<span>` elements, so the tests should still pass. But update comments to reference Chroma instead of hljs:

```typescript
// Update comment on line 13:
// Addition side should have Chroma spans for Go keywords/strings

// Update comment on line 17-18:
// Check that the content contains <span> elements (Chroma highlighting)
```

**Step 2: Run E2E tests**

```bash
make e2e
```

Expected: all PASS. The tests check for presence of `<span>` elements in diff content — Chroma produces these just like hljs did.

**Step 3: Commit if there are changes**

```bash
git add e2e/tests/syntax-highlighting.spec.ts
git commit -m "test: update syntax highlighting test comments for Chroma"
```

---

### Task 7: Run full test suite and manual verification

**Step 1: Run Go tests**

```bash
go test ./... -v
```

**Step 2: Run E2E tests**

```bash
make e2e
```

**Step 3: Manual verification**

```bash
# Build and run against a real repo with Elixir/Go/JS files
go build -o crit .
cd /path/to/repo-with-elixir && /path/to/crit
```

Check:
- [ ] Diff view shows richer highlighting than before (atoms, module names, function names have distinct colors)
- [ ] Split and unified diff modes both work
- [ ] Spacer expansion shows highlighted lines
- [ ] Light and dark themes both look correct
- [ ] Markdown fenced code blocks still highlight (via hljs)
- [ ] File mode (non-git) code files render with highlighting

**Step 4: Final commit if any fixes needed**

---

### Task 8: Port Chroma CSS to crit-web (parity requirement)

**Files:**
- Modify: `../crit-web/assets/css/app.css`

Per the monorepo CLAUDE.md, review page CSS must stay in sync between crit and crit-web. Port the new Chroma CSS classes to `crit-web/assets/css/app.css`, replacing the hljs theme rules there as well.

Note: crit-web's `document-renderer.js` still uses hljs on the client side — that's a separate migration. For now, just port the CSS so both have the Chroma classes available. The crit-web diff rendering is done server-side differently (it receives pre-rendered HTML from the crit upload), so the CSS classes need to match.

**Step 1: Copy the Chroma CSS theme rules from crit's theme.css to crit-web's app.css**

Replace the `.hljs*` rules in crit-web with the same Chroma rules.

**Step 2: Commit**

```bash
cd ../crit-web
git add assets/css/app.css
git commit -m "feat: port Chroma CSS theme from crit local"
```

# View All Comments Panel — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a right-side panel that shows all comments across all files in one scrollable list, with click-to-navigate to the comment's location in the document.

**Architecture:** A new right-side panel (mirroring the file-tree-panel pattern on the left) that aggregates comments from all files, groups them by file, and renders each as a clickable card. Clicking a comment scrolls to and highlights the inline comment in the main content area. The panel is toggled via the header comment count (which becomes clickable) and a keyboard shortcut.

**Tech Stack:** Vanilla JS (no frameworks), CSS custom properties, existing DOM patterns from file-tree-panel.

---

## Design Decisions

1. **Toggle mechanism**: Clicking the existing `#commentCount` span in the header opens/closes the panel. This is the natural affordance — user sees "3 comments" and clicks it to see them.
2. **Panel position**: Right side of `.main-layout` (opposite the file tree). Uses the same sticky sidebar pattern.
3. **Keyboard shortcut**: `Shift+C` — follows the existing convention where uppercase = action variant (`Shift+F` = Finish).
4. **Persistence**: Panel open/closed state saved to cookie (`crit-comments-panel`), same pattern as TOC.
5. **Grouping**: Comments grouped by file path, sorted by line number within each file. File groups shown in same order as `files` array.
6. **Click navigation**: Clicking a comment card scrolls the file section into view, opens it if collapsed, then scrolls to and briefly highlights the inline comment.
7. **Live updates**: Panel re-renders whenever `updateCommentCount()` is called (add/edit/delete/SSE refresh), keeping it in sync.
8. **Empty state**: When no comments exist, show a muted "No comments yet" message.
9. **No backend changes needed**: All data is already available client-side in the `files` array. No new API endpoints.

---

### Task 1: Add the comments panel HTML shell

**Files:**
- Modify: `frontend/index.html:74-87` (inside `.main-layout`)

**Step 1: Add the panel element after `.main-content`**

In `frontend/index.html`, add the comments panel div inside `.main-layout`, after the `.main-content` div (after line 86):

```html
<div class="comments-panel comments-panel-hidden" id="commentsPanel">
  <div class="comments-panel-header">
    <span class="comments-panel-title">Comments</span>
    <button class="comments-panel-close" title="Close comments panel" aria-label="Close comments panel">&#x2715;</button>
  </div>
  <div class="comments-panel-body" id="commentsPanelBody"></div>
</div>
```

The `.main-layout` should now have three children: `.file-tree-panel`, `.main-content`, `.comments-panel`.

**Step 2: Verify the page still loads**

Run: `go build -o crit . && ./crit --no-open --port 3123 e2e/fixtures/test-plan.md`
Expected: Server starts, page loads at `http://127.0.0.1:3123`, panel is hidden (has `comments-panel-hidden` class).

**Step 3: Commit**

```bash
git add frontend/index.html
git commit -m "feat: add comments panel HTML shell"
```

---

### Task 2: Style the comments panel

**Files:**
- Modify: `frontend/style.css` (add after File Tree Sidebar section, around line 1370)

**Step 1: Add comments panel CSS**

Add the following CSS after the file tree sidebar section in `style.css`:

```css
/* ===== Comments Panel (Right Sidebar) ===== */
.comments-panel {
  width: 320px;
  flex-shrink: 0;
  background: var(--bg-secondary);
  border-left: 1px solid var(--border);
  position: sticky;
  top: var(--header-height, 49px);
  height: calc(100vh - var(--header-height, 49px));
  overflow-y: auto;
  overflow-x: hidden;
  display: flex;
  flex-direction: column;
  z-index: 85;
}
.comments-panel::-webkit-scrollbar { width: 5px; }
.comments-panel::-webkit-scrollbar-thumb { background: var(--scrollbar-thumb); border-radius: 3px; }
.comments-panel-hidden { display: none; }

.comments-panel-header {
  padding: 12px 16px 8px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.comments-panel-title {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--fg-muted);
}
.comments-panel-close {
  background: none;
  border: none;
  color: var(--fg-muted);
  cursor: pointer;
  font-size: 14px;
  padding: 2px 6px;
  border-radius: 4px;
}
.comments-panel-close:hover { background: var(--bg-hover); color: var(--fg); }

.comments-panel-body {
  padding: 8px 0;
  flex: 1;
}

.comments-panel-empty {
  padding: 24px 16px;
  text-align: center;
  color: var(--fg-muted);
  font-size: 13px;
}

.comments-panel-file-group {
  margin-bottom: 4px;
}
.comments-panel-file-name {
  padding: 6px 16px;
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.comments-panel-card {
  padding: 8px 16px;
  cursor: pointer;
  border-bottom: 1px solid var(--border);
}
.comments-panel-card:hover { background: var(--bg-hover); }
.comments-panel-card-line {
  font-size: 11px;
  color: var(--fg-muted);
  margin-bottom: 4px;
}
.comments-panel-card-body {
  font-size: 13px;
  line-height: 1.5;
  color: var(--fg);
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
}
.comments-panel-card-body p { margin: 0; }
.comments-panel-card-body p + p { margin-top: 4px; }

.comments-panel-card-highlight {
  animation: comment-panel-flash 1s ease-out;
}
@keyframes comment-panel-flash {
  0% { background: var(--accent-bg, rgba(59, 130, 246, 0.15)); }
  100% { background: transparent; }
}
```

**Step 2: Add responsive rules**

In the existing `@media (max-width: 900px)` block, add:
```css
.comments-panel { width: 260px; }
```

In the existing `@media (max-width: 600px)` block, add:
```css
.comments-panel { display: none !important; }
```

**Step 3: Verify styles render correctly**

Manually remove `comments-panel-hidden` class in browser DevTools to confirm panel appears on the right side with correct styling.

**Step 4: Commit**

```bash
git add frontend/style.css
git commit -m "feat: style comments panel sidebar"
```

---

### Task 3: Make the header comment count clickable

**Files:**
- Modify: `frontend/style.css` (comment-count rule)
- Modify: `frontend/app.js:3131-3141` (updateCommentCount function)

**Step 1: Add clickable style to comment count**

Find the `.comment-count` CSS rule in `style.css` and add cursor/hover styles:

```css
.comment-count { cursor: pointer; }
.comment-count:hover { text-decoration: underline; }
```

**Step 2: Add click handler to toggle panel**

In `app.js`, after the `updateCommentCount()` function (around line 3141), add:

```javascript
document.getElementById('commentCount').addEventListener('click', function() {
  toggleCommentsPanel();
});
```

**Step 3: Add the toggle function**

In `app.js`, after the `updateCommentCount` function, add:

```javascript
function toggleCommentsPanel() {
  var panel = document.getElementById('commentsPanel');
  var isHidden = panel.classList.contains('comments-panel-hidden');
  panel.classList.toggle('comments-panel-hidden');
  setCookie('crit-comments-panel', isHidden ? 'open' : 'closed');
  if (isHidden) {
    renderCommentsPanel();
  }
}
```

**Step 4: Verify click toggles panel**

Run the app, add a comment, click the "1 comment" text in the header. Panel should appear/disappear.

**Step 5: Commit**

```bash
git add frontend/style.css frontend/app.js
git commit -m "feat: make comment count clickable to toggle panel"
```

---

### Task 4: Render comments in the panel

**Files:**
- Modify: `frontend/app.js` (add `renderCommentsPanel` function)

**Step 1: Add the renderCommentsPanel function**

Add this function near the `updateCommentCount` function in `app.js`:

```javascript
function renderCommentsPanel() {
  var panel = document.getElementById('commentsPanel');
  if (panel.classList.contains('comments-panel-hidden')) return;

  var body = document.getElementById('commentsPanelBody');
  body.innerHTML = '';

  var hasComments = false;

  for (var i = 0; i < files.length; i++) {
    var file = files[i];
    var unresolvedComments = file.comments.filter(function(c) { return !c.resolved; });
    if (unresolvedComments.length === 0) continue;
    hasComments = true;

    // Sort by start_line
    unresolvedComments.sort(function(a, b) { return a.start_line - b.start_line; });

    var group = document.createElement('div');
    group.className = 'comments-panel-file-group';

    // File name header (only in multi-file mode)
    if (files.length > 1) {
      var fileName = document.createElement('div');
      fileName.className = 'comments-panel-file-name';
      fileName.textContent = file.path;
      fileName.title = file.path;
      group.appendChild(fileName);
    }

    for (var j = 0; j < unresolvedComments.length; j++) {
      var comment = unresolvedComments[j];
      var card = document.createElement('div');
      card.className = 'comments-panel-card';
      card.dataset.commentId = comment.id;
      card.dataset.filePath = file.path;

      var lineRef = document.createElement('div');
      lineRef.className = 'comments-panel-card-line';
      lineRef.textContent = comment.start_line === comment.end_line
        ? 'Line ' + comment.start_line
        : 'Lines ' + comment.start_line + '-' + comment.end_line;
      if (comment.carried_forward) {
        lineRef.textContent += ' · Unresolved';
      }

      var bodyEl = document.createElement('div');
      bodyEl.className = 'comments-panel-card-body';
      bodyEl.innerHTML = commentMd.render(comment.body);

      card.appendChild(lineRef);
      card.appendChild(bodyEl);
      card.addEventListener('click', (function(commentId, filePath) {
        return function() { scrollToComment(commentId, filePath); };
      })(comment.id, file.path));

      group.appendChild(card);
    }

    body.appendChild(group);
  }

  if (!hasComments) {
    var empty = document.createElement('div');
    empty.className = 'comments-panel-empty';
    empty.textContent = 'No comments yet';
    body.appendChild(empty);
  }
}
```

**Step 2: Call renderCommentsPanel from updateCommentCount**

Modify `updateCommentCount()` to also refresh the panel. At the end of the function (before the closing `}`), add:

```javascript
renderCommentsPanel();
```

**Step 3: Verify panel shows comments**

Run the app, add a comment, click the comment count. Panel should show the comment with line reference and body text.

**Step 4: Commit**

```bash
git add frontend/app.js
git commit -m "feat: render comments list in panel"
```

---

### Task 5: Implement click-to-navigate (scrollToComment)

**Files:**
- Modify: `frontend/app.js` (add `scrollToComment` function)
- Modify: `frontend/style.css` (add highlight animation for inline comments)

**Step 1: Add highlight CSS for inline comments**

In `style.css`, add to the comment card section:

```css
.comment-card-highlight {
  animation: comment-inline-flash 1.5s ease-out;
}
@keyframes comment-inline-flash {
  0%, 30% { box-shadow: 0 0 0 2px var(--accent); }
  100% { box-shadow: none; }
}
```

**Step 2: Add scrollToComment function**

In `app.js`, add near the `renderCommentsPanel` function:

```javascript
function scrollToComment(commentId, filePath) {
  // 1. Find the file section and expand if collapsed
  var section = document.querySelector('.file-section[data-path="' + CSS.escape(filePath) + '"]');
  if (!section) return;
  if (!section.open) section.open = true;

  // 2. Find the inline comment card by comment ID
  var commentCard = section.querySelector('.comment-card[data-comment-id="' + commentId + '"]');
  if (!commentCard) return;

  // 3. Scroll into view
  commentCard.scrollIntoView({ behavior: 'smooth', block: 'center' });

  // 4. Flash highlight
  commentCard.classList.remove('comment-card-highlight');
  // Force reflow to restart animation
  void commentCard.offsetWidth;
  commentCard.classList.add('comment-card-highlight');
  commentCard.addEventListener('animationend', function() {
    commentCard.classList.remove('comment-card-highlight');
  }, { once: true });
}
```

**Step 3: Add data-comment-id to inline comment cards**

In the `createCommentElement` function (around line 2973), add a `data-comment-id` attribute to the card element. Find the line:

```javascript
card.className = 'comment-card' + (comment.carried_forward ? ' carried-forward' : '');
```

Add after it:

```javascript
card.dataset.commentId = comment.id;
```

**Step 4: Verify click-to-navigate works**

Run the app with multiple files. Add comments to different files. Open the panel, click a comment card. Should scroll to and highlight the inline comment.

**Step 5: Commit**

```bash
git add frontend/app.js frontend/style.css
git commit -m "feat: click comment in panel to navigate to inline location"
```

---

### Task 6: Add keyboard shortcut (Shift+C)

**Files:**
- Modify: `frontend/app.js` (keyboard handler, around line 3804)
- Modify: `frontend/index.html:101-119` (shortcuts table)

**Step 1: Add keyboard handler**

In the `keydown` handler switch statement in `app.js`, add a new case after the `'F'` case (around line 3810):

```javascript
case 'C': {
  e.preventDefault();
  toggleCommentsPanel();
  break;
}
```

**Step 2: Add to shortcuts overlay**

In `index.html`, add a row to the shortcuts table (after the `Shift+F` row, line 112):

```html
<tr><td><kbd>Shift</kbd>+<kbd>C</kbd></td><td>Toggle comments panel</td></tr>
```

**Step 3: Verify shortcut works**

Run the app, press `Shift+C`. Panel should toggle open/closed.

**Step 4: Commit**

```bash
git add frontend/app.js frontend/index.html
git commit -m "feat: add Shift+C shortcut for comments panel"
```

---

### Task 7: Restore panel state and add close button handler

**Files:**
- Modify: `frontend/app.js` (initialization code)

**Step 1: Add close button handler**

In `app.js`, near the TOC close handler (around line 3677), add:

```javascript
document.querySelector('.comments-panel-close').addEventListener('click', function() {
  document.getElementById('commentsPanel').classList.add('comments-panel-hidden');
  setCookie('crit-comments-panel', 'closed');
});
```

**Step 2: Restore panel state on load**

In the initialization section of `app.js`, after data is loaded and rendered (find where `buildToc()` is called, and add nearby):

```javascript
if (getCookie('crit-comments-panel') === 'open') {
  document.getElementById('commentsPanel').classList.remove('comments-panel-hidden');
  renderCommentsPanel();
}
```

**Step 3: Verify persistence**

Open panel, reload page — panel should remain open. Close panel, reload — should stay closed.

**Step 4: Commit**

```bash
git add frontend/app.js
git commit -m "feat: persist comments panel state and add close button"
```

---

### Task 8: Port to crit-web (document-renderer.js)

**Files:**
- Modify: `crit-web/assets/js/document-renderer.js` (add panel rendering + toggle)
- Modify: `crit-web/lib/crit_web/live/review_live.html.heex` (add panel HTML)
- Modify: `crit-web/assets/css/app.css` (add panel styles, same as crit local)

**Step 1: Add panel HTML to review_live.html.heex**

Find the `.main-layout` div and add the comments panel after `.main-content`, matching the exact same HTML structure from `frontend/index.html`.

**Step 2: Copy panel CSS to app.css**

Copy the full comments panel CSS section from `frontend/style.css` to `crit-web/assets/css/app.css` in the corresponding location.

**Step 3: Port the JS functions to document-renderer.js**

Port `renderCommentsPanel()`, `scrollToComment()`, `toggleCommentsPanel()`, the click handler on `#commentCount`, and the close button handler. The `document-renderer.js` already has access to the same `files` array and `commentMd` renderer.

**Step 4: Verify in crit-web**

Run: `cd crit-web && mix phx.server`
Navigate to a review with comments, verify panel works identically.

**Step 5: Commit**

```bash
git add crit-web/assets/js/document-renderer.js crit-web/lib/crit_web/live/review_live.html.heex crit-web/assets/css/app.css
git commit -m "feat: port comments panel to crit-web"
```

---

### Task 9: Final integration testing and polish

**Step 1: Run all Go tests**

Run: `go test ./...`
Expected: All pass (no backend changes, but verify nothing broke).

**Step 2: Manual testing checklist**

- [ ] Single-file mode: panel works, no file name headers shown
- [ ] Multi-file mode: panel groups comments by file with file name headers
- [ ] Git mode: comments on diff lines show correctly in panel
- [ ] Click-to-navigate works across collapsed file sections
- [ ] Panel state persists across page reloads
- [ ] Shift+C shortcut toggles panel
- [ ] Close button works
- [ ] Empty state shows when no comments
- [ ] Panel updates in real-time as comments are added/edited/deleted
- [ ] Theme switching: panel renders correctly in light, dark, and system themes
- [ ] Responsive: panel hidden on small screens (< 600px)
- [ ] Comment body truncated at 3 lines with ellipsis in panel cards

**Step 3: Commit any polish fixes**

```bash
git add -A
git commit -m "fix: comments panel polish"
```

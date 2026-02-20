# Draft Autosave

## Goal

If a user is typing a comment and accidentally closes the tab, navigates away, or the browser crashes, their in-progress text should survive and be restored when they return.

## Design

**What to save:** The current `activeForm` state (if a comment form is open) plus the textarea's current value. Stored as a single JSON object in `localStorage` under the key `crit-draft-{filename}` (scoped per file to avoid cross-file collisions).

**Shape:**

```json
{
  "startLine": 10,
  "endLine": 12,
  "afterBlockIndex": 5,
  "editingId": null,
  "body": "This needs more detail about...",
  "savedAt": 1708444800000
}
```

**When to save:** On every `input` event on the comment textarea, debounced to 500ms. Also on `beforeunload`.

**When to restore:** During `init()`, after comments are loaded and the document is rendered. If a draft exists:
1. Check that `savedAt` is less than 24 hours old (discard stale drafts).
2. Verify the `startLine`/`endLine` range still exists in the current document (the file may have changed).
3. If valid, set `activeForm` from the draft, render, and populate the textarea with the saved body.
4. Show a subtle toast: "Draft restored" that auto-dismisses after 3 seconds.

**When to clear:** On successful `submitComment()` or `cancelComment()`. Cancel should also clear the draft (the user explicitly abandoned it).

**Edge case — editing vs. new:** If `editingId` is set, verify the comment still exists before restoring. If it was deleted, discard the draft.

**No server involvement.** This is purely a localStorage feature.

## Implementation

- Add a `saveDraft()` function called from the textarea `input` handler (debounced) and `beforeunload`.
- Add a `clearDraft()` function called from `submitComment()` and `cancelComment()`.
- Add a `restoreDraft()` function called at the end of `init()`.
- The toast is a fixed-position div that fades in/out with CSS transitions. No new HTML element needed — create it dynamically.

## Scope

Frontend-only change to `index.html`. No backend modifications.

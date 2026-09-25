import { test, expect, type Page, type Locator } from '@playwright/test';
import {
  clearAllComments, loadPage, goSection, clearFocus, switchToDocumentView,
  openLineComment,
} from './helpers';

// Keyboard focus is model-driven in git mode. On a diff it shows as the one
// line Pierre marks [data-selected-line]; in a markdown file's Document view
// it is the rendered `.line-block.kb-nav.focused` (visual range adds
// `.selected`).

// Every line Pierre marks selected, as "path:line:lineType:column" in
// visual order. In split view a focused row marks both of its sides.
function selectedCells(page: Page): Promise<string[]> {
  return page.locator('diffs-container [data-content] > [data-selected-line][data-line]').evaluateAll(els =>
    els.flatMap((el) => {
      // Skip rows detached mid-read by a re-render.
      const root = el.getRootNode();
      if (!(root instanceof ShadowRoot) || !root.host.isConnected) return [];
      const path = (root.host.querySelector('.pierre-file-header') as HTMLElement | null)?.dataset.filePath;
      const column = el.closest('code[data-deletions]') ? 'old' : 'new';
      return [`${path}:${(el as HTMLElement).dataset.line}:${(el as HTMLElement).dataset.lineType}:${column}`];
    }));
}

// Keyboard-focused diff rows as "path:line:lineType". A row is named by its
// new-side line, or its old-side line when it has no new side (the same rule
// Crit's j/k and `c` use).
async function focusedDiff(page: Page): Promise<string[]> {
  const cells = await selectedCells(page);
  const newSide = cells.filter(c => c.endsWith(':new'));
  return (newSide.length ? newSide : cells).map(c => c.replace(/:(new|old)$/, ''));
}

// Current keyboard focus (diff line or document block), or '' when none.
async function focusKey(page: Page): Promise<string> {
  const block = await page.locator('.line-block.kb-nav.focused').evaluateAll(els =>
    els.map(el => `block:${(el as HTMLElement).dataset.filePath}:${(el as HTMLElement).dataset.startLine}`));
  return [...(await focusedDiff(page)), ...block].join(' | ');
}

// Press a nav key and wait until the focus has moved.
async function step(page: Page, key: 'j' | 'k'): Promise<string> {
  const before = await focusKey(page);
  await page.keyboard.press(key);
  await expect.poll(() => focusKey(page)).not.toBe(before);
  return focusKey(page);
}

// Press j until the focus matches `want` (a prefix of a focusKey value).
async function jUntil(page: Page, want: string, max = 200): Promise<void> {
  for (let i = 0; i < max; i++) {
    if ((await focusKey(page)).startsWith(want)) return;
    await step(page, 'j');
  }
  throw new Error(`keyboard focus never reached ${want}`);
}

async function treePaths(page: Page): Promise<string[]> {
  const tree = page.locator('.tree-file[data-tree-path]');
  await expect(tree.first()).toBeVisible();
  return tree.evaluateAll(els => els.map(el => (el as HTMLElement).dataset.treePath!));
}

// Focus a block of plan.md's rendered document with j.
async function focusDocBlock(page: Page, startLine: number): Promise<Locator> {
  await jUntil(page, `block:plan.md:${startLine}`);
  const block = page.locator(`.line-block.kb-nav.focused[data-file-path="plan.md"][data-start-line="${startLine}"]`);
  await expect(block).toHaveCount(1);
  return block;
}

// ============================================================
// j/k Navigation on Diff Lines (Split Mode)
// ============================================================
test.describe('Keyboard Navigation — Diff Split Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await clearFocus(page);
  });

  test('j focuses the first line of the first file', async ({ page }) => {
    const [first] = await treePaths(page);
    await expect.poll(() => focusedDiff(page)).toEqual([]);

    await page.keyboard.press('j');
    await expect.poll(() => focusedDiff(page)).toEqual([expect.stringMatching(new RegExp(`^${first.replace('.', '\\.')}:1:`))]);
  });

  test('j navigates to next line, k navigates to previous', async ({ page }) => {
    const [first] = await treePaths(page);
    await step(page, 'j');
    const second = await step(page, 'j');
    expect(second).toMatch(new RegExp(`^${first.replace('.', '\\.')}:2:`));
    await expect.poll(() => focusedDiff(page)).toHaveLength(1);

    const back = await step(page, 'k');
    expect(back).toMatch(new RegExp(`^${first.replace('.', '\\.')}:1:`));
    await expect.poll(() => focusedDiff(page)).toHaveLength(1);
  });

  test('multiple j presses move forward sequentially', async ({ page }) => {
    const [first] = await treePaths(page);
    await page.keyboard.press('j');
    await page.keyboard.press('j');
    await page.keyboard.press('j');
    await expect.poll(() => focusedDiff(page)).toEqual([expect.stringMatching(new RegExp(`^${first.replace('.', '\\.')}:3:`))]);
  });

  test('j/k in split diff mode navigates rows, not individual sides', async ({ page }) => {
    // legacy.go: deletions Old1..Old12 (old 8-19) are replaced by additions
    // New1..New4 (new 8-11). Split view pairs them row by row, so the row
    // holding old 8 / new 8 is one stop: j from it goes to new 9, never to
    // old 8 on the other side of the same row.
    await jUntil(page, 'legacy.go:8:change-addition');
    // The whole row is focused: both of its sides, nothing else.
    await expect.poll(() => selectedCells(page)).toEqual(['legacy.go:8:change-deletion:old', 'legacy.go:8:change-addition:new']);
    await page.keyboard.press('j');
    await expect.poll(() => selectedCells(page)).toEqual(['legacy.go:9:change-deletion:old', 'legacy.go:9:change-addition:new']);
    await page.keyboard.press('j');
    await expect.poll(() => selectedCells(page)).toEqual(['legacy.go:10:change-deletion:old', 'legacy.go:10:change-addition:new']);
    await page.keyboard.press('k');
    await expect.poll(() => selectedCells(page)).toEqual(['legacy.go:9:change-deletion:old', 'legacy.go:9:change-addition:new']);
  });

  test('j/k stays continuous when mouse is stationary over the document', async ({ page }) => {
    const paths = await treePaths(page);
    // Rest the pointer over the diff while navigating purely with j.
    const box = await page.locator('#filesContainer').boundingBox();
    expect(box).toBeTruthy();
    await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);

    const seen: string[] = [];
    for (let i = 0; i < 20; i++) {
      seen.push(await step(page, 'j'));
      await expect.poll(() => focusedDiff(page)).toHaveLength(1);
    }
    // Every press lands on a new line, moving forward through the files.
    expect(new Set(seen).size).toBe(seen.length);
    const fileOrder = seen.map(k => paths.indexOf(k.split(':')[0]));
    expect(fileOrder).toEqual([...fileOrder].sort((a, b) => a - b));
    for (let i = 1; i < seen.length; i++) {
      const [pa, la] = seen[i - 1].split(':');
      const [pb, lb] = seen[i].split(':');
      if (pa === pb && !seen[i].endsWith('change-deletion') && !seen[i - 1].endsWith('change-deletion')) {
        expect(Number(lb)).toBeGreaterThan(Number(la));
      }
    }
  });

  test('j/k resumes after canceling comment form with Escape', async ({ page }) => {
    for (let i = 0; i < 17; i++) await step(page, 'j');
    await expect.poll(() => focusedDiff(page)).toHaveLength(1);
    const next = await focusKey(page);
    const target = await step(page, 'k');

    await page.keyboard.press('c');
    const form = page.locator('#filesContainer .comment-form');
    await expect(form.locator('textarea')).toBeFocused();
    await form.locator('textarea').press('Escape');
    await expect(form).toHaveCount(0);

    // Focus is still on the same line after cancel (not reset to top).
    await expect.poll(() => focusKey(page)).toBe(target);
    expect(await step(page, 'j')).toBe(next);
  });

  test('j/k resumes after submitting a new comment', async ({ page, request }) => {
    for (let i = 0; i < 17; i++) await step(page, 'j');
    await expect.poll(() => focusedDiff(page)).toHaveLength(1);
    const next = await focusKey(page);
    const target = await step(page, 'k');
    const [path, line] = target.split(':');

    await page.keyboard.press('c');
    const textarea = page.locator('#filesContainer .comment-form textarea');
    await expect(textarea).toBeFocused();
    await textarea.fill('resume focus after submit');
    await page.locator('#filesContainer .comment-form .btn-primary').click();
    await expect(page.locator('#filesContainer .comment-form')).toHaveCount(0);
    await expect(page.locator('.comment-card').filter({ hasText: 'resume focus after submit' })).toBeVisible();

    // The comment landed on the focused line.
    const comments = await (await request.get(`/api/file/comments?path=${encodeURIComponent(path)}`)).json() as Array<{ body: string; end_line: number }>;
    expect(comments.find(c => c.body === 'resume focus after submit')?.end_line).toBe(Number(line));

    await expect.poll(() => focusKey(page)).toBe(target);
    expect(await step(page, 'j')).toBe(next);
  });

  test('k from first line stays at first line', async ({ page }) => {
    const first = await step(page, 'j');
    await page.keyboard.press('k');
    // Wait for the k to be handled (a later j proves the key queue drained).
    await page.keyboard.press('j');
    await expect.poll(() => focusKey(page)).not.toBe(first);
    expect(await step(page, 'k')).toBe(first);
    await page.keyboard.press('k');
    const second = await step(page, 'j');
    expect(second).not.toBe(first);
    await expect.poll(() => focusedDiff(page)).toHaveLength(1);
  });

  test('k with no focus goes to the last line of the last file', async ({ page, request }) => {
    const paths = await treePaths(page);
    const last = paths[paths.length - 1];
    const diff = await (await request.get(`/api/file/diff?path=${encodeURIComponent(last)}`)).json();
    const hunks = diff.hunks as Array<{ Lines: Array<{ Type: string; NewNum: number }> }>;
    const lastLine = hunks[hunks.length - 1].Lines.filter(l => l.Type !== 'del').pop()!;

    await page.keyboard.press('k');
    await expect.poll(() => focusedDiff(page)).toEqual([expect.stringMatching(new RegExp(`^${last.replace('.', '\\.')}:${lastLine.NewNum}:`))]);
  });
});

// ============================================================
// j/k Navigation on Markdown Blocks (Document View)
// ============================================================
test.describe('Keyboard Navigation — Markdown Document View', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);
    await clearFocus(page);
  });

  test('j/k navigates markdown line-blocks', async ({ page }) => {
    const first = await focusDocBlock(page, 1);
    await expect(first).toHaveAttribute('data-block-index', '0');
    // No diff line is focused while a document block is.
    await expect.poll(() => focusedDiff(page)).toEqual([]);

    await page.keyboard.press('j');
    const second = page.locator('.line-block.kb-nav.focused');
    await expect(second).toHaveCount(1);
    await expect(second).toHaveAttribute('data-block-index', '1');

    await page.keyboard.press('k');
    await expect(page.locator('.line-block.kb-nav.focused')).toHaveAttribute('data-block-index', '0');
    await expect(page.locator('.line-block.kb-nav.focused')).toHaveCount(1);
  });
});

// ============================================================
// Comment Shortcuts (c, e, d)
// ============================================================
test.describe('Keyboard Comment Shortcuts — Diff', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await clearFocus(page);
  });

  test('c opens comment form on focused diff line', async ({ page }) => {
    const [first] = await treePaths(page);
    const focused = await step(page, 'j');
    expect(focused.startsWith(`${first}:1:`)).toBe(true);

    await page.keyboard.press('c');

    const form = page.locator('#filesContainer .comment-form');
    await expect(form).toBeVisible();
    await expect(form.locator('textarea')).toBeFocused();
    await expect(form.locator('.comment-form-header')).toHaveText('Comment on Line 1');
    await expect(page.locator('diffs-container').filter({
      has: page.locator(`.pierre-file-header[data-file-path="${first}"]`),
    }).locator('.comment-form')).toHaveCount(1);
  });

  // Commenting through the gutter leaves keyboard focus on the commented
  // line, so e / d act on that comment.
  async function commentOnServerLine(page: Page, body: string) {
    const item = await goSection(page);
    const form = await openLineComment(page, item, 23);
    await form.locator('textarea').fill(body);
    await form.locator('.btn-primary').click();
    const card = item.locator('.comment-card', { hasText: body });
    await expect(card).toBeVisible();
    return card;
  }

  test('e edits comment on focused diff line', async ({ page }) => {
    await commentOnServerLine(page, 'Edit me via shortcut');

    await page.keyboard.press('e');

    const editTextarea = page.locator('#filesContainer .comment-form textarea');
    await expect(editTextarea).toBeVisible();
    await expect(editTextarea).toHaveValue('Edit me via shortcut');
  });

  test('d deletes comment on focused diff line', async ({ page, request }) => {
    const card = await commentOnServerLine(page, 'Delete me via shortcut');

    await page.keyboard.press('d');

    await expect(card).toHaveCount(0);
    await expect.poll(async () => (await (await request.get('/api/file/comments?path=server.go')).json()).length).toBe(0);
  });
});

test.describe('Keyboard Comment Shortcuts — Markdown', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('c opens comment form on focused markdown block', async ({ page }) => {
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await clearFocus(page);

    await focusDocBlock(page, 1);
    await page.keyboard.press('c');

    const form = doc.locator('.comment-form');
    await expect(form).toBeVisible();
    await expect(form.locator('textarea')).toBeFocused();
    await expect(form.locator('.comment-form-header')).toHaveText('Comment on Line 1');
  });

  test('e edits comment on focused markdown block', async ({ page, request }) => {
    await request.post(`/api/file/comments?path=plan.md`, {
      data: { start_line: 1, end_line: 1, body: 'Edit this markdown comment' },
    });

    await loadPage(page);
    await switchToDocumentView(page);
    await clearFocus(page);

    await focusDocBlock(page, 1);
    await page.keyboard.press('e');

    const textarea = page.locator('#filesContainer .comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('Edit this markdown comment');
  });

  test('d deletes comment on focused markdown block', async ({ page, request }) => {
    await request.post(`/api/file/comments?path=plan.md`, {
      data: { start_line: 1, end_line: 1, body: 'Delete this markdown comment' },
    });

    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await clearFocus(page);

    await expect(doc.locator('.comment-card')).toBeVisible();
    await focusDocBlock(page, 1);
    await page.keyboard.press('d');

    await expect(page.locator('#filesContainer .comment-card', { hasText: 'Delete this markdown comment' })).toHaveCount(0);
  });
});

// ============================================================
// UI Toggles
// ============================================================
test.describe('Keyboard UI Toggles', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await clearFocus(page);
  });

  test('? opens settings panel to Shortcuts tab', async ({ page }) => {
    const overlay = page.locator('.settings-overlay');

    // Initially not active
    await expect(overlay).not.toHaveClass(/active/);

    // Press ? to open
    await page.keyboard.press('?');
    await expect(overlay).toHaveClass(/active/);
    await expect(page.locator('.settings-tab.active')).toHaveText('Shortcuts');

    // Press ? again to close (toggle behavior when on shortcuts tab)
    await page.keyboard.press('?');
    await expect(overlay).not.toHaveClass(/active/);
  });

  test('Escape closes settings panel', async ({ page }) => {
    const overlay = page.locator('.settings-overlay');

    await page.keyboard.press('?');
    await expect(overlay).toHaveClass(/active/);

    await page.keyboard.press('Escape');
    await expect(overlay).not.toHaveClass(/active/);
  });

  test('Shift+F triggers finish review (shows waiting overlay)', async ({ page }) => {
    const waitingOverlay = page.locator('#waitingOverlay');
    await expect(waitingOverlay).not.toHaveClass(/active/);

    await page.keyboard.press('Shift+F');

    // The waiting overlay should become active after the finish API call
    await expect(waitingOverlay).toHaveClass(/active/);
  });

  test('t does nothing in git mode (TOC is disabled)', async ({ page }) => {
    const toc = page.locator('#toc');

    // Initially has toc-hidden
    await expect(toc).toHaveClass(/toc-hidden/);

    // Press t — should be a no-op since TOC is hidden in git mode
    await page.keyboard.press('t');
    await expect(toc).toHaveClass(/toc-hidden/);
  });
});

// ============================================================
// Escape Behavior
// ============================================================
test.describe('Keyboard Escape Behavior', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await clearFocus(page);
  });

  test('Escape closes open comment form', async ({ page }) => {
    await step(page, 'j');
    await page.keyboard.press('c');

    const form = page.locator('#filesContainer .comment-form');
    await expect(form).toBeVisible();

    await form.locator('textarea').press('Escape');
    await expect(form).toHaveCount(0);
  });

  test('Escape clears focus when no form is open', async ({ page }) => {
    await step(page, 'j');
    await expect.poll(() => focusedDiff(page)).toHaveLength(1);

    await page.keyboard.press('Escape');
    await expect.poll(() => focusedDiff(page)).toEqual([]);
  });

  test('Escape clears document block focus when no form is open', async ({ page }) => {
    await switchToDocumentView(page);
    await clearFocus(page);
    await focusDocBlock(page, 1);

    await page.keyboard.press('Escape');
    await expect(page.locator('.kb-nav.focused')).toHaveCount(0);
    // A fresh j starts over from the top of the review.
    const [first] = await treePaths(page);
    expect((await step(page, 'j')).startsWith(`${first}:1:`)).toBe(true);
  });

  test('Escape on non-empty comment form prompts for confirmation', async ({ page }) => {
    await step(page, 'j');
    await page.keyboard.press('c');

    const textarea = page.locator('#filesContainer .comment-form textarea');
    await expect(textarea).toBeFocused();
    await textarea.fill('important draft I do not want to lose');

    // Cancel the confirm — form must stay open with content intact.
    page.once('dialog', (dialog) => {
      expect(dialog.type()).toBe('confirm');
      void dialog.dismiss();
    });
    await textarea.press('Escape');
    await expect(page.locator('#filesContainer .comment-form')).toBeVisible();
    await expect(textarea).toHaveValue('important draft I do not want to lose');

    // Accept the confirm — form should close.
    page.once('dialog', (dialog) => {
      expect(dialog.type()).toBe('confirm');
      void dialog.accept();
    });
    await textarea.press('Escape');
    await expect(page.locator('#filesContainer .comment-form')).toHaveCount(0);
  });

  test('Cancel button on non-empty comment form discards immediately (no confirm)', async ({ page }) => {
    await step(page, 'j');
    await page.keyboard.press('c');

    const textarea = page.locator('#filesContainer .comment-form textarea');
    await textarea.fill('draft');

    // Cancel is an explicit, labeled discard action — no prompt.
    let dialogShown = false;
    page.once('dialog', () => { dialogShown = true; });

    await page.locator('#filesContainer .comment-form button', { hasText: 'Cancel' }).click();
    await expect(page.locator('#filesContainer .comment-form')).toHaveCount(0);
    expect(dialogShown).toBe(false);
  });

  test('Escape on empty comment form closes silently (no confirm)', async ({ page }) => {
    await step(page, 'j');
    await page.keyboard.press('c');

    const textarea = page.locator('#filesContainer .comment-form textarea');
    await expect(textarea).toBeFocused();

    // If a dialog appears, the test fails (we never accept/dismiss).
    let dialogShown = false;
    page.once('dialog', () => { dialogShown = true; });

    await textarea.press('Escape');
    await expect(page.locator('#filesContainer .comment-form')).toHaveCount(0);
    expect(dialogShown).toBe(false);
  });
});

// ============================================================
// Visual Line Mode (V)
// ============================================================
test.describe('Keyboard Visual Line Mode — Markdown', () => {
  let doc: Locator;

  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    doc = await switchToDocumentView(page);
    await clearFocus(page);
  });

  test('V enters visual mode; j extends selection; c opens form spanning the range', async ({ page, request }) => {
    const anchor = await focusDocBlock(page, 1);
    const anchorStart = Number(await anchor.getAttribute('data-start-line'));

    // Enter visual mode — anchor block becomes selected
    await page.keyboard.press('Shift+V');
    await expect(doc.locator('.line-block.kb-nav[data-block-index="0"]')).toHaveClass(/selected/);

    // Extend selection downward with j: blocks 0-2 selected, 3 not.
    await page.keyboard.press('j');
    await page.keyboard.press('j');
    const blocks = doc.locator('.line-block.kb-nav');
    await expect(doc.locator('.line-block.kb-nav.focused')).toHaveAttribute('data-block-index', '2');
    await expect(blocks.nth(0)).toHaveClass(/selected/);
    await expect(blocks.nth(1)).toHaveClass(/selected/);
    await expect(blocks.nth(2)).toHaveClass(/selected/);
    await expect(blocks.nth(3)).not.toHaveClass(/selected/);
    const expansionEnd = Number(await blocks.nth(2).getAttribute('data-end-line'));

    // Open form on the selection
    await page.keyboard.press('c');
    const form = doc.locator('.comment-form');
    await expect(form).toBeVisible();
    await expect(form.locator('.comment-form-header')).toHaveText(`Comment on Lines ${anchorStart}-${expansionEnd}`);

    // Submit the comment and verify its persisted line range via the API.
    await form.locator('textarea').fill('multi-line via V');
    await form.locator('.btn-primary').click();
    await expect(doc.locator('.comment-card').filter({ hasText: 'multi-line via V' })).toBeVisible();

    const resp = await request.get('/api/file/comments?path=plan.md');
    const data = await resp.json() as Array<{ body: string; start_line: number; end_line: number }>;
    const created = data.find((c) => c.body === 'multi-line via V');
    expect(created).toBeDefined();
    expect(created!.start_line).toBe(anchorStart);
    expect(created!.end_line).toBe(expansionEnd);
  });

  test('Escape clears visual selection and keeps focus on current block', async ({ page }) => {
    await focusDocBlock(page, 1);
    await page.keyboard.press('Shift+V');
    await page.keyboard.press('j');
    await page.keyboard.press('j');

    // Selection is active across multiple blocks
    const focused = doc.locator('.line-block.kb-nav.focused');
    await expect(focused).toHaveAttribute('data-block-index', '2');
    await expect(doc.locator('.line-block.selected')).toHaveCount(3);

    await page.keyboard.press('Escape');

    // Selection cleared; focus stays at the furthest expansion.
    await expect(doc.locator('.line-block.selected')).toHaveCount(0);
    await expect(focused).toHaveCount(1);
    await expect(focused).toHaveAttribute('data-block-index', '2');
    // And j continues from there.
    await page.keyboard.press('j');
    await expect(focused).toHaveAttribute('data-block-index', '3');
  });

  test('V again exits visual mode (toggle)', async ({ page }) => {
    await focusDocBlock(page, 1);
    await page.keyboard.press('Shift+V');
    await page.keyboard.press('j');
    await expect(doc.locator('.line-block.selected')).toHaveCount(2);

    await page.keyboard.press('Shift+V');
    await expect(doc.locator('.line-block.selected')).toHaveCount(0);
    // Focus stays on the block, and j moves without extending a selection.
    await expect(doc.locator('.line-block.kb-nav.focused')).toHaveAttribute('data-block-index', '1');
    await page.keyboard.press('j');
    await expect(doc.locator('.line-block.kb-nav.focused')).toHaveAttribute('data-block-index', '2');
    await expect(doc.locator('.line-block.selected')).toHaveCount(0);
  });
});

// ============================================================
// Shortcuts Disabled When Typing
// ============================================================
test.describe('Shortcuts Disabled When Typing', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await clearFocus(page);
  });

  test('j types into textarea instead of navigating when textarea is focused', async ({ page }) => {
    const focused = await step(page, 'j');
    await page.keyboard.press('c');

    const textarea = page.locator('#filesContainer .comment-form textarea');
    await expect(textarea).toBeFocused();

    // Type 'j' — should go into the textarea, NOT navigate
    await textarea.pressSequentially('jjj');
    await expect(textarea).toHaveValue('jjj');

    // Keyboard focus did not move.
    await expect.poll(() => focusKey(page)).toBe(focused);
  });

  test('other shortcuts (?, t) do not fire when textarea is focused', async ({ page }) => {
    await step(page, 'j');
    await page.keyboard.press('c');

    const textarea = page.locator('#filesContainer .comment-form textarea');
    await expect(textarea).toBeFocused();

    // Type '?' — should go into textarea, not open settings panel
    await textarea.pressSequentially('?');
    await expect(textarea).toHaveValue('?');

    const overlay = page.locator('.settings-overlay');
    await expect(overlay).not.toHaveClass(/active/);
  });
});

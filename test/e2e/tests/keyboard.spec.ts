import { test, expect, type Page, type Locator } from '@playwright/test';
import {
  clearAllComments, loadPage, goSection, clearFocus, switchToDocumentView,
  openLineComment, treePaths, pressUntil, reviewScroller, addComment,
  recordClassAdditions, expectClassAddedTo, classAdditions,
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
function jUntil(page: Page, want: string): Promise<void> {
  return pressUntil(page, 'j', async () => (await focusKey(page)).startsWith(want), { state: () => focusKey(page) });
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

  test('j/k move line by line; k stays on the first line; Escape clears focus; k from nothing goes to the end', async ({ page, request }) => {
    const paths = await treePaths(page);
    const first = paths[0].replace('.', '\\.');
    await expect.poll(() => focusedDiff(page)).toEqual([]);

    // j focuses the first line of the first file; j/k step one line.
    await page.keyboard.press('j');
    await expect.poll(() => focusedDiff(page)).toEqual([expect.stringMatching(new RegExp(`^${first}:1:`))]);
    expect(await step(page, 'j')).toMatch(new RegExp(`^${first}:2:`));
    await expect.poll(() => focusedDiff(page)).toHaveLength(1);
    const top = await step(page, 'k');
    expect(top).toMatch(new RegExp(`^${first}:1:`));
    await expect.poll(() => focusedDiff(page)).toHaveLength(1);

    // k on the first line stays there: the next j goes to line 2.
    await page.keyboard.press('k');
    expect(await step(page, 'j')).toMatch(new RegExp(`^${first}:2:`));

    // Escape with no form open clears focus.
    await page.keyboard.press('Escape');
    await expect.poll(() => focusedDiff(page)).toEqual([]);

    // k with no focus goes to the last line of the last file.
    const last = paths[paths.length - 1];
    const diff = await (await request.get(`/api/file/diff?path=${encodeURIComponent(last)}`)).json();
    const hunks = diff.hunks as Array<{ Lines: Array<{ Type: string; NewNum: number }> }>;
    const lastLine = hunks[hunks.length - 1].Lines.filter(l => l.Type !== 'del').pop()!;
    await page.keyboard.press('k');
    await expect.poll(() => focusedDiff(page)).toEqual([expect.stringMatching(new RegExp(`^${last.replace('.', '\\.')}:${lastLine.NewNum}:`))]);

    // Multiple quick j presses from no focus move forward sequentially.
    await page.keyboard.press('Escape');
    await expect.poll(() => focusedDiff(page)).toEqual([]);
    await page.keyboard.press('j');
    await page.keyboard.press('j');
    await page.keyboard.press('j');
    await expect.poll(() => focusedDiff(page)).toEqual([expect.stringMatching(new RegExp(`^${first}:3:`))]);
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
    const box = await reviewScroller(page).boundingBox();
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

  test('j/k resumes after canceling a comment form with Escape and after submitting one', async ({ page, request }) => {
    for (let i = 0; i < 17; i++) await step(page, 'j');
    await expect.poll(() => focusedDiff(page)).toHaveLength(1);
    const next = await focusKey(page);
    const target = await step(page, 'k');
    const form = page.locator('#filesContainer .comment-form');

    // Cancel with Escape: focus is still on the same line (not reset to top).
    await page.keyboard.press('c');
    await expect(form.locator('textarea')).toBeFocused();
    await form.locator('textarea').press('Escape');
    await expect(form).toHaveCount(0);
    await expect.poll(() => focusKey(page)).toBe(target);
    expect(await step(page, 'j')).toBe(next);
    expect(await step(page, 'k')).toBe(target);

    // Submit: the comment lands on the focused line, and focus stays there.
    const [path, line] = target.split(':');
    await page.keyboard.press('c');
    const textarea = form.locator('textarea');
    await expect(textarea).toBeFocused();
    await textarea.fill('resume focus after submit');
    await form.locator('.btn-primary').click();
    await expect(form).toHaveCount(0);
    await expect(page.locator('.comment-card').filter({ hasText: 'resume focus after submit' })).toBeVisible();
    await expect.poll(async () => {
      const comments = await (await request.get(`/api/file/comments?path=${encodeURIComponent(path)}`)).json() as Array<{ body: string; end_line: number }>;
      return comments.find(c => c.body === 'resume focus after submit')?.end_line;
    }).toBe(Number(line));
    await expect.poll(() => focusKey(page)).toBe(target);
    expect(await step(page, 'j')).toBe(next);
  });
});

// ============================================================
// Comment shortcuts (c, e, d) and form Escape/Cancel — Diff
// ============================================================
test.describe('Keyboard Comment Shortcuts — Diff', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await clearFocus(page);
  });

  test('c opens a form; Escape and Cancel close it (confirming only a non-empty Escape); e edits and d deletes', async ({ page, request }) => {
    const [first] = await treePaths(page);
    const focused = await step(page, 'j');
    expect(focused.startsWith(`${first}:1:`)).toBe(true);
    const form = page.locator('#filesContainer .comment-form');
    const textarea = form.locator('textarea');
    let dialogs = 0;
    page.on('dialog', () => { dialogs++; });

    // c opens a form on the focused line, in that line's file.
    await page.keyboard.press('c');
    await expect(form).toBeVisible();
    await expect(textarea).toBeFocused();
    await expect(form.locator('.comment-form-header')).toHaveText('Comment on Line 1');
    await expect(page.locator('diffs-container').filter({
      has: page.locator(`.pierre-file-header[data-file-path="${first}"]`),
    }).locator('.comment-form')).toHaveCount(1);

    // Escape on an empty form closes silently (no confirm).
    await textarea.press('Escape');
    await expect(form).toHaveCount(0);
    expect(dialogs).toBe(0);

    // Escape on a non-empty form asks first: dismiss keeps it, accept closes.
    await page.keyboard.press('c');
    await expect(textarea).toBeFocused();
    await textarea.fill('important draft I do not want to lose');
    page.once('dialog', (dialog) => {
      expect(dialog.type()).toBe('confirm');
      void dialog.dismiss();
    });
    await textarea.press('Escape');
    await expect.poll(() => dialogs).toBe(1);
    await expect(form).toBeVisible();
    await expect(textarea).toHaveValue('important draft I do not want to lose');
    page.once('dialog', (dialog) => {
      expect(dialog.type()).toBe('confirm');
      void dialog.accept();
    });
    await textarea.press('Escape');
    await expect(form).toHaveCount(0);
    expect(dialogs).toBe(2);

    // Cancel is an explicit, labeled discard action — no prompt.
    await page.keyboard.press('c');
    await textarea.fill('draft');
    await form.locator('button', { hasText: 'Cancel' }).click();
    await expect(form).toHaveCount(0);
    expect(dialogs).toBe(2);

    // Commenting through the gutter leaves keyboard focus on the commented
    // line, so e / d act on that comment.
    const item = await goSection(page);
    const lineForm = await openLineComment(page, item, 23);
    await lineForm.locator('textarea').fill('Edit me via shortcut');
    await lineForm.locator('.btn-primary').click();
    const card = item.locator('.comment-card', { hasText: 'Edit me via shortcut' });
    await expect(card).toBeVisible();

    await page.keyboard.press('e');
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('Edit me via shortcut');
    await form.locator('button', { hasText: 'Cancel' }).click();
    await expect(form).toHaveCount(0);

    await page.keyboard.press('d');
    await expect(card).toHaveCount(0);
    await expect.poll(async () => (await (await request.get('/api/file/comments?path=server.go')).json()).length).toBe(0);
  });
});

// ============================================================
// Markdown Document View: j/k, c/e/d, Escape, visual line mode (V)
// ============================================================
test.describe('Keyboard — Markdown Document View', () => {
  let doc: Locator;

  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    doc = await switchToDocumentView(page);
    await clearFocus(page);
  });

  test('j/k navigate blocks; c, e and d act on the focused block; Escape clears block focus', async ({ page, request }) => {
    const firstBlock = await focusDocBlock(page, 1);
    await expect(firstBlock).toHaveAttribute('data-block-index', '0');
    // No diff line is focused while a document block is.
    await expect.poll(() => focusedDiff(page)).toEqual([]);
    const focused = doc.locator('.line-block.kb-nav.focused');
    await page.keyboard.press('j');
    await expect(focused).toHaveCount(1);
    await expect(focused).toHaveAttribute('data-block-index', '1');
    await page.keyboard.press('k');
    await expect(focused).toHaveAttribute('data-block-index', '0');
    await expect(focused).toHaveCount(1);

    // c opens a form on the focused block.
    await page.keyboard.press('c');
    const form = doc.locator('.comment-form');
    await expect(form).toBeVisible();
    await expect(form.locator('textarea')).toBeFocused();
    await expect(form.locator('.comment-form-header')).toHaveText('Comment on Line 1');
    await form.locator('textarea').fill('Edit this markdown comment');
    await form.locator('.btn-primary').click();
    const card = doc.locator('.comment-card', { hasText: 'Edit this markdown comment' });
    await expect(card).toBeVisible();
    await expect.poll(async () => (await (await request.get('/api/file/comments?path=plan.md')).json()).length).toBe(1);

    // Focus stays on the commented block, so e edits its comment.
    await expect(focused).toHaveAttribute('data-block-index', '0');
    await page.keyboard.press('e');
    const editTextarea = page.locator('#filesContainer .comment-form textarea');
    await expect(editTextarea).toBeVisible();
    await expect(editTextarea).toHaveValue('Edit this markdown comment');
    await doc.locator('.comment-form button', { hasText: 'Cancel' }).click();
    await expect(doc.locator('.comment-form')).toHaveCount(0);

    // d deletes it.
    await expect(focused).toHaveAttribute('data-block-index', '0');
    await page.keyboard.press('d');
    await expect(page.locator('#filesContainer .comment-card', { hasText: 'Edit this markdown comment' })).toHaveCount(0);

    // Escape clears document block focus; a fresh j starts over from the top
    // of the review.
    await expect(focused).toHaveAttribute('data-block-index', '0');
    await page.keyboard.press('Escape');
    await expect(page.locator('.kb-nav.focused')).toHaveCount(0);
    const [first] = await treePaths(page);
    expect((await step(page, 'j')).startsWith(`${first}:1:`)).toBe(true);
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

  test('V again exits visual mode; Escape clears the selection and keeps focus', async ({ page }) => {
    const focused = doc.locator('.line-block.kb-nav.focused');
    const selected = doc.locator('.line-block.selected');
    await focusDocBlock(page, 1);

    // V toggles: the selection goes, focus stays, and j moves without extending.
    await page.keyboard.press('Shift+V');
    await page.keyboard.press('j');
    await expect(selected).toHaveCount(2);
    await page.keyboard.press('Shift+V');
    await expect(selected).toHaveCount(0);
    await expect(focused).toHaveAttribute('data-block-index', '1');
    await page.keyboard.press('j');
    await expect(focused).toHaveAttribute('data-block-index', '2');
    await expect(selected).toHaveCount(0);

    // Escape clears an active selection; focus stays at the furthest
    // expansion, and j continues from there.
    await page.keyboard.press('Shift+V');
    await page.keyboard.press('j');
    await page.keyboard.press('j');
    await expect(focused).toHaveAttribute('data-block-index', '4');
    await expect(selected).toHaveCount(3);
    await page.keyboard.press('Escape');
    await expect(selected).toHaveCount(0);
    await expect(focused).toHaveCount(1);
    await expect(focused).toHaveAttribute('data-block-index', '4');
    await page.keyboard.press('j');
    await expect(focused).toHaveAttribute('data-block-index', '5');
  });
});

// ============================================================
// UI shortcuts. Opening/closing the settings panel with ? and Escape:
// settings-panel.spec.ts ("? key opens panel to Shortcuts tab", "? key
// toggles shortcuts tab when panel is open on shortcuts", "Escape closes
// the panel").
// ============================================================
test.describe('Keyboard UI shortcuts', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('shortcuts type into a focused textarea; t is a no-op in git mode', async ({ page, request }) => {
    // A comment for ] / [ to land on if they were (wrongly) handled.
    const existing = await addComment(request, 'plan.md', 1, 'Existing comment');
    await loadPage(page);
    await clearFocus(page);
    await recordClassAdditions(page, 'comment-nav-highlight');

    const focused = await step(page, 'j');
    await page.keyboard.press('c');
    const textarea = page.locator('#filesContainer .comment-form textarea');
    await expect(textarea).toBeFocused();

    // Typed into the textarea: no navigation, no settings panel, no comment jump.
    await textarea.pressSequentially('jjj?][');
    await expect(textarea).toHaveValue('jjj?][');
    await expect.poll(() => focusKey(page)).toBe(focused);
    await expect(page.locator('.settings-overlay')).not.toHaveClass(/active/);

    // Close the form; the same keys now act (proving they were not just slow).
    page.once('dialog', (dialog) => void dialog.accept());
    await textarea.press('Escape');
    await expect(page.locator('#filesContainer .comment-form')).toHaveCount(0);
    expect(await classAdditions(page, 'comment-nav-highlight')).toEqual([]);
    await expectClassAddedTo(page, 'comment-nav-highlight', () => page.keyboard.press(']'), existing.id);

    // t is a no-op in git mode (the TOC is disabled). Keys are handled in
    // order, so once ? has opened the settings panel, t has been handled.
    const toc = page.locator('#toc');
    await expect(toc).toHaveClass(/toc-hidden/);
    await clearFocus(page);
    await page.keyboard.press('t');
    await page.keyboard.press('?');
    await expect(page.locator('.settings-overlay')).toHaveClass(/active/);
    await expect(toc).toHaveClass(/toc-hidden/);
  });

  test('Shift+F triggers finish review (shows waiting overlay)', async ({ page }) => {
    await loadPage(page);
    await clearFocus(page);
    const waitingOverlay = page.locator('#waitingOverlay');
    await expect(waitingOverlay).not.toHaveClass(/active/);

    await page.keyboard.press('Shift+F');

    // The waiting overlay should become active after the finish API call
    await expect(waitingOverlay).toHaveClass(/active/);
  });
});

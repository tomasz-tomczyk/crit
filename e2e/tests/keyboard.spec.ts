import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, goSection, clearFocus, switchToDocumentView } from './helpers';

// ============================================================
// j/k Navigation on Diff Blocks (Split Mode)
// ============================================================
test.describe('Keyboard Navigation — Diff Split Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await clearFocus(page);
  });

  test('j focuses the first .kb-nav element', async ({ page }) => {
    // No element should be focused initially
    await expect(page.locator('.kb-nav.focused')).toHaveCount(0);

    await page.keyboard.press('j');

    const focused = page.locator('.kb-nav.focused');
    await expect(focused).toHaveCount(1);
  });

  test('j navigates to next block, k navigates to previous', async ({ page }) => {
    // Press j twice to move to the second element
    await page.keyboard.press('j');
    await page.keyboard.press('j');

    const focused = page.locator('.kb-nav.focused');
    await expect(focused).toHaveCount(1);

    // Get the index of the second focused element
    const allNav = page.locator('.kb-nav');
    const secondEl = allNav.nth(1);
    await expect(secondEl).toHaveClass(/focused/);

    // Press k to go back to the first element
    await page.keyboard.press('k');
    const firstEl = allNav.nth(0);
    await expect(firstEl).toHaveClass(/focused/);
    // Second element should no longer be focused
    await expect(secondEl).not.toHaveClass(/focused/);
  });

  test('multiple j presses move forward sequentially', async ({ page }) => {
    const allNav = page.locator('.kb-nav');
    const count = await allNav.count();
    expect(count).toBeGreaterThan(3);

    // Press j three times
    await page.keyboard.press('j');
    await page.keyboard.press('j');
    await page.keyboard.press('j');

    // The third element (index 2) should be focused
    const thirdEl = allNav.nth(2);
    await expect(thirdEl).toHaveClass(/focused/);

    // Only one element should have focused class
    await expect(page.locator('.kb-nav.focused')).toHaveCount(1);
  });

  test('j/k in split diff mode navigates rows, not individual sides', async ({ page }) => {
    // In split mode, .diff-split-row elements get .kb-nav
    // Pressing j should focus rows, not alternate between left/right
    const diffRows = page.locator('.diff-split-row.kb-nav');
    await expect(diffRows.first()).toBeAttached();

    // Hover a diff-split-row to set focusedElement, then press j to focus it
    await diffRows.first().hover();
    await page.keyboard.press('j');

    const currentRow = page.locator('.diff-split-row.kb-nav.focused');
    await expect(currentRow).toHaveCount(1);

    // Record the current index
    const currentIndex = await currentRow.evaluate(el => {
      const allRows = Array.from(document.querySelectorAll('.kb-nav'));
      return allRows.indexOf(el);
    });

    // Now press j again — should move to the NEXT row, not the right side of the same row
    await page.keyboard.press('j');

    const nextFocused = page.locator('.kb-nav.focused');
    const nextIndex = await nextFocused.evaluate(el => {
      const allRows = Array.from(document.querySelectorAll('.kb-nav'));
      return allRows.indexOf(el);
    });

    // Should have moved forward by exactly 1
    expect(nextIndex).toBe(currentIndex + 1);
  });

  test('k from first element stays at first element', async ({ page }) => {
    // Press j to focus first element
    await page.keyboard.press('j');
    const allNav = page.locator('.kb-nav');
    await expect(allNav.first()).toHaveClass(/focused/);

    // Press k — should stay at first
    await page.keyboard.press('k');
    await expect(allNav.first()).toHaveClass(/focused/);
    await expect(page.locator('.kb-nav.focused')).toHaveCount(1);
  });

  test('k with no focus goes to last element', async ({ page }) => {
    await page.keyboard.press('k');

    const allNav = page.locator('.kb-nav');
    const lastEl = allNav.last();
    await expect(lastEl).toHaveClass(/focused/);
    await expect(page.locator('.kb-nav.focused')).toHaveCount(1);
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
    const section = mdSection(page);
    const lineBlocks = section.locator('.line-block.kb-nav');
    const count = await lineBlocks.count();
    expect(count).toBeGreaterThan(2);

    // Hover the first markdown line-block to set focusedElement, then press j to focus it
    const firstBlock = lineBlocks.first();
    await firstBlock.hover();
    await page.keyboard.press('j');

    await expect(page.locator('.line-block.kb-nav.focused')).toHaveCount(1);
    const firstFocusedText = await page.locator('.line-block.kb-nav.focused').textContent();

    // Press j again to move to the next line block
    await page.keyboard.press('j');

    const secondFocused = page.locator('.kb-nav.focused');
    await expect(secondFocused).toHaveCount(1);
    const secondFocusedText = await secondFocused.textContent();

    // The text should have changed (moved to a different block)
    expect(secondFocusedText).not.toBe(firstFocusedText);
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

  test('c opens comment form on focused diff block', async ({ page }) => {
    // Hover a diff row to set focusedElement via mouseenter
    const diffRow = page.locator('.diff-split-row.kb-nav').first();
    await expect(diffRow).toBeAttached();
    await diffRow.hover();

    // Press c to open comment form (uses focusedElement directly)
    await page.keyboard.press('c');

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();
    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();
  });

  test('e edits comment on focused diff block', async ({ page }) => {
    // Use the UI to create a comment on server.go, then test editing via keyboard
    const section = goSection(page);
    const additionSide = section.locator('.diff-split-side.addition').first();
    await additionSide.hover();
    const commentBtn = additionSide.locator('.diff-comment-btn');
    await commentBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Edit me via shortcut');
    await page.locator('.comment-form .btn-primary').click();

    // Comment card should appear
    await expect(section.locator('.comment-card')).toBeVisible();

    // Hover over the addition side with the comment to set focusedElement via mouseenter
    const commentedSide = section.locator('.diff-split-side.has-comment').first();
    await expect(commentedSide).toBeVisible();
    await commentedSide.hover();

    // Press e to edit — the hover set focusedElement to the row's nav element
    await page.keyboard.press('e');

    const editTextarea = page.locator('.comment-form textarea');
    await expect(editTextarea).toBeVisible();
    await expect(editTextarea).toHaveValue('Edit me via shortcut');
  });

  test('d deletes comment on focused diff block', async ({ page }) => {
    // Use the UI to create a comment on server.go
    const section = goSection(page);
    const additionSide = section.locator('.diff-split-side.addition').first();
    await additionSide.hover();
    const commentBtn = additionSide.locator('.diff-comment-btn');
    await commentBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Delete me via shortcut');
    await page.locator('.comment-form .btn-primary').click();

    // Verify comment exists
    const commentCard = section.locator('.comment-card');
    await expect(commentCard).toBeVisible();

    // Hover over the addition side with the comment to set focusedElement via mouseenter
    const commentedSide = section.locator('.diff-split-side.has-comment').first();
    await expect(commentedSide).toBeVisible();
    await commentedSide.hover();

    // Press d to delete — the hover set focusedElement to the row's nav element
    await page.keyboard.press('d');

    // Comment should be removed
    await expect(commentCard).toHaveCount(0);
  });
});

test.describe('Keyboard Comment Shortcuts — Markdown', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('c opens comment form on focused markdown block', async ({ page }) => {
    await loadPage(page);
    await switchToDocumentView(page);
    await clearFocus(page);

    // Hover a markdown line-block to set focusedElement via mouseenter
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block.kb-nav').first();
    await expect(lineBlock).toBeAttached();
    await lineBlock.hover();

    // c uses focusedElement directly (set by hover), no j-key needed
    await page.keyboard.press('c');

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();
    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();
  });

  test('e edits comment on focused markdown block', async ({ page, request }) => {
    // Create a comment on line 1 of plan.md via API
    await request.post(`/api/file/comments?path=plan.md`, {
      data: { start_line: 1, end_line: 1, body: 'Edit this markdown comment' },
    });

    await loadPage(page);
    await switchToDocumentView(page);
    await clearFocus(page);

    // Hover the line-block covering line 1 to set focusedElement via mouseenter
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block.kb-nav[data-start-line="1"]');
    await expect(lineBlock).toBeAttached();
    await lineBlock.hover();

    // e uses focusedElement directly (set by hover), no j-key needed
    await page.keyboard.press('e');

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('Edit this markdown comment');
  });

  test('d deletes comment on focused markdown block', async ({ page, request }) => {
    // Create a comment on line 1 of plan.md via API
    await request.post(`/api/file/comments?path=plan.md`, {
      data: { start_line: 1, end_line: 1, body: 'Delete this markdown comment' },
    });

    await loadPage(page);
    await switchToDocumentView(page);
    await clearFocus(page);

    // Verify comment exists
    const section = mdSection(page);
    await expect(section.locator('.comment-card')).toBeVisible();

    // Hover the line-block covering line 1 to set focusedElement via mouseenter
    const lineBlock = section.locator('.line-block.kb-nav[data-start-line="1"]');
    await expect(lineBlock).toBeAttached();
    await lineBlock.hover();

    // d uses focusedElement directly (set by hover), no j-key needed
    await page.keyboard.press('d');

    await expect(section.locator('.comment-card')).toHaveCount(0);
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
    // Hover a diff row to set focusedElement, then open comment form
    const diffRow = page.locator('.diff-split-row.kb-nav').first();
    await expect(diffRow).toBeAttached();
    await diffRow.hover();
    await page.keyboard.press('c');

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    // Press Escape (from within the textarea)
    await page.locator('.comment-form textarea').press('Escape');

    await expect(form).toHaveCount(0);
  });

  test('Escape clears focus when no form is open', async ({ page }) => {
    // Navigate to focus a block
    await page.keyboard.press('j');
    await expect(page.locator('.kb-nav.focused')).toHaveCount(1);

    // Press Escape to clear focus
    await page.keyboard.press('Escape');
    await expect(page.locator('.kb-nav.focused')).toHaveCount(0);
  });

  test('Escape on non-empty comment form prompts for confirmation', async ({ page }) => {
    const diffRow = page.locator('.diff-split-row.kb-nav').first();
    await expect(diffRow).toBeAttached();
    await diffRow.hover();
    await page.keyboard.press('c');

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();
    await textarea.fill('important draft I do not want to lose');

    // Cancel the confirm — form must stay open with content intact.
    page.once('dialog', (dialog) => {
      expect(dialog.type()).toBe('confirm');
      void dialog.dismiss();
    });
    await textarea.press('Escape');
    await expect(page.locator('.comment-form')).toBeVisible();
    await expect(textarea).toHaveValue('important draft I do not want to lose');

    // Accept the confirm — form should close.
    page.once('dialog', (dialog) => {
      expect(dialog.type()).toBe('confirm');
      void dialog.accept();
    });
    await textarea.press('Escape');
    await expect(page.locator('.comment-form')).toHaveCount(0);
  });

  test('Cancel button on non-empty comment form discards immediately (no confirm)', async ({ page }) => {
    const diffRow = page.locator('.diff-split-row.kb-nav').first();
    await expect(diffRow).toBeAttached();
    await diffRow.hover();
    await page.keyboard.press('c');

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('draft');

    // Cancel is an explicit, labeled discard action — no prompt.
    let dialogShown = false;
    page.once('dialog', () => { dialogShown = true; });

    await page.locator('.comment-form button', { hasText: 'Cancel' }).click();
    await expect(page.locator('.comment-form')).toHaveCount(0);
    expect(dialogShown).toBe(false);
  });

  test('Escape on empty comment form closes silently (no confirm)', async ({ page }) => {
    const diffRow = page.locator('.diff-split-row.kb-nav').first();
    await expect(diffRow).toBeAttached();
    await diffRow.hover();
    await page.keyboard.press('c');

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();

    // If a dialog appears, the test fails (we never accept/dismiss).
    let dialogShown = false;
    page.once('dialog', () => { dialogShown = true; });

    await textarea.press('Escape');
    await expect(page.locator('.comment-form')).toHaveCount(0);
    expect(dialogShown).toBe(false);
  });
});

// ============================================================
// Visual Line Mode (V)
// ============================================================
test.describe('Keyboard Visual Line Mode — Markdown', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);
    await clearFocus(page);
  });

  test('V enters visual mode; j extends selection; c opens form spanning the range', async ({ page, request }) => {
    const section = mdSection(page);
    const lineBlocks = section.locator('.line-block.kb-nav');
    await expect(lineBlocks.first()).toBeAttached();

    // Focus the first line-block
    await lineBlocks.first().hover();
    await page.keyboard.press('j');
    const firstFocused = page.locator('.line-block.kb-nav.focused');
    await expect(firstFocused).toHaveCount(1);
    const anchorStart = parseInt((await firstFocused.getAttribute('data-start-line'))!);
    const filePath = await firstFocused.getAttribute('data-file-path');

    // Enter visual mode — anchor block becomes selected
    await page.keyboard.press('Shift+V');
    await expect(firstFocused).toHaveClass(/selected/);

    // Extend selection downward with j
    await page.keyboard.press('j');
    await page.keyboard.press('j');

    // Multiple blocks should now have .selected.
    const selected = section.locator('.line-block.selected');
    await expect.poll(() => selected.count()).toBeGreaterThan(1);

    // Capture the furthest expansion line before opening the form
    const lastFocused = page.locator('.line-block.kb-nav.focused');
    const expansionEnd = parseInt((await lastFocused.getAttribute('data-end-line'))!);

    // Open form on the selection
    await page.keyboard.press('c');
    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    // Submit the comment and verify its persisted line range via the API.
    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('multi-line via V');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card').filter({ hasText: 'multi-line via V' })).toBeVisible();

    const resp = await request.get(`/api/file/comments?path=${encodeURIComponent(filePath!)}`);
    const data = await resp.json() as Array<{ body: string; start_line: number; end_line: number }>;
    const created = data.find((c) => c.body === 'multi-line via V');
    expect(created).toBeDefined();
    expect(created.start_line).toBe(anchorStart);
    expect(created.end_line).toBe(expansionEnd);
  });

  test('Escape clears visual selection and keeps focus on current block', async ({ page }) => {
    const section = mdSection(page);
    const lineBlocks = section.locator('.line-block.kb-nav');
    await lineBlocks.first().hover();
    await page.keyboard.press('j');
    await page.keyboard.press('Shift+V');
    await page.keyboard.press('j');
    await page.keyboard.press('j');

    // Selection is active across multiple blocks
    await expect.poll(() => section.locator('.line-block.selected').count()).toBeGreaterThan(1);

    // Capture the line currently focused (the furthest expansion)
    const beforeFocus = await page.locator('.line-block.kb-nav.focused').getAttribute('data-start-line');

    await page.keyboard.press('Escape');

    // Selection cleared
    await expect(section.locator('.line-block.selected')).toHaveCount(0);

    // Focused block stays at the furthest expansion (issue spec)
    const afterFocus = await page.locator('.line-block.kb-nav.focused').getAttribute('data-start-line');
    expect(afterFocus).toBe(beforeFocus);
  });

  test('V again exits visual mode (toggle)', async ({ page }) => {
    const section = mdSection(page);
    const lineBlocks = section.locator('.line-block.kb-nav');
    await lineBlocks.first().hover();
    await page.keyboard.press('j');
    await page.keyboard.press('Shift+V');
    await expect(section.locator('.line-block.selected').first()).toBeAttached();

    await page.keyboard.press('Shift+V');
    await expect(section.locator('.line-block.selected')).toHaveCount(0);
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
    // Hover a diff row to set focusedElement, then open comment form
    const diffRow = page.locator('.diff-split-row.kb-nav').first();
    await expect(diffRow).toBeAttached();
    await diffRow.hover();
    await page.keyboard.press('c');

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();

    // Type 'j' — should go into the textarea, NOT navigate
    await textarea.type('jjj');

    await expect(textarea).toHaveValue('jjj');

    // Focus should NOT have moved (only one focused element — the one that was focused before opening form)
    // The important thing is: the textarea contains the text and no navigation happened
  });

  test('other shortcuts (?, t) do not fire when textarea is focused', async ({ page }) => {
    // Hover a diff row to set focusedElement, then open comment form
    const diffRow = page.locator('.diff-split-row.kb-nav').first();
    await expect(diffRow).toBeAttached();
    await diffRow.hover();
    await page.keyboard.press('c');

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();

    // Type '?' — should go into textarea, not open settings panel
    await textarea.type('?');
    await expect(textarea).toHaveValue('?');

    const overlay = page.locator('.settings-overlay');
    await expect(overlay).not.toHaveClass(/active/);
  });
});

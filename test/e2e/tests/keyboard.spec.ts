import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, goSection, clearFocus, switchToDocumentView, focusKbNavByJ, focusKbNavElement, focusInsideFile } from './helpers';

async function focusMarkdownBlockWithStartLine(page: import('@playwright/test').Page, startLine: string) {
  const block = (await mdSection(page)).locator(`.line-block.kb-nav[data-start-line="${startLine}"]`);
  await expect(block).toBeAttached();
  await focusKbNavElement(page, block);
}

// ============================================================
// j/k Navigation on Diff Blocks (Split Mode)
// ============================================================
test.describe('Keyboard Navigation — Diff Split Mode', () => {
  // navigateVirtualDiffRow applies focus asynchronously after scrollToRow — wait for key change.
  async function pressJUntilKeyChanges(page: import('@playwright/test').Page, prevKey: string | null) {
    await page.keyboard.press('j');
    await expect.poll(async () =>
      page.locator('[id="file-section-server.go"] .kb-nav.focused').getAttribute('data-virtual-key')
    ).not.toBe(prevKey);
  }


  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    const section = await goSection(page);
    await expect(section.locator('.kb-nav').first()).toBeVisible();
    await focusInsideFile(page, 'server.go');
    await expect(page.locator('[id="file-section-server.go"] .kb-nav.focused')).toHaveCount(1);
  });

  test('j focuses a kb-nav row inside the mounted file', async ({ page }) => {
    await expect(page.locator('.kb-nav.focused')).toHaveCount(1);
    await expect(page.locator('[id="file-section-server.go"] .kb-nav.focused')).toHaveCount(1);
  });

  test('j navigates to next block, k navigates to previous', async ({ page }) => {
    const focused = page.locator('[id="file-section-server.go"] .kb-nav.focused');
    const before = await focused.getAttribute('data-virtual-key');
    expect(before).toBeTruthy();

    await pressJUntilKeyChanges(page, before);
    const mid = await focused.getAttribute('data-virtual-key');
    expect(mid).toBeTruthy();

    await page.keyboard.press('k');
    await expect.poll(async () => focused.getAttribute('data-virtual-key')).toBe(before);
  });

  test('multiple j presses move forward sequentially', async ({ page }) => {
    const focused = page.locator('[id="file-section-server.go"] .kb-nav.focused');
    const keys: (string | null)[] = [await focused.getAttribute('data-virtual-key')];
    await pressJUntilKeyChanges(page, keys[0]);
    keys.push(await focused.getAttribute('data-virtual-key'));
    await pressJUntilKeyChanges(page, keys[1]);
    keys.push(await focused.getAttribute('data-virtual-key'));
    expect(new Set(keys).size).toBe(3);
    await expect(page.locator('.kb-nav.focused')).toHaveCount(1);
  });

  test('j/k in split diff mode navigates rows, not individual sides', async ({ page }) => {
    const focused = page.locator('[id="file-section-server.go"] .diff-split-row.kb-nav.focused');
    await expect(focused).toHaveCount(1);
    const before = await focused.getAttribute('data-virtual-key');
    await pressJUntilKeyChanges(page, before);
    await expect(page.locator('.diff-split-row.kb-nav.focused')).toHaveCount(1);
  });

  test('j/k resumes after canceling comment form with Escape', async ({ page }) => {
    const focused = page.locator('[id="file-section-server.go"] .kb-nav.focused');
    let key = await focused.getAttribute('data-virtual-key');
    await pressJUntilKeyChanges(page, key);
    key = await focused.getAttribute('data-virtual-key');
    await pressJUntilKeyChanges(page, key);
    const targetKey = await focused.getAttribute('data-virtual-key');
    expect(targetKey).toBeTruthy();

    await page.keyboard.press('c');
    await expect(page.locator('.comment-form')).toBeVisible();
    await page.locator('.comment-form textarea').press('Escape');
    await expect(page.locator('.comment-form')).toHaveCount(0);

    await expect.poll(async () => focused.getAttribute('data-virtual-key')).toBe(targetKey);

    await pressJUntilKeyChanges(page, targetKey);
  });

  test('j/k resumes after submitting a new comment', async ({ page }) => {
    const focused = page.locator('[id="file-section-server.go"] .kb-nav.focused');
    let key = await focused.getAttribute('data-virtual-key');
    await pressJUntilKeyChanges(page, key);
    key = await focused.getAttribute('data-virtual-key');
    await pressJUntilKeyChanges(page, key);
    const targetKey = await focused.getAttribute('data-virtual-key');
    expect(targetKey).toBeTruthy();

    await page.keyboard.press('c');
    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await textarea.fill('resume focus after submit');
    await page.locator('.comment-form .btn-primary').click();
    await expect(page.locator('.comment-form')).toHaveCount(0);
    await expect(page.locator('.comment-card').filter({ hasText: 'resume focus after submit' })).toBeVisible();

    await expect.poll(async () => focused.getAttribute('data-virtual-key')).toBe(targetKey);
    await pressJUntilKeyChanges(page, targetKey);
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
    const section = await mdSection(page);
    const lineBlocks = section.locator('.line-block.kb-nav');
    const count = await lineBlocks.count();
    expect(count).toBeGreaterThan(2);

    // Focus the first markdown line-block, then advance one block with j
    const firstBlock = lineBlocks.first();
    await focusKbNavElement(page, firstBlock);

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
    await focusKbNavByJ(page, 1);

    // Press c to open comment form (uses keyboard-focused element)
    await page.keyboard.press('c');

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();
    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();
  });

  test('e edits comment on focused diff block', async ({ page }) => {
    // Use the UI to create a comment on server.go, then test editing via keyboard
    const section = await goSection(page);
    const additionSide = section.locator('.diff-split-side.addition').first();
    const commentedRow = additionSide.locator('xpath=ancestor::*[contains(@class,"diff-split-row") and contains(@class,"kb-nav")][1]');
    await additionSide.hover();
    const commentBtn = additionSide.locator('.diff-comment-btn');
    await commentBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Edit me via shortcut');
    await page.locator('.comment-form .btn-primary').click();

    // Comment card should appear
    await expect(section.locator('.comment-card')).toBeVisible();

    // Submit restores keyboard focus to the commented line
    await page.keyboard.press('e');

    const editTextarea = page.locator('.comment-form textarea');
    await expect(editTextarea).toBeVisible();
    await expect(editTextarea).toHaveValue('Edit me via shortcut');
  });

  test('d deletes comment on focused diff block', async ({ page }) => {
    // Use the UI to create a comment on server.go
    const section = await goSection(page);
    const additionSide = section.locator('.diff-split-side.addition').first();
    const commentedRow = additionSide.locator('xpath=ancestor::*[contains(@class,"diff-split-row") and contains(@class,"kb-nav")][1]');
    await additionSide.hover();
    const commentBtn = additionSide.locator('.diff-comment-btn');
    await commentBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Delete me via shortcut');
    await page.locator('.comment-form .btn-primary').click();

    // Verify comment exists
    const commentCard = section.locator('.comment-card');
    await expect(commentCard).toBeVisible();

    // Submit restores keyboard focus to the commented line
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

    await focusKbNavElement(page, (await mdSection(page)).locator('.line-block.kb-nav').first());

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

    await focusMarkdownBlockWithStartLine(page, '1');

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
    const section = await mdSection(page);
    await expect(section.locator('.comment-card')).toBeVisible();

    await focusMarkdownBlockWithStartLine(page, '1');

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
    await focusKbNavByJ(page, 1);
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
    await focusKbNavByJ(page, 1);
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
    await focusKbNavByJ(page, 1);
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
    await focusKbNavByJ(page, 1);
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
    const section = await mdSection(page);
    const lineBlocks = section.locator('.line-block.kb-nav');
    await expect(lineBlocks.first()).toBeAttached();

    // Focus the first line-block with j/k
    await focusKbNavElement(page, lineBlocks.first());
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
    const section = await mdSection(page);
    const lineBlocks = section.locator('.line-block.kb-nav');
    await focusKbNavElement(page, lineBlocks.first());
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
    const section = await mdSection(page);
    const lineBlocks = section.locator('.line-block.kb-nav');
    await focusKbNavElement(page, lineBlocks.first());
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
    await focusKbNavByJ(page, 1);
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
    await focusKbNavByJ(page, 1);
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

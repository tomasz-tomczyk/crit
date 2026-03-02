import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

// Helper: clean all comments via API so each test starts fresh.
async function clearAllComments(request: APIRequestContext) {
  const sessionRes = await request.get('/api/session');
  const session = await sessionRes.json();
  for (const f of (session.files || [])) {
    const commentsRes = await request.get(`/api/file/comments?path=${encodeURIComponent(f.path)}`);
    const comments = await commentsRes.json();
    if (Array.isArray(comments)) {
      for (const c of comments) {
        await request.delete(`/api/comment/${c.id}?path=${encodeURIComponent(f.path)}`);
      }
    }
  }
}

// Helper: navigate and wait for page load
async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

// Helper: scope selectors to plan.md file section
function mdSection(page: Page) {
  return page.locator('.file-section').filter({ hasText: 'plan.md' });
}

// Helper: switch plan.md to document view (markdown defaults to diff in git mode)
async function switchToDocumentView(page: Page) {
  const section = mdSection(page);
  await expect(section).toBeVisible();
  const docBtn = section.locator('.file-header-toggle .toggle-btn[data-mode="document"]');
  await expect(docBtn).toBeVisible();
  await docBtn.click();
  await expect(section.locator('.document-wrapper')).toBeVisible();
}

// Helper: scope selectors to server.go file section
function goSection(page: Page) {
  return page.locator('#file-section-server\\.go');
}

// Helper: click body to ensure no element has focus (reset keyboard state)
async function clearFocus(page: Page) {
  await page.locator('body').click({ position: { x: 0, y: 0 } });
}

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
    await page.keyboard.press('j');

    const focused = page.locator('.kb-nav.focused');
    await expect(focused).toHaveCount(1);

    // The focused element should be a diff-split-row (in the code files section)
    // or a line-block (if markdown is first). Let's navigate into the diff area.
    const diffRows = page.locator('.diff-split-row.kb-nav');
    const diffRowCount = await diffRows.count();
    expect(diffRowCount).toBeGreaterThan(0);

    // Navigate until we hit a diff-split-row
    let foundDiffRow = false;
    for (let i = 0; i < 30; i++) {
      const focusedEl = page.locator('.kb-nav.focused');
      const tagName = await focusedEl.evaluate(el => el.className);
      if (tagName.includes('diff-split-row')) {
        foundDiffRow = true;
        break;
      }
      await page.keyboard.press('j');
    }
    expect(foundDiffRow).toBe(true);

    // Now press j again — should move to the NEXT row, not the right side of the same row
    const currentRow = page.locator('.diff-split-row.kb-nav.focused');
    const currentIndex = await currentRow.evaluate(el => {
      const allRows = Array.from(document.querySelectorAll('.kb-nav'));
      return allRows.indexOf(el);
    });

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

    // Press j to focus the first navigable element (could be diff row from earlier file)
    // Keep pressing j until we're in a line-block
    let foundLineBlock = false;
    for (let i = 0; i < 50; i++) {
      await page.keyboard.press('j');
      const focused = page.locator('.line-block.kb-nav.focused');
      if (await focused.count() > 0) {
        foundLineBlock = true;
        break;
      }
    }
    expect(foundLineBlock).toBe(true);

    // Press j again to move to the next line block
    const firstFocusedText = await page.locator('.line-block.kb-nav.focused').textContent();
    await page.keyboard.press('j');

    const secondFocused = page.locator('.kb-nav.focused');
    await expect(secondFocused).toHaveCount(1);
    const secondFocusedText = await secondFocused.textContent();

    // If still in markdown area, the text should have changed
    // (or we moved to a different block at minimum)
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
    // Navigate to a diff row
    let foundDiffRow = false;
    for (let i = 0; i < 30; i++) {
      await page.keyboard.press('j');
      const focused = page.locator('.diff-split-row.kb-nav.focused');
      if (await focused.count() > 0) {
        foundDiffRow = true;
        break;
      }
    }
    expect(foundDiffRow).toBe(true);

    // Press c to open comment form
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
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);
    await clearFocus(page);
  });

  test('c opens comment form on focused markdown block', async ({ page }) => {
    // Navigate to a markdown line-block
    let found = false;
    for (let i = 0; i < 50; i++) {
      await page.keyboard.press('j');
      const focused = page.locator('.line-block.kb-nav.focused');
      if (await focused.count() > 0) {
        found = true;
        break;
      }
    }
    expect(found).toBe(true);

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

    // Navigate to the first line-block (line 1)
    let found = false;
    for (let i = 0; i < 50; i++) {
      await page.keyboard.press('j');
      const focused = page.locator('.line-block.kb-nav.focused');
      if (await focused.count() > 0) {
        // Check if the block's data attributes cover line 1
        const startLine = await focused.getAttribute('data-start-line');
        const endLine = await focused.getAttribute('data-end-line');
        if (startLine && endLine) {
          const sl = parseInt(startLine);
          const el = parseInt(endLine);
          if (sl <= 1 && el >= 1) {
            found = true;
            break;
          }
        }
      }
    }
    expect(found).toBe(true);

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

    // Navigate to the first line-block covering line 1
    let found = false;
    for (let i = 0; i < 50; i++) {
      await page.keyboard.press('j');
      const focused = page.locator('.line-block.kb-nav.focused');
      if (await focused.count() > 0) {
        const startLine = await focused.getAttribute('data-start-line');
        const endLine = await focused.getAttribute('data-end-line');
        if (startLine && endLine) {
          const sl = parseInt(startLine);
          const el = parseInt(endLine);
          if (sl <= 1 && el >= 1) {
            found = true;
            break;
          }
        }
      }
    }
    expect(found).toBe(true);

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

  test('? toggles shortcuts overlay', async ({ page }) => {
    const overlay = page.locator('#shortcutsOverlay');

    // Initially not active
    await expect(overlay).not.toHaveClass(/active/);

    // Press ? to open
    await page.keyboard.press('?');
    await expect(overlay).toHaveClass(/active/);

    // Press ? again to close
    await page.keyboard.press('?');
    await expect(overlay).not.toHaveClass(/active/);
  });

  test('Escape closes shortcuts overlay', async ({ page }) => {
    const overlay = page.locator('#shortcutsOverlay');

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

  test('t toggles table of contents (hidden in git mode, but toggles)', async ({ page }) => {
    // In git mode, the tocToggle button is hidden (display: none), so pressing t clicks a hidden button.
    // The toc starts with toc-hidden. Pressing t clicks the toggle button.
    // Since the button is display:none in git mode, the click via keyboard shortcut
    // still fires the click handler. Let's verify toc state changes.
    const toc = page.locator('#toc');

    // Initially has toc-hidden
    await expect(toc).toHaveClass(/toc-hidden/);

    // Press t
    await page.keyboard.press('t');

    // In git mode, the toggle button is hidden, but the 't' shortcut calls .click() on it,
    // which should still toggle the class
    await expect(toc).not.toHaveClass(/toc-hidden/);

    // Press t again to close
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
    // Navigate to a diff row and open comment form
    for (let i = 0; i < 30; i++) {
      await page.keyboard.press('j');
      const focused = page.locator('.diff-split-row.kb-nav.focused');
      if (await focused.count() > 0) break;
    }
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
    // Navigate to a diff row and open comment form
    for (let i = 0; i < 30; i++) {
      await page.keyboard.press('j');
      const focused = page.locator('.diff-split-row.kb-nav.focused');
      if (await focused.count() > 0) break;
    }
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
    // Open a comment form
    for (let i = 0; i < 30; i++) {
      await page.keyboard.press('j');
      const focused = page.locator('.diff-split-row.kb-nav.focused');
      if (await focused.count() > 0) break;
    }
    await page.keyboard.press('c');

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();

    // Type '?' — should go into textarea, not toggle shortcuts overlay
    await textarea.type('?');
    await expect(textarea).toHaveValue('?');

    const overlay = page.locator('#shortcutsOverlay');
    await expect(overlay).not.toHaveClass(/active/);
  });
});

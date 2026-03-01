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

// Helper: click body to ensure no element has focus
async function clearFocus(page: Page) {
  await page.locator('body').click({ position: { x: 0, y: 0 } });
}

// ============================================================
// j/k Navigation on Markdown Blocks (File Mode — document view by default)
// ============================================================
test.describe('Keyboard Navigation — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    // In file mode, plan.md is already in document view
    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await clearFocus(page);
  });

  test('j focuses the first navigable block', async ({ page }) => {
    await expect(page.locator('.kb-nav.focused')).toHaveCount(0);

    await page.keyboard.press('j');

    const focused = page.locator('.kb-nav.focused');
    await expect(focused).toHaveCount(1);
  });

  test('j/k navigate through markdown line-blocks', async ({ page }) => {
    const lineBlocks = page.locator('.line-block.kb-nav');
    const count = await lineBlocks.count();
    expect(count).toBeGreaterThan(2);

    // Press j to focus first block
    await page.keyboard.press('j');
    const firstFocused = page.locator('.kb-nav.focused');
    await expect(firstFocused).toHaveCount(1);
    const firstText = await firstFocused.textContent();

    // Press j to move to second block
    await page.keyboard.press('j');
    const secondFocused = page.locator('.kb-nav.focused');
    await expect(secondFocused).toHaveCount(1);
    const secondText = await secondFocused.textContent();
    expect(secondText).not.toBe(firstText);

    // Press k to go back to first block
    await page.keyboard.press('k');
    const backFocused = page.locator('.kb-nav.focused');
    await expect(backFocused).toHaveCount(1);
    const backText = await backFocused.textContent();
    expect(backText).toBe(firstText);
  });

  test('multiple j presses move forward sequentially through blocks', async ({ page }) => {
    const allNav = page.locator('.kb-nav');
    const totalCount = await allNav.count();
    expect(totalCount).toBeGreaterThan(4);

    // Press j four times
    await page.keyboard.press('j');
    await page.keyboard.press('j');
    await page.keyboard.press('j');
    await page.keyboard.press('j');

    // The fourth element (index 3) should be focused
    const fourthEl = allNav.nth(3);
    await expect(fourthEl).toHaveClass(/focused/);
    await expect(page.locator('.kb-nav.focused')).toHaveCount(1);
  });

  test('k at first element stays at first element', async ({ page }) => {
    await page.keyboard.press('j');
    const allNav = page.locator('.kb-nav');
    await expect(allNav.first()).toHaveClass(/focused/);

    await page.keyboard.press('k');
    await expect(allNav.first()).toHaveClass(/focused/);
  });
});

// ============================================================
// Comment Shortcuts (c, e, d)
// ============================================================
test.describe('Keyboard Comment Shortcuts — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await clearFocus(page);
  });

  test('c opens comment form on focused markdown block', async ({ page }) => {
    // Navigate to a line-block
    await page.keyboard.press('j');
    const focused = page.locator('.line-block.kb-nav.focused');
    await expect(focused).toHaveCount(1);

    await page.keyboard.press('c');

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();
    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();
  });

  test('e edits comment on focused block', async ({ page, request }) => {
    // Create a comment on line 1 of plan.md
    await request.post(`/api/file/comments?path=plan.md`, {
      data: { start_line: 1, end_line: 1, body: 'Filemode edit test' },
    });

    await loadPage(page);
    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await clearFocus(page);

    // Verify comment exists
    await expect(section.locator('.comment-card')).toBeVisible();

    // Navigate to the block covering line 1
    await page.keyboard.press('j');
    const focused = page.locator('.line-block.kb-nav.focused');
    await expect(focused).toHaveCount(1);

    // Check this block covers line 1
    const startLine = await focused.getAttribute('data-start-line');
    expect(parseInt(startLine!)).toBeLessThanOrEqual(1);

    await page.keyboard.press('e');

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('Filemode edit test');
  });

  test('d deletes comment on focused block', async ({ page, request }) => {
    // Create a comment on line 1 of plan.md
    await request.post(`/api/file/comments?path=plan.md`, {
      data: { start_line: 1, end_line: 1, body: 'Filemode delete test' },
    });

    await loadPage(page);
    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await clearFocus(page);

    // Verify comment exists
    await expect(section.locator('.comment-card')).toBeVisible();

    // Navigate to the first block (should cover line 1)
    await page.keyboard.press('j');
    await expect(page.locator('.line-block.kb-nav.focused')).toHaveCount(1);

    await page.keyboard.press('d');

    await expect(section.locator('.comment-card')).toHaveCount(0);
  });
});

// ============================================================
// UI Toggles — File Mode
// ============================================================
test.describe('Keyboard UI Toggles — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await clearFocus(page);
  });

  test('? toggles shortcuts overlay', async ({ page }) => {
    const overlay = page.locator('#shortcutsOverlay');
    await expect(overlay).not.toHaveClass(/active/);

    await page.keyboard.press('?');
    await expect(overlay).toHaveClass(/active/);

    await page.keyboard.press('?');
    await expect(overlay).not.toHaveClass(/active/);
  });

  test('t toggles table of contents', async ({ page }) => {
    const toc = page.locator('#toc');

    // In file mode, the toc toggle is visible and toc starts hidden
    await expect(toc).toHaveClass(/toc-hidden/);

    await page.keyboard.press('t');
    await expect(toc).not.toHaveClass(/toc-hidden/);

    await page.keyboard.press('t');
    await expect(toc).toHaveClass(/toc-hidden/);
  });

  test('Escape closes shortcuts overlay', async ({ page }) => {
    const overlay = page.locator('#shortcutsOverlay');

    await page.keyboard.press('?');
    await expect(overlay).toHaveClass(/active/);

    await page.keyboard.press('Escape');
    await expect(overlay).not.toHaveClass(/active/);
  });
});

// ============================================================
// Escape Behavior — File Mode
// ============================================================
test.describe('Keyboard Escape — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await clearFocus(page);
  });

  test('Escape closes open comment form', async ({ page }) => {
    // Open a comment form via keyboard
    await page.keyboard.press('j');
    await page.keyboard.press('c');

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    // Press Escape from textarea
    await page.locator('.comment-form textarea').press('Escape');
    await expect(form).toHaveCount(0);
  });

  test('Escape clears focus when no form is open', async ({ page }) => {
    await page.keyboard.press('j');
    await expect(page.locator('.kb-nav.focused')).toHaveCount(1);

    await page.keyboard.press('Escape');
    await expect(page.locator('.kb-nav.focused')).toHaveCount(0);
  });
});

// ============================================================
// Shortcuts Disabled When Typing — File Mode
// ============================================================
test.describe('Shortcuts Disabled When Typing — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await clearFocus(page);
  });

  test('j types into textarea instead of navigating', async ({ page }) => {
    // Open comment form
    await page.keyboard.press('j');
    await page.keyboard.press('c');

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();

    await textarea.type('jjj');
    await expect(textarea).toHaveValue('jjj');

    // Shortcuts overlay should not have opened
    await expect(page.locator('#shortcutsOverlay')).not.toHaveClass(/active/);
  });
});

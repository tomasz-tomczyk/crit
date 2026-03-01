import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

// Helper: clean all comments via API so each test starts fresh.
async function clearAllComments(request: APIRequestContext) {
  const sessionRes = await request.get('/api/session');
  const session = await sessionRes.json();
  const files = session.files || [];

  for (const f of files) {
    const commentsRes = await request.get(`/api/file/comments?path=${encodeURIComponent(f.path)}`);
    const comments = await commentsRes.json();
    if (Array.isArray(comments)) {
      for (const c of comments) {
        await request.delete(`/api/comment/${c.id}?path=${encodeURIComponent(f.path)}`);
      }
    }
  }
}

async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

function serverSection(page: Page) {
  return page.locator('#file-section-server\\.go');
}

// Expand the first spacer in server.go and return the section locator
async function expandFirstSpacer(page: Page) {
  const section = serverSection(page);
  await expect(section).toBeVisible();
  const spacer = section.locator('.diff-spacer').first();
  await expect(spacer).toBeVisible();
  await spacer.click();
  // Wait for expanded rows to appear
  await expect(section.locator('.diff-split-row').first()).toBeVisible();
  return section;
}

// Find a context line on the right (new) side — a row where right side
// is NOT addition and NOT deletion (i.e., a context line with a line number).
async function findContextRightSide(section: import('@playwright/test').Locator) {
  const rows = section.locator('.diff-split-row');
  const count = await rows.count();
  for (let i = 0; i < count; i++) {
    const row = rows.nth(i);
    const right = row.locator('.diff-split-side.right');
    const hasAddition = await right.evaluate(el => el.classList.contains('addition'));
    const hasDeletion = await right.evaluate(el => el.classList.contains('deletion'));
    const hasEmpty = await right.evaluate(el => el.classList.contains('empty'));
    if (!hasAddition && !hasDeletion && !hasEmpty) {
      // Verify it has a line number (not empty)
      const numText = await right.locator('.diff-gutter-num').textContent();
      if (numText && numText.trim()) {
        return right;
      }
    }
  }
  return null;
}

// Find a context line on the left (old) side
async function findContextLeftSide(section: import('@playwright/test').Locator) {
  const rows = section.locator('.diff-split-row');
  const count = await rows.count();
  for (let i = 0; i < count; i++) {
    const row = rows.nth(i);
    const left = row.locator('.diff-split-side.left');
    const hasDeletion = await left.evaluate(el => el.classList.contains('deletion'));
    const hasEmpty = await left.evaluate(el => el.classList.contains('empty'));
    if (!hasDeletion && !hasEmpty) {
      const numText = await left.locator('.diff-gutter-num').textContent();
      if (numText && numText.trim()) {
        return left;
      }
    }
  }
  return null;
}

// ============================================================
// Expanded Context Line Comments — Split Mode (New/Right Side)
// ============================================================
test.describe('Expanded Context Comments — Split Mode (New Side)', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('submit comment on expanded context line (new side)', async ({ page }) => {
    const section = await expandFirstSpacer(page);

    const rightSide = await findContextRightSide(section);
    expect(rightSide).not.toBeNull();
    await rightSide!.hover();

    const commentBtn = rightSide!.locator('.diff-comment-btn');
    await expect(commentBtn).toBeVisible();
    await commentBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await textarea.fill('Comment on expanded context line (new side)');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Comment on expanded context line (new side)');
  });

  test('edit comment on expanded context line (new side)', async ({ page }) => {
    const section = await expandFirstSpacer(page);

    // Create a comment first
    const rightSide = await findContextRightSide(section);
    expect(rightSide).not.toBeNull();
    await rightSide!.hover();
    await rightSide!.locator('.diff-comment-btn').click();
    await page.locator('.comment-form textarea').fill('Original expanded comment');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    // Click Edit
    const editBtn = section.locator('.comment-actions button').filter({ hasText: 'Edit' });
    await editBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('Original expanded comment');

    await textarea.clear();
    await textarea.fill('Edited expanded comment');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Edited expanded comment');
  });

  test('delete comment on expanded context line (new side)', async ({ page }) => {
    const section = await expandFirstSpacer(page);

    // Create a comment first
    const rightSide = await findContextRightSide(section);
    expect(rightSide).not.toBeNull();
    await rightSide!.hover();
    await rightSide!.locator('.diff-comment-btn').click();
    await page.locator('.comment-form textarea').fill('Delete me expanded');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    // Delete it
    const deleteBtn = section.locator('.comment-actions .delete-btn');
    await deleteBtn.click();

    await expect(section.locator('.comment-card')).toHaveCount(0);
  });
});

// ============================================================
// Expanded Context Line Comments — Split Mode (Old/Left Side)
// ============================================================
test.describe('Expanded Context Comments — Split Mode (Old Side)', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('submit comment on expanded context line (old side)', async ({ page }) => {
    const section = await expandFirstSpacer(page);

    const leftSide = await findContextLeftSide(section);
    expect(leftSide).not.toBeNull();
    await leftSide!.hover();

    const commentBtn = leftSide!.locator('.diff-comment-btn');
    await expect(commentBtn).toBeVisible();
    await commentBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await textarea.fill('Comment on old side context line');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Comment on old side context line');
  });

  test('edit comment on expanded context line (old side)', async ({ page }) => {
    const section = await expandFirstSpacer(page);

    const leftSide = await findContextLeftSide(section);
    expect(leftSide).not.toBeNull();
    await leftSide!.hover();
    await leftSide!.locator('.diff-comment-btn').click();
    await page.locator('.comment-form textarea').fill('Original old side comment');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    const editBtn = section.locator('.comment-actions button').filter({ hasText: 'Edit' });
    await editBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toHaveValue('Original old side comment');
    await textarea.clear();
    await textarea.fill('Edited old side comment');
    await page.locator('.comment-form .btn-primary').click();

    await expect(section.locator('.comment-card .comment-body')).toContainText('Edited old side comment');
  });

  test('delete comment on expanded context line (old side)', async ({ page }) => {
    const section = await expandFirstSpacer(page);

    const leftSide = await findContextLeftSide(section);
    expect(leftSide).not.toBeNull();
    await leftSide!.hover();
    await leftSide!.locator('.diff-comment-btn').click();
    await page.locator('.comment-form textarea').fill('Delete old side');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    const deleteBtn = section.locator('.comment-actions .delete-btn');
    await deleteBtn.click();

    await expect(section.locator('.comment-card')).toHaveCount(0);
  });
});

// ============================================================
// Expanded Context Line Comments — Unified Mode
// ============================================================
test.describe('Expanded Context Comments — Unified Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    // Switch to unified mode
    const unifiedBtn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
    await unifiedBtn.click();
    await expect(page.locator('.diff-container.unified').first()).toBeVisible();
  });

  test('submit comment on expanded context line in unified mode', async ({ page }) => {
    const section = serverSection(page);
    await expect(section).toBeVisible();

    // Expand spacer in unified mode
    const spacer = section.locator('.diff-spacer').first();
    await expect(spacer).toBeVisible();
    await spacer.click();

    // Find a context line (not addition, not deletion) in unified mode
    const contextLine = section.locator('.diff-container.unified .diff-line:not(.addition):not(.deletion)').first();
    await expect(contextLine).toBeVisible();
    await contextLine.hover();

    const commentBtn = contextLine.locator('.diff-comment-btn');
    await expect(commentBtn).toBeVisible();
    await commentBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await textarea.fill('Unified expanded context comment');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Unified expanded context comment');
  });

  test('edit comment on expanded context line in unified mode', async ({ page }) => {
    const section = serverSection(page);

    const spacer = section.locator('.diff-spacer').first();
    await expect(spacer).toBeVisible();
    await spacer.click();

    const contextLine = section.locator('.diff-container.unified .diff-line:not(.addition):not(.deletion)').first();
    await contextLine.hover();
    await contextLine.locator('.diff-comment-btn').click();
    await page.locator('.comment-form textarea').fill('Original unified context');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    const editBtn = section.locator('.comment-actions button').filter({ hasText: 'Edit' });
    await editBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toHaveValue('Original unified context');
    await textarea.clear();
    await textarea.fill('Edited unified context');
    await page.locator('.comment-form .btn-primary').click();

    await expect(section.locator('.comment-card .comment-body')).toContainText('Edited unified context');
  });

  test('delete comment on expanded context line in unified mode', async ({ page }) => {
    const section = serverSection(page);

    const spacer = section.locator('.diff-spacer').first();
    await expect(spacer).toBeVisible();
    await spacer.click();

    const contextLine = section.locator('.diff-container.unified .diff-line:not(.addition):not(.deletion)').first();
    await contextLine.hover();
    await contextLine.locator('.diff-comment-btn').click();
    await page.locator('.comment-form textarea').fill('Delete unified context');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    const deleteBtn = section.locator('.comment-actions .delete-btn');
    await deleteBtn.click();

    await expect(section.locator('.comment-card')).toHaveCount(0);
  });
});

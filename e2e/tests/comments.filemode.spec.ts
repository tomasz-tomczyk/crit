import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

// Helper: clean all comments via API so each test starts fresh.
// Uses Playwright's request context which resolves relative URLs against baseURL.
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

// Helper: navigate and wait for page load
async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

// Helper: scope selectors to plan.md file section
function mdSection(page: Page) {
  return page.locator('.file-section').filter({ hasText: 'plan.md' });
}

// ============================================================
// File Mode Comments — plan.md (document view by default)
// ============================================================
test.describe('Comments — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    // In file mode, plan.md is already in document view — no toggle needed
    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
  });

  test('clicking + gutter button opens comment form', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();

    const gutterBtn = section.locator('.line-comment-gutter').first();
    await expect(gutterBtn).toBeVisible();
    await gutterBtn.click();

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();
  });

  test('submitting comment displays it below the block', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('File mode comment');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('File mode comment');
  });

  test('Ctrl+Enter submits comment', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Submitted via Ctrl+Enter');
    await textarea.press('Control+Enter');

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Submitted via Ctrl+Enter');
  });

  test('editing a comment works', async ({ page }) => {
    const section = mdSection(page);

    // Create a comment
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('Before edit');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    // Edit
    const editBtn = section.locator('.comment-actions button[title="Edit"]');
    await editBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('Before edit');

    await textarea.clear();
    await textarea.fill('After edit');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('After edit');
  });

  test('deleting a comment removes it', async ({ page }) => {
    const section = mdSection(page);

    // Create a comment
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('To be deleted');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    // Delete
    const deleteBtn = section.locator('.comment-actions .delete-btn');
    await deleteBtn.click();

    await expect(section.locator('.comment-card')).toHaveCount(0);
  });

  test('comment count updates after add and delete', async ({ page }) => {
    const section = mdSection(page);
    const countEl = page.locator('#commentCount');

    // Initially no comments
    await expect(countEl).toHaveText('');

    // Add comment
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('Count check');
    await page.locator('.comment-form .btn-primary').click();
    await expect(countEl).toContainText('1');

    // Delete comment
    await section.locator('.comment-actions .delete-btn').click();
    await expect(countEl).toHaveText('');
  });
});

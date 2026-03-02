import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

async function clearAllComments(request: APIRequestContext) {
  const sessionRes = await request.get('/api/session');
  const session = await sessionRes.json();
  for (const f of session.files || []) {
    const commentsRes = await request.get(`/api/file/comments?path=${encodeURIComponent(f.path)}`);
    const comments = await commentsRes.json();
    if (Array.isArray(comments)) {
      for (const c of comments) {
        await request.delete(`/api/comment/${c.id}?path=${encodeURIComponent(f.path)}`);
      }
    }
  }
}

// ============================================================
// File Tree Panel — File Mode
// ============================================================
test.describe('File Tree — File Mode', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
  });

  test('file tree panel is visible with multiple files', async ({ page }) => {
    const panel = page.locator('#fileTreePanel');
    await expect(panel).toBeVisible();
  });

  test('file tree lists all files', async ({ page }) => {
    // File mode fixture has: plan.md, server.go, handler.js
    const treeFiles = page.locator('.tree-file');
    await expect(treeFiles).toHaveCount(3);
  });

  test('file tree shows correct file names', async ({ page }) => {
    const fileNames = page.locator('.tree-file-name');
    const names: string[] = [];
    const count = await fileNames.count();
    for (let i = 0; i < count; i++) {
      names.push(await fileNames.nth(i).textContent() || '');
    }
    expect(names).toContain('plan.md');
    expect(names).toContain('server.go');
    expect(names).toContain('handler.js');
  });

  test('file tree header shows file count', async ({ page }) => {
    const stats = page.locator('#fileTreeStats');
    await expect(stats).toBeVisible();
    await expect(stats).toContainText('3');
  });

  test('clicking a file in tree scrolls to its section', async ({ page }) => {
    const treeFile = page.locator('.tree-file', {
      has: page.locator('.tree-file-name', { hasText: 'handler.js' }),
    });
    await treeFile.click();

    const section = page.locator('.file-section').filter({ hasText: 'handler.js' });
    await expect(section).toBeInViewport();
  });

  test('clicking a file marks it as active', async ({ page }) => {
    const treeFile = page.locator('.tree-file', {
      has: page.locator('.tree-file-name', { hasText: 'plan.md' }),
    });
    await treeFile.click();
    await expect(treeFile).toHaveClass(/active/);
  });
});

// ============================================================
// File Tree Comment Badges — File Mode
// ============================================================
test.describe('File Tree Comment Badges — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('no comment badges shown initially', async ({ page }) => {
    const badges = page.locator('.tree-comment-badge');
    await expect(badges).toHaveCount(0);
  });

  test('comment badge appears after adding a comment on markdown', async ({ page }) => {
    // plan.md is in document view by default in file mode
    const section = page.locator('.file-section').filter({ hasText: 'plan.md' });
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('File mode badge test');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    // Tree should now show a badge
    const treeFile = page.locator('.tree-file', {
      has: page.locator('.tree-file-name', { hasText: 'plan.md' }),
    });
    const badge = treeFile.locator('.tree-comment-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText('1');
  });
});

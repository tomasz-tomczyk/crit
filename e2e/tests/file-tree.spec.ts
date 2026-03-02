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
// File Tree Panel — Git Mode
// ============================================================
test.describe('File Tree — Git Mode', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
  });

  test('file tree panel is visible', async ({ page }) => {
    const panel = page.locator('#fileTreePanel');
    await expect(panel).toBeVisible();
  });

  test('file tree lists all files from the session', async ({ page }) => {
    // Git fixture has: plan.md, server.go, handler.js, deleted.txt
    const treeFiles = page.locator('.tree-file');
    await expect(treeFiles).toHaveCount(4);
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
    expect(names).toContain('deleted.txt');
  });

  test('file tree header shows file count', async ({ page }) => {
    const stats = page.locator('#fileTreeStats');
    await expect(stats).toBeVisible();
    await expect(stats).toContainText('4');
  });

  test('file tree header shows addition stats', async ({ page }) => {
    const addStat = page.locator('#fileTreeStats .tree-stat-add');
    await expect(addStat).toBeVisible();
    const text = await addStat.textContent();
    expect(text).toMatch(/\+\d+/);
  });

  test('file tree header shows deletion stats', async ({ page }) => {
    const delStat = page.locator('#fileTreeStats .tree-stat-del');
    await expect(delStat).toBeVisible();
    const text = await delStat.textContent();
    expect(text).toMatch(/-\d+/);
  });

  test('clicking a file in tree scrolls to its section', async ({ page }) => {
    const treeFile = page.locator('.tree-file', {
      has: page.locator('.tree-file-name', { hasText: 'handler.js' }),
    });
    await treeFile.click();

    const section = page.locator('#file-section-handler\\.js');
    await expect(section).toBeInViewport();
  });

  test('clicking a file in tree marks it as active', async ({ page }) => {
    const treeFile = page.locator('.tree-file', {
      has: page.locator('.tree-file-name', { hasText: 'server.go' }),
    });
    await treeFile.click();
    await expect(treeFile).toHaveClass(/active/);
  });

  test('only one file is active at a time', async ({ page }) => {
    // Click server.go
    const serverFile = page.locator('.tree-file', {
      has: page.locator('.tree-file-name', { hasText: 'server.go' }),
    });
    await serverFile.click();
    await expect(serverFile).toHaveClass(/active/);

    // Click handler.js
    const handlerFile = page.locator('.tree-file', {
      has: page.locator('.tree-file-name', { hasText: 'handler.js' }),
    });
    await handlerFile.click();
    await expect(handlerFile).toHaveClass(/active/);
    await expect(serverFile).not.toHaveClass(/active/);
  });

  test('file status icons have correct classes', async ({ page }) => {
    // plan.md and handler.js are added
    const addedIcons = page.locator('.tree-file-status-icon.added');
    await expect(addedIcons).toHaveCount(2);

    // server.go is modified
    const modifiedIcons = page.locator('.tree-file-status-icon.modified');
    await expect(modifiedIcons).toHaveCount(1);

    // deleted.txt is deleted
    const deletedIcons = page.locator('.tree-file-status-icon.deleted');
    await expect(deletedIcons).toHaveCount(1);
  });
});

// ============================================================
// File Tree Comment Badges — Git Mode
// ============================================================
test.describe('File Tree Comment Badges — Git Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('no comment badges shown when there are no comments', async ({ page }) => {
    const badges = page.locator('.tree-comment-badge');
    await expect(badges).toHaveCount(0);
  });

  test('comment badge appears after adding a comment', async ({ page }) => {
    // Add a comment on server.go (diff file)
    const section = page.locator('#file-section-server\\.go');
    const additionSide = section.locator('.diff-split-side.addition').first();
    await additionSide.hover();
    await additionSide.locator('.diff-comment-btn').click();
    await page.locator('.comment-form textarea').fill('Badge test');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    // Tree should now show a badge on server.go
    const serverTreeFile = page.locator('.tree-file', {
      has: page.locator('.tree-file-name', { hasText: 'server.go' }),
    });
    const badge = serverTreeFile.locator('.tree-comment-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText('1');
  });

  test('comment badge updates when comment is deleted', async ({ page }) => {
    // Add a comment on server.go
    const section = page.locator('#file-section-server\\.go');
    const additionSide = section.locator('.diff-split-side.addition').first();
    await additionSide.hover();
    await additionSide.locator('.diff-comment-btn').click();
    await page.locator('.comment-form textarea').fill('Badge delete test');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    // Verify badge exists
    const serverTreeFile = page.locator('.tree-file', {
      has: page.locator('.tree-file-name', { hasText: 'server.go' }),
    });
    await expect(serverTreeFile.locator('.tree-comment-badge')).toBeVisible();

    // Delete the comment
    await section.locator('.comment-actions .delete-btn').click();
    await expect(section.locator('.comment-card')).toHaveCount(0);

    // Badge should be gone
    await expect(serverTreeFile.locator('.tree-comment-badge')).toHaveCount(0);
  });
});

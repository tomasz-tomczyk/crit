import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

test.describe('File Tree — File Mode', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
  });

  test('file tree lists only the file-mode files', async ({ page }) => {
    // File mode fixture has: plan.md, server.go, handler.js
    await expect(page.locator('.tree-file')).toHaveCount(3);
  });

  test('file tree shows correct file names', async ({ page }) => {
    const tree = page.locator('#fileTreePanel');
    await expect(tree.locator('.tree-file-name', { hasText: 'plan.md' })).toBeVisible();
    await expect(tree.locator('.tree-file-name', { hasText: 'server.go' })).toBeVisible();
    await expect(tree.locator('.tree-file-name', { hasText: 'handler.js' })).toBeVisible();
  });

  test('file tree header shows file count', async ({ page }) => {
    await expect(page.locator('#fileTreeStats')).toContainText('3');
  });
});

test.describe('File Tree Comment Badges — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('comment badge appears after adding a comment on markdown', async ({ page }) => {
    const section = page.locator('.file-section').filter({ hasText: 'plan.md' });
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await textarea.fill('File mode badge test');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    const treeFile = page.locator('.tree-file', {
      has: page.locator('.tree-file-name', { hasText: 'plan.md' }),
    });
    const badge = treeFile.locator('.tree-comment-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText('1');
  });
});

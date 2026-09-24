import { test, expect, type Page } from '@playwright/test';
import { clearAllComments, loadPage, fileSectionByName, reviewFileOrder, treeFiles } from './helpers';

// Helpers scoped to this fixture's files (mount via tree — file-list virt)
async function planSection(page: Page) {
  return fileSectionByName(page, 'plan.md');
}

async function goSection(page: Page) {
  return fileSectionByName(page, 'main.go');
}

async function exSection(page: Page) {
  return fileSectionByName(page, 'handler.ex');
}

test.describe('Multi-File Mode — Loading', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('shows all files including directory contents in file tree', async ({ page }) => {
    // Should have 5 files: plan.md, main.go, handler.ex, lib/utils.ex, lib/config.ex
    const treeFiles = page.locator('.tree-file');
    await expect(treeFiles).toHaveCount(5);
  });

  test('file tree shows directory path for nested files', async ({ page }) => {
    // lib/utils.ex and lib/config.ex should appear with their paths
    await expect(page.locator('.tree-file-name', { hasText: 'utils.ex' })).toBeVisible();
    await expect(page.locator('.tree-file-name', { hasText: 'config.ex' })).toBeVisible();
  });

  test('displays all file sections', async ({ page }) => {
    await expect(await planSection(page)).toBeVisible();
    await expect(await goSection(page)).toBeVisible();
    await expect(await exSection(page)).toBeVisible();
    await expect(await fileSectionByName(page, 'utils.ex')).toBeVisible();
    await expect(await fileSectionByName(page, 'config.ex')).toBeVisible();
  });

  test('session mode is "files"', async ({ request }) => {
    const res = await request.get('/api/session');
    const session = await res.json();
    expect(session.mode).toBe('files');
  });

  test('stats show correct file count', async ({ page }) => {
    const stats = page.locator('#fileTreeStats');
    await expect(stats).toContainText('5');
  });

  test('preserves CLI argument order (does not sort alphabetically)', async ({ page }) => {
    // Fixture passes: plan.md main.go handler.ex lib/
    // Expected order: CLI args in given order, then directory contents (walked alphabetically)
    await expect(treeFiles(page)).toHaveCount(5);
    expect(await reviewFileOrder(page)).toEqual([
      'plan.md',
      'main.go',
      'handler.ex',
      'lib/config.ex',
      'lib/utils.ex',
    ]);
  });
});

test.describe('Multi-File Mode — Code File Rendering', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('Go file renders with syntax-highlighted code', async ({ page }) => {
    const section = await goSection(page);
    await expect(section).toBeVisible();
    // Code files in file mode default to document view
    const codeBlock = section.locator('code');
    await expect(codeBlock.first()).toBeVisible();
  });

  test('Elixir file renders with code content', async ({ page }) => {
    const section = await exSection(page);
    await expect(section).toBeVisible();
    // Should contain Elixir keywords
    await expect(section).toContainText('defmodule');
    await expect(section).toContainText('def handle_request');
  });

  test('markdown file renders in document view by default', async ({ page }) => {
    const section = await planSection(page);
    await expect(section).toBeVisible();
    const docWrapper = section.locator('.document-wrapper');
    await expect(docWrapper).toBeVisible();
    await expect(section).toContainText('Migration Plan');
  });
});

test.describe('Multi-File Mode — Comments on Code Files', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('can add a comment on a Go file line', async ({ page }) => {
    const section = await goSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();

    const gutterBtn = section.locator('.line-comment-gutter').first();
    await expect(gutterBtn).toBeVisible();
    await gutterBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Comment on Go code');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Comment on Go code');
  });

  test('can add a comment on an Elixir file line', async ({ page }) => {
    const section = await exSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();

    const gutterBtn = section.locator('.line-comment-gutter').first();
    await expect(gutterBtn).toBeVisible();
    await gutterBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Comment on Elixir code');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Comment on Elixir code');
  });

  test('can add a comment on a nested directory file', async ({ page }) => {
    const section = await fileSectionByName(page, 'utils.ex');
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();

    const gutterBtn = section.locator('.line-comment-gutter').first();
    await expect(gutterBtn).toBeVisible();
    await gutterBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Comment on nested file');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Comment on nested file');
  });

  test('comment count reflects comments across all files', async ({ page, request }) => {
    // Add comments via API on different files
    await request.post('/api/file/comments?path=main.go', {
      data: { start_line: 1, end_line: 1, body: 'Go comment' },
    });
    await request.post('/api/file/comments?path=handler.ex', {
      data: { start_line: 1, end_line: 1, body: 'Elixir comment' },
    });
    await request.post('/api/file/comments?path=lib/utils.ex', {
      data: { start_line: 1, end_line: 1, body: 'Nested comment' },
    });

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    const countEl = page.locator('#commentCount');
    await expect(countEl).toBeVisible();
    await expect(countEl).toHaveAttribute('title', /3 unresolved/);
  });
});

test.describe('Multi-File Mode — File Tree Interaction', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('clicking a file in the tree scrolls to that section', async ({ page }) => {
    const treeFile = page.locator('.tree-file-name', { hasText: 'config.ex' });
    await treeFile.click();

    const section = await fileSectionByName(page, 'config.ex');
    await expect(section).toBeInViewport();
  });

  test('file tree shows comment badges for files with comments', async ({ page, request }) => {
    await request.post('/api/file/comments?path=main.go', {
      data: { start_line: 1, end_line: 1, body: 'Badge test' },
    });

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // The tree file for main.go should have a comment badge
    const goTreeFile = page.locator('.tree-file').filter({ hasText: 'main.go' });
    const badge = goTreeFile.locator('.tree-comment-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toContainText('1');
  });
});

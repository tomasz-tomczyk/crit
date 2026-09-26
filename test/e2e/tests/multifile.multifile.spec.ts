import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import {
  clearAllComments, loadPage, revealFile, fileItem, diffLine, openLineComment,
  reviewScroller, nextFrames,
} from './helpers';

// Helpers scoped to this fixture's files. The file list is virtualized, so
// each brings its file into view and returns the file's Pierre item.
function planSection(page: Page) {
  return revealFile(page, 'plan.md');
}

function goSection(page: Page) {
  return revealFile(page, 'main.go');
}

function exSection(page: Page) {
  return revealFile(page, 'handler.ex');
}

// The comment is stored on the expected file and line.
async function expectCommentOn(request: APIRequestContext, filePath: string, line: number, body: string) {
  await expect.poll(async () => {
    const comments = await (await request.get(`/api/file/comments?path=${encodeURIComponent(filePath)}`)).json();
    return comments.map((c: { start_line: number; end_line: number; body: string }) => `${c.start_line}-${c.end_line}:${c.body}`);
  }).toEqual([`${line}-${line}:${body}`]);
}

// File paths in list order: walk the #filesContainer scroller top to bottom
// and record each file header the first time it is rendered.
async function listOrder(page: Page): Promise<string[]> {
  const scroller = reviewScroller(page);
  const seen: string[] = [];
  await scroller.evaluate(el => el.scrollTo(0, 0));
  for (let step = 0; step < 200; step++) {
    const paths = await page.locator('.pierre-file-header[data-file-path]').evaluateAll(els =>
      els
        .map(el => ({ p: el.getAttribute('data-file-path') || '', top: el.getBoundingClientRect().top }))
        .sort((a, b) => a.top - b.top)
        .map(x => x.p));
    for (const p of paths) if (!seen.includes(p)) seen.push(p);
    const atEnd = await scroller.evaluate(el => {
      const before = el.scrollTop;
      el.scrollTop = before + el.clientHeight / 2;
      return el.scrollTop === before;
    });
    if (atEnd) break;
    // Let the virtualizer mount what scrolled into view.
    await nextFrames(page);
  }
  return seen;
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
    await expect((await planSection(page)).locator('.document-wrapper')).toBeVisible();
    await expect(diffLine(await goSection(page), 1)).toBeVisible();
    await expect(diffLine(await exSection(page), 1)).toBeVisible();
    // Nested files too
    await expect(diffLine(await revealFile(page, 'lib/utils.ex'), 1)).toBeVisible();
    await expect(diffLine(await revealFile(page, 'lib/config.ex'), 1)).toBeVisible();
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
    await expect(page.locator('.pierre-file-header').first()).toBeVisible();
    expect(await listOrder(page)).toEqual([
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
    // Code files in file mode render the whole file (no diff sides)
    await expect(diffLine(section, 1)).toContainText('package');
    await expect(section.locator('code[data-additions], code[data-deletions], code[data-unified]')).toHaveCount(0);
    // Shiki tokens carry per-theme colors as inline custom properties
    await expect(section.locator('code[data-code] [data-content] span[style*="--diffs-token"]').first()).toBeVisible();
  });

  test('Elixir file renders with code content', async ({ page }) => {
    const section = await exSection(page);
    // Should contain Elixir keywords
    await expect(section.locator('code[data-code]')).toContainText('defmodule');
    await expect(section.locator('code[data-code]')).toContainText('def handle_request');
  });

  test('markdown file renders in document view by default', async ({ page }) => {
    const section = await planSection(page);
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

  test('can add a comment on a Go file line', async ({ page, request }) => {
    const section = await goSection(page);
    const form = await openLineComment(page, section, 1);

    const textarea = form.locator('textarea');
    await textarea.fill('Comment on Go code');
    await form.locator('.btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Comment on Go code');
    await expectCommentOn(request, 'main.go', 1, 'Comment on Go code');
  });

  test('can add a comment on an Elixir file line', async ({ page, request }) => {
    const section = await exSection(page);
    const form = await openLineComment(page, section, 1);

    const textarea = form.locator('textarea');
    await textarea.fill('Comment on Elixir code');
    await form.locator('.btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Comment on Elixir code');
    await expectCommentOn(request, 'handler.ex', 1, 'Comment on Elixir code');
  });

  test('can add a comment on a nested directory file', async ({ page, request }) => {
    const section = await revealFile(page, 'lib/utils.ex');
    const form = await openLineComment(page, section, 1);

    const textarea = form.locator('textarea');
    await textarea.fill('Comment on nested file');
    await form.locator('.btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Comment on nested file');
    await expectCommentOn(request, 'lib/utils.ex', 1, 'Comment on nested file');
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

    const header = page.locator('.pierre-file-header[data-file-path="lib/config.ex"]');
    await expect(header).toBeInViewport();
    await expect(diffLine(fileItem(page, 'lib/config.ex'), 1)).toBeInViewport();
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

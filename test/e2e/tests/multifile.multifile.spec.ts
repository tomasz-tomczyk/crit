import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import {
  addComment, clearAllComments, loadPage, revealFile, fileItem, diffLine, openLineComment,
  reviewScroller, nextFrames,
} from './helpers';

// Helpers scoped to this fixture's files. The file list is virtualized, so
// each brings its file into view and returns the file's Pierre item.
function planSection(page: Page) {
  return revealFile(page, 'plan.md');
}

function mainGoSection(page: Page) {
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

  test('file tree lists all files including directory contents, with nested names and count', async ({ page, request }) => {
    // 11 files: plan.md, main.go, handler.ex, flow.mmd, seq.mmd, main.py.tmpl, greeting.gts, report.qmd, app.conf, lib/utils.ex, lib/config.ex
    await expect(page.locator('.tree-file')).toHaveCount(11);
    // lib/utils.ex and lib/config.ex appear by name
    await expect(page.locator('.tree-file-name', { hasText: 'utils.ex' })).toBeVisible();
    await expect(page.locator('.tree-file-name', { hasText: 'config.ex' })).toBeVisible();
    await expect(page.locator('#fileTreeStats')).toContainText('11');
    // The session is a files-mode session.
    const session = await (await request.get('/api/session')).json();
    expect(session.mode).toBe('files');
  });

  test('displays all file sections', async ({ page }) => {
    await expect((await planSection(page)).locator('.document-wrapper')).toBeVisible();
    await expect(diffLine(await mainGoSection(page), 1)).toBeVisible();
    await expect(diffLine(await exSection(page), 1)).toBeVisible();
    // Nested files too
    await expect(diffLine(await revealFile(page, 'lib/utils.ex'), 1)).toBeVisible();
    await expect(diffLine(await revealFile(page, 'lib/config.ex'), 1)).toBeVisible();
  });

  test('preserves CLI argument order (does not sort alphabetically)', async ({ page }) => {
    // Fixture passes: plan.md main.go handler.ex flow.mmd seq.mmd main.py.tmpl greeting.gts report.qmd app.conf lib/
    // Expected order: CLI args in given order, then directory contents (walked alphabetically)
    await expect(page.locator('.pierre-file-header').first()).toBeVisible();
    expect(await listOrder(page)).toEqual([
      'plan.md',
      'main.go',
      'handler.ex',
      'flow.mmd',
      'seq.mmd',
      'main.py.tmpl',
      'greeting.gts',
      'report.qmd',
      'app.conf',
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
    const section = await mainGoSection(page);
    // Code files in file mode render the whole file (no diff sides)
    await expect(diffLine(section, 1)).toContainText('package');
    await expect(section.locator('code[data-additions], code[data-deletions], code[data-unified]')).toHaveCount(0);
    // Shiki tokens carry per-theme colors as inline custom properties
    await expect(section.locator('code[data-code] [data-content] span[style*="--diffs-token"]').first()).toBeVisible();
  });

  test('template file is highlighted as the language under its suffix', async ({ page }) => {
    const section = await revealFile(page, 'main.py.tmpl');
    await expect(diffLine(section, 1)).toContainText('from app import agents');
    // `.tmpl` alone is plain text; the `.py` before it gives Python tokens
    await expect(section.locator('code[data-code] [data-content] span[style*="--diffs-token"]').first()).toBeVisible();
  });

  test('Ember .gts file highlights its TypeScript and its <template> block', async ({ page }) => {
    const section = await revealFile(page, 'greeting.gts');
    await expect(diffLine(section, 1)).toContainText('import Component');
    // Distinct token colours in a line; a grammar missing from the bundle
    // falls back to single-colour plain text.
    const colours = (line: number) => diffLine(section, line).evaluate(el => new Set(
      Array.from(el.querySelectorAll('span[style*="--diffs-token-light"]'))
        .map(s => (s as HTMLElement).style.getPropertyValue('--diffs-token-light').trim())).size);
    // export default class Greeting extends Component<Signature> {
    await expect.poll(() => colours(6)).toBeGreaterThanOrEqual(3);
    // <button type="button" {{on "click" this.greet}}>Hi {{@name}}</button>
    await expect.poll(() => colours(10)).toBeGreaterThanOrEqual(3);
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

test.describe('Multi-File Mode — languages config', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('the session reports the configured file type and language', async ({ request }) => {
    const session = await (await request.get('/api/session')).json();
    const byPath = Object.fromEntries(session.files.map((f: { path: string }) => [f.path, f]));
    expect(byPath['report.qmd']).toMatchObject({ file_type: 'markdown' });
    expect(byPath['report.qmd'].lang).toBeUndefined();
    expect(byPath['app.conf']).toMatchObject({ file_type: 'code', lang: 'yaml' });
    expect(byPath['main.go'].lang).toBeUndefined();
  });

  test('a .qmd file matched as markdown renders as a document', async ({ page }) => {
    const section = await revealFile(page, 'report.qmd');
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await expect(section.locator('.document-wrapper strong')).toHaveText('every');
  });

  test('a .conf file matched as yaml is tokenized as YAML, not nginx', async ({ page }) => {
    const section = await revealFile(page, 'app.conf');
    await expect(diffLine(section, 2)).toContainText('port: 8080');
    // YAML gives the key its own token ("  port", Pierre folds the indent in);
    // nginx (Pierre's detection for .conf) makes one "  port: " token.
    await expect(diffLine(section, 2).locator('span[style*="--diffs-token-light"]', { hasText: /^\s*port$/ })).toHaveCount(1);
  });
});

test.describe('Multi-File Mode — Comments on Code Files', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('can add a comment on a Go file line', async ({ page, request }) => {
    await loadPage(page);
    const section = await mainGoSection(page);
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
    await loadPage(page);
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
    await loadPage(page);
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
    await addComment(request, 'main.go', 1, 'Go comment');
    await addComment(request, 'handler.ex', 1, 'Elixir comment');
    await addComment(request, 'lib/utils.ex', 1, 'Nested comment');
    await loadPage(page);

    const countEl = page.locator('#commentCount');
    await expect(countEl).toBeVisible();
    await expect(countEl).toHaveAttribute('title', /3 unresolved/);
  });
});

test.describe('Multi-File Mode — File Tree Interaction', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('clicking a file in the tree scrolls to that section', async ({ page }) => {
    await loadPage(page);
    const treeFile = page.locator('.tree-file-name', { hasText: 'config.ex' });
    await treeFile.click();

    const header = page.locator('.pierre-file-header[data-file-path="lib/config.ex"]');
    await expect(header).toBeInViewport();
    await expect(diffLine(fileItem(page, 'lib/config.ex'), 1)).toBeInViewport();
  });

  test('file tree shows comment badges for files with comments', async ({ page, request }) => {
    await addComment(request, 'main.go', 1, 'Badge test');
    await loadPage(page);

    // The tree file for main.go should have a comment badge
    const goTreeFile = page.locator('.tree-file').filter({ hasText: 'main.go' });
    const badge = goTreeFile.locator('.tree-comment-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toContainText('1');
  });
});

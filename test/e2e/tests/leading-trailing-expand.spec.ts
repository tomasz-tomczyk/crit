import { test, expect, type Page, type Locator } from '@playwright/test';
import {
  clearAllComments, loadPage, revealFile, goSection, jsSection, diffLine,
  openLineComment, showLine, clickWhenHittable, scanDiffRows, lineRange, waitForScrollStable, waitHighlighted,
} from './helpers';

// utils.go appends a Reverse function at the end. Its only hunk starts at
// line 8, so lines 1..7 are collapsed behind a leading separator, and the
// hunk runs to EOF (line 19), so there is no trailing separator.
function utilsSection(page: Page) {
  return revealFile(page, 'utils.go');
}

// server.go's first hunk starts at line 2: the 1-line gap (package main) is
// too small to collapse. Its last hunk ends at EOF (line 71).
// handler.js is a new file — its hunk starts at line 1.

// Separator shown before the first hunk / after the last hunk.
function leadingSeparator(item: Locator): Locator {
  return item.locator('[data-separator][data-separator-first]').filter({ visible: true });
}
function trailingSeparator(item: Locator): Locator {
  return item.locator('[data-separator][data-separator-last]');
}
function leadingExpandButton(item: Locator): Locator {
  return item.locator('[data-separator][data-separator-first] [data-expand-button]').filter({ visible: true }).first();
}

async function expectLeadingSeparator(page: Page, item: Locator) {
  const sep = leadingSeparator(item).first();
  await expect(sep).toBeVisible();
  await expect(sep).toContainText('7 unmodified lines');
  await expect(leadingExpandButton(item)).toBeVisible();
  // The collapsed lines are not rendered; the hunk starts at line 8.
  expect((await scanDiffRows(page, item, 8)).slice(0, 2)).toEqual(['sep', '8']);
  await expect(diffLine(item, 1)).toHaveCount(0);
  await expect(diffLine(item, 7)).toHaveCount(0);
}

// One click reveals the whole gap (it is under 20 lines). The separator sits
// right under the sticky file header, so clickWhenHittable waits until the
// button holds still and takes hits before clicking once. A second click is
// never needed: if one lands and nothing expands, that is a bug.
async function expandLeading(page: Page, item: Locator) {
  await waitHighlighted(item, 9);
  await clickWhenHittable(page, leadingExpandButton(item));
  await expect(leadingSeparator(item)).toHaveCount(0);
  await waitForScrollStable(page);
  // Lines 1..7 are now rendered rows, in order, with no separator left.
  expect((await scanDiffRows(page, item, 19))).toEqual(lineRange(1, 19));
  await expect(item.locator('[data-separator]')).toHaveCount(0);
  await showLine(page, diffLine(item, 1));
  await expect(diffLine(item, 1)).toContainText('package main');
}

// ============================================================
// Leading separator (utils.go)
// ============================================================
test.describe('Leading Separator — Split Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('leading separator before a hunk that does not start at line 1 expands, and the lines are commentable', async ({ page, request }) => {
    await loadPage(page);
    const item = await utilsSection(page);
    await expectLeadingSeparator(page, item);

    // Clicking it reveals the context above the first hunk, on both sides,
    // and the separator disappears.
    await expandLeading(page, item);
    await expect(diffLine(item, 1)).toHaveAttribute('data-line-type', /^context/);
    await expect(diffLine(item, 1, 'old')).toContainText('package main');

    // Expanded leading context lines can be commented on.
    await showLine(page, diffLine(item, 3));
    const form = await openLineComment(page, item, 3);
    await form.locator('textarea').fill('Comment on expanded leading line');
    await form.locator('.btn-primary').click();
    await expect(item.locator('.comment-card .comment-body')).toContainText('Comment on expanded leading line');
    await expect.poll(async () => {
      const comments = await (await request.get('/api/file/comments?path=utils.go')).json();
      return comments.map((c: { start_line: number; end_line: number }) => `${c.start_line}-${c.end_line}`);
    }).toEqual(['3-3']);
  });

  test('no separator where a hunk starts at line 1 or ends at EOF', async ({ page }) => {
    await loadPage(page);

    // New file: its hunk starts at line 1, no separator at all.
    const handler = await jsSection(page);
    await expect(diffLine(handler, 1)).toBeVisible();
    await expect(handler.locator('[data-separator]')).toHaveCount(0);

    // server.go: the 1-line leading gap (package main) renders inline, and
    // the last hunk reaches EOF (line 71) so there is no trailing separator.
    const server = await goSection(page);
    await expect(diffLine(server, 1)).toBeVisible();
    await expect(diffLine(server, 1)).toContainText('package main');
    await expect(diffLine(server, 1, 'old')).toContainText('package main');
    await expect(server.locator('[data-separator][data-separator-first]')).toHaveCount(0);
    // Render the file's tail so a trailing separator would be in the DOM.
    await showLine(page, diffLine(server, 71));
    await expect(diffLine(server, 71)).toContainText('}');
    await expect(trailingSeparator(server)).toHaveCount(0);

    // utils.go: its only hunk runs to EOF (line 19).
    const utils = await utilsSection(page);
    await showLine(page, diffLine(utils, 19));
    await expect(trailingSeparator(utils)).toHaveCount(0);
  });
});

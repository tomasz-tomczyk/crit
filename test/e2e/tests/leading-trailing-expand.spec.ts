import { test, expect, type Page, type Locator } from '@playwright/test';
import { clearAllComments, loadPage, revealFile, goSection, jsSection, diffLine, openLineComment } from './helpers';

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

async function switchToUnified(page: Page) {
  const btn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
  await expect(btn).toBeVisible();
  await btn.click();
  const item = await goSection(page);
  await expect(item.locator('code[data-unified]')).toBeVisible();
}

// Pierre virtualizes lines inside long files: scroll the list down until
// the line is rendered, then bring it on screen.
async function showLine(page: Page, line: Locator) {
  await expect.poll(async () => {
    if (await line.count() > 0) return true;
    await page.locator('#filesContainer').evaluate(el => el.scrollBy(0, 200));
    return false;
  }, { timeout: 15_000 }).toBe(true);
  // The row can re-mount while the list settles; retry until it holds.
  await expect(async () => {
    await line.first().scrollIntoViewIfNeeded({ timeout: 1000 });
    await expect(line.first()).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10_000 });
}

// Pierre ignores pointer events briefly after any scroll. Click only once
// the element is the hit target at its centre, so the click lands once.
async function clickWhenHittable(page: Page, target: Locator) {
  await expect(async () => {
    await target.scrollIntoViewIfNeeded({ timeout: 1000 });
    expect(await target.evaluate(el => {
      const r = el.getBoundingClientRect();
      const root = el.getRootNode() as Document | ShadowRoot;
      const hit = root.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
      return !!hit && el.contains(hit);
    }, undefined, { timeout: 1000 })).toBe(true);
  }).toPass({ timeout: 10_000 });
  const box = await target.boundingBox();
  await page.mouse.click(box!.x + box!.width / 2, box!.y + box!.height / 2);
}

async function expectLeadingSeparator(item: Locator) {
  const sep = leadingSeparator(item).first();
  await expect(sep).toBeVisible();
  await expect(sep).toContainText('7 unmodified lines');
  await expect(leadingExpandButton(item)).toBeVisible();
  // The collapsed lines are not rendered; the hunk starts at line 8.
  await expect(diffLine(item, 8)).toBeVisible();
  await expect(diffLine(item, 1)).toHaveCount(0);
  await expect(diffLine(item, 7)).toHaveCount(0);
}

async function expandLeading(page: Page, item: Locator) {
  await clickWhenHittable(page, leadingExpandButton(item));
  // The gap is under 20 lines, so one click reveals all of it.
  for (let n = 1; n <= 7; n++) await expect(diffLine(item, n)).toBeAttached();
  await expect(diffLine(item, 1)).toContainText('package main');
  await expect(item.locator('[data-separator][data-separator-first]')).toHaveCount(0);
}

// ============================================================
// Leading Separator — Split Mode
// ============================================================
test.describe('Leading Separator — Split Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('leading separator appears before first hunk when it does not start at line 1', async ({ page }) => {
    const item = await utilsSection(page);
    await expectLeadingSeparator(item);
  });

  test('no leading separator when first hunk starts at line 1 (new file)', async ({ page }) => {
    const item = await jsSection(page);
    await expect(diffLine(item, 1)).toBeVisible();
    await expect(item.locator('[data-separator]')).toHaveCount(0);
  });

  test('clicking leading separator reveals context lines above the first hunk', async ({ page }) => {
    const item = await utilsSection(page);
    await expectLeadingSeparator(item);
    await expandLeading(page, item);
    // Revealed lines are unchanged context, on both sides.
    await expect(diffLine(item, 1)).toHaveAttribute('data-line-type', /^context/);
    await expect(diffLine(item, 1, 'old')).toContainText('package main');
  });

  test('leading separator disappears after expanding all lines to line 1', async ({ page }) => {
    const item = await utilsSection(page);
    await expect(leadingSeparator(item).first()).toBeVisible();
    await expandLeading(page, item);
    await expect(item.locator('[data-separator]')).toHaveCount(0);
  });

  test('server.go 1-line leading gap renders inline instead of a separator', async ({ page }) => {
    const item = await goSection(page);
    await expect(diffLine(item, 1)).toBeVisible();
    await expect(diffLine(item, 1)).toContainText('package main');
    await expect(diffLine(item, 1, 'old')).toContainText('package main');
    await expect(item.locator('[data-separator][data-separator-first]')).toHaveCount(0);
  });
});

// ============================================================
// Trailing Separator — Split Mode
// ============================================================
test.describe('Trailing Separator — Split Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('no trailing separator when last hunk reaches EOF (server.go)', async ({ page }) => {
    const item = await goSection(page);
    // Render the file's tail so a trailing separator would be in the DOM.
    await showLine(page, diffLine(item, 71));
    await expect(diffLine(item, 71)).toContainText('}');
    await expect(trailingSeparator(item)).toHaveCount(0);
  });

  test('no trailing separator when last hunk reaches EOF (utils.go)', async ({ page }) => {
    const item = await utilsSection(page);
    await showLine(page, diffLine(item, 19));
    await expect(trailingSeparator(item)).toHaveCount(0);
  });
});

// ============================================================
// Leading Separator — Unified Mode
// ============================================================
test.describe('Leading Separator — Unified Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToUnified(page);
  });

  test('leading separator appears in unified mode', async ({ page }) => {
    const item = await utilsSection(page);
    await expect(item.locator('code[data-unified]')).toBeVisible();
    await expectLeadingSeparator(item);
  });

  test('clicking leading separator in unified mode reveals context lines', async ({ page }) => {
    const item = await utilsSection(page);
    await expectLeadingSeparator(item);
    await expandLeading(page, item);
  });

  test('no leading separator in unified mode for new file', async ({ page }) => {
    const item = await jsSection(page);
    await expect(item.locator('code[data-unified]')).toBeVisible();
    await expect(diffLine(item, 1)).toBeVisible();
    await expect(item.locator('[data-separator]')).toHaveCount(0);
  });
});

// ============================================================
// Trailing Separator — Unified Mode
// ============================================================
test.describe('Trailing Separator — Unified Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToUnified(page);
  });

  test('no trailing separator in unified mode when last hunk reaches EOF', async ({ page }) => {
    const item = await utilsSection(page);
    await showLine(page, diffLine(item, 19));
    await expect(trailingSeparator(item)).toHaveCount(0);
  });
});

// ============================================================
// Expanded context lines are commentable
// ============================================================
test.describe('Leading Separator — Expanded Lines Are Commentable', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('expanded leading context lines can be commented on', async ({ page, request }) => {
    const item = await utilsSection(page);
    await expect(leadingSeparator(item).first()).toBeVisible();
    await expandLeading(page, item);

    const form = await openLineComment(page, item, 3);
    await form.locator('textarea').fill('Comment on expanded leading line');
    await form.locator('.btn-primary').click();

    await expect(item.locator('.comment-card .comment-body')).toContainText('Comment on expanded leading line');
    await expect.poll(async () => {
      const comments = await (await request.get('/api/file/comments?path=utils.go')).json();
      return comments.map((c: { start_line: number; end_line: number }) => `${c.start_line}-${c.end_line}`);
    }).toEqual(['3-3']);
  });
});

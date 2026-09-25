import { test, expect, type Page, type Locator } from '@playwright/test';
import {
  loadPage, goSection, revealFile, fileHeader, diffLine, diffLineNumber,
  hoverLine, selectedLines, clearAllComments,
} from './helpers';

// Git-mode files render through Pierre (see helpers.ts). server.go has
// adjacent del/add pairs; routes.go has a 37-line gap (new 15..51) between
// its two hunks, collapsed behind a separator.

async function setDiffMode(page: Page, mode: 'split' | 'unified') {
  await page.locator(`#diffModeToggle .toggle-btn[data-mode="${mode}"]`).click();
}

// Hidden-line labels of the file's separators (one per collapsed gap).
function separators(item: Locator): Locator {
  return item.locator('[data-separator] [data-unmodified-lines]').filter({ visible: true });
}

// The visible expand control of the first separator. Pierre's "up" control
// grows the hunk above the gap downward; "down" grows the hunk below upward.
function expandButton(item: Locator, which: 'below-previous-hunk' | 'above-next-hunk'): Locator {
  const dir = which === 'below-previous-hunk' ? 'up' : 'down';
  return item.locator(`[data-separator] [data-expand-button][data-expand-${dir}]`).filter({ visible: true }).first();
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
// the element is actually the hit target at its centre, so the click lands
// exactly once.
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

test.describe('Diff Rendering — Split Mode (default)', () => {
  test('shows split diff by default', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);
    await expect(item.locator('pre[data-diff-type="split"]')).toBeVisible();
    await expect(item.locator('code[data-deletions]')).toBeVisible();
    await expect(item.locator('code[data-additions]')).toBeVisible();
    await expect(item.locator('code[data-unified]')).toHaveCount(0);
  });

  test('split diff has left and right sides', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);

    // Old code on the left, new code on the right, side by side.
    const left = item.locator('code[data-deletions]');
    const right = item.locator('code[data-additions]');
    await expect(left).toBeVisible();
    await expect(right).toBeVisible();
    const l = await left.boundingBox();
    const r = await right.boundingBox();
    expect(l!.x + l!.width).toBeLessThanOrEqual(r!.x + 1);

    // server.go old line 42 was replaced by new line 67.
    await showLine(page, diffLine(item, 67));
    await expect(diffLine(item, 42, 'old')).toContainText('fmt.Println("Server starting on :8080")');
    await expect(diffLine(item, 67)).toContainText('log.Printf("Server starting on :%s", port)');
  });

  test('addition lines are marked as additions on the new side', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);
    // server.go new line 5 (`"log"` import) is added.
    const added = diffLine(item, 5);
    await expect(added).toBeVisible();
    await expect(added).toHaveAttribute('data-line-type', 'change-addition');
    await expect(added).toContainText('"log"');
    await expect(item.locator('code[data-deletions] [data-line-type="change-addition"]')).toHaveCount(0);
  });

  test('deletion lines are marked as deletions on the old side', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);
    const deleted = diffLine(item, 23, 'old');
    await showLine(page, deleted);
    await expect(deleted).toHaveAttribute('data-line-type', 'change-deletion');
    await expect(deleted).toContainText('r.URL.Path[1:]');
    await expect(item.locator('code[data-additions] [data-line-type="change-deletion"]')).toHaveCount(0);
  });

  test('deleted file shows "This file was deleted."', async ({ page }) => {
    await loadPage(page);
    const item = await revealFile(page, 'deleted.txt');
    const header = fileHeader(page, 'deleted.txt');
    await expect(header.locator('.file-header-badge.deleted')).toBeVisible();
    if (await header.evaluate(el => el.classList.contains('collapsed'))) {
      await header.locator('.file-header-name').click();
      await expect(header).not.toHaveClass(/\bcollapsed\b/);
    }
    // The expanded body must explain itself rather than render empty.
    await expect(item.getByText('This file was deleted.')).toBeVisible();
  });

  test('separator between hunks shows the hidden line count', async ({ page }) => {
    await loadPage(page);
    const item = await revealFile(page, 'routes.go');

    // routes.go: hunk 1 ends at new line 14, hunk 2 starts at 52.
    const sep = separators(item).first();
    await expect(sep).toBeVisible();
    await expect(sep).toContainText('37 unmodified lines');
    await expect(diffLine(item, 14)).toBeVisible();
    await expect(diffLine(item, 15)).toHaveCount(0);
    await expect(diffLine(item, 51)).toHaveCount(0);
  });

  test('clicking separator expands context lines', async ({ page }) => {
    await loadPage(page);
    const item = await revealFile(page, 'routes.go');

    const sep = separators(item).first();
    await expect(sep).toContainText('37 unmodified lines');
    await clickWhenHittable(page, expandButton(item, 'below-previous-hunk'));

    // expansionLineCount is 20: lines 15..34 appear below hunk 1.
    await expect(diffLine(item, 15)).toBeAttached();
    await expect(diffLine(item, 34)).toBeAttached();
    await expect(diffLine(item, 35)).toHaveCount(0);
    await expect(separators(item).first()).toContainText('17 unmodified lines');
  });

  test('expanded lines have comment gutter (+ button) on hover', async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    const item = await revealFile(page, 'routes.go');

    const sep = separators(item).first();
    await expect(sep).toBeVisible();
    await clickWhenHittable(page, expandButton(item, 'below-previous-hunk'));
    await expect(diffLine(item, 20)).toBeAttached();

    const plus = await hoverLine(page, item, 20);
    await expect(plus).toBeVisible();
  });
});

test.describe('Diff Mode Toggle', () => {
  test.afterEach(async ({ context }) => {
    await context.clearCookies();
  });

  test('can switch to unified mode', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);
    await expect(item.locator('pre[data-diff-type="split"]')).toBeVisible();

    await setDiffMode(page, 'unified');

    await expect(item.locator('code[data-unified]')).toBeVisible();
    await expect(item.locator('code[data-additions], code[data-deletions]')).toHaveCount(0);
  });

  test('unified mode shows single-pane diff lines', async ({ page }) => {
    await loadPage(page);
    await setDiffMode(page, 'unified');
    const item = await goSection(page);

    // Old and new versions of a changed line are stacked in one column.
    const code = item.locator('code[data-unified]');
    await expect(code).toBeVisible();
    const del = diffLine(item, 42, 'old');
    const add = diffLine(item, 67);
    await showLine(page, add);
    await expect(del).toContainText('fmt.Println');
    await expect(add).toContainText('log.Printf');
    const d = await del.boundingBox();
    const a = await add.boundingBox();
    expect(Math.abs(d!.x - a!.x)).toBeLessThan(2);
    expect(d!.y).toBeLessThan(a!.y);
  });

  test('unified mode marks addition lines distinctly from context', async ({ page }) => {
    await loadPage(page);
    await setDiffMode(page, 'unified');
    const item = await goSection(page);

    const added = diffLine(item, 5);
    const context = diffLine(item, 4);
    await expect(added).toHaveAttribute('data-line-type', 'change-addition');
    await expect(context).toHaveAttribute('data-line-type', /^context/);
    // The addition is visibly highlighted (its background differs from context).
    await expect.poll(async () => {
      const bgAdd = await added.evaluate(el => getComputedStyle(el).backgroundColor);
      const bgCtx = await context.evaluate(el => getComputedStyle(el).backgroundColor);
      return bgAdd !== bgCtx && bgAdd !== 'rgba(0, 0, 0, 0)';
    }).toBe(true);
  });

  test('diff mode persists across reload', async ({ page, context }) => {
    await context.clearCookies();
    await loadPage(page);
    let item = await goSection(page);
    await expect(item.locator('pre[data-diff-type="split"]')).toBeVisible();

    await setDiffMode(page, 'unified');
    await expect(item.locator('code[data-unified]')).toBeVisible();

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    item = await goSection(page);
    await expect(item.locator('code[data-unified]')).toBeVisible();
    await expect(item.locator('pre[data-diff-type="split"]')).toHaveCount(0);
  });

  test('can switch back to split', async ({ page }) => {
    await loadPage(page);
    await setDiffMode(page, 'unified');
    const item = await goSection(page);
    await expect(item.locator('code[data-unified]')).toBeVisible();

    await setDiffMode(page, 'split');

    await expect(item.locator('pre[data-diff-type="split"]')).toBeVisible();
    await expect(item.locator('code[data-unified]')).toHaveCount(0);
  });
});

test.describe('Unified Mode — Drag Indicator Across Line Types', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });
  test.afterEach(async ({ context }) => {
    await context.clearCookies();
  });

  // server.go unified order around here: 40+ | 21- | 41+ | 42 | 43+
  function unifiedSelected(item: Locator): Locator {
    return item.locator('code[data-unified] [data-content] > [data-selected-line]');
  }

  test('drag indicator shows on deletion lines when dragging from addition line', async ({ page }) => {
    await loadPage(page);
    await setDiffMode(page, 'unified');
    const item = await goSection(page);
    await expect(item.locator('code[data-unified]')).toBeVisible();
    await showLine(page, diffLine(item, 41));

    const plus = await hoverLine(page, item, 41);
    const start = await plus.boundingBox();
    const end = await diffLineNumber(item, 21, 'old').boundingBox();
    await page.mouse.move(start!.x + start!.width / 2, start!.y + start!.height / 2);
    await page.mouse.down();
    await page.mouse.move(start!.x + start!.width / 2, end!.y + end!.height / 2, { steps: 8 });

    // The deletion line the pointer is on is part of the drag range.
    await expect(unifiedSelected(item).and(item.locator('[data-line-type="change-deletion"]'))).toHaveCount(1);
    await expect(diffLine(item, 21, 'old')).toHaveAttribute('data-selected-line', /.*/);
    await expect(diffLine(item, 41)).toHaveAttribute('data-selected-line', /.*/);

    await page.mouse.up();
    await expect(page.locator('#filesContainer .comment-form textarea')).toBeVisible();
  });

  test('all lines between drag endpoints are selected in unified mode', async ({ page }) => {
    await loadPage(page);
    await setDiffMode(page, 'unified');
    const item = await goSection(page);
    await expect(item.locator('code[data-unified]')).toBeVisible();
    await showLine(page, diffLine(item, 43));

    // Drag from the deletion (old 21) down to the addition (new 43):
    // four visual rows of mixed type.
    const plus = await hoverLine(page, item, 21, 'old');
    const start = await plus.boundingBox();
    const end = await diffLineNumber(item, 43).boundingBox();
    await page.mouse.move(start!.x + start!.width / 2, start!.y + start!.height / 2);
    await page.mouse.down();
    await page.mouse.move(start!.x + start!.width / 2, end!.y + end!.height / 2, { steps: 8 });

    await expect(unifiedSelected(item)).toHaveCount(4);
    for (const line of [diffLine(item, 21, 'old'), diffLine(item, 41), diffLine(item, 42), diffLine(item, 43)]) {
      await expect(line).toHaveAttribute('data-selected-line', /.*/);
    }
    await expect(selectedLines(page)).toHaveCount(4);

    await page.mouse.up();
  });
});

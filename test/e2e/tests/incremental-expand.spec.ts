import { test, expect, type Page, type Locator } from '@playwright/test';
import {
  clearAllComments, loadPage, revealFile, diffLine, clickWhenHittable, hoverLine, showLine,
  scanDiffRows, lineRange, waitForScrollStable, waitHighlighted,
} from './helpers';

// routes.go: hunk 1 ends at new line 14 (old 10), hunk 2 starts at new 52
// (old 48). The 37 unchanged lines between them (new 15..51) are collapsed
// behind one separator; Crit asks Pierre to expand 20 lines per click.
//
// Line presence is read with scanDiffRows, which scrolls through the file:
// Pierre virtualizes rows, so a row far from the viewport is legitimately not
// attached and a per-line toBeAttached() flakes on slow runners.
function routesSection(page: Page) {
  return revealFile(page, 'routes.go');
}

function gapLabel(item: Locator): Locator {
  return item.locator('[data-separator] [data-unmodified-lines]').filter({ visible: true }).first();
}

// Pierre's "up" control grows the hunk above the gap downward; its "down"
// control grows the hunk below the gap upward.
function expandBelowPreviousHunk(item: Locator): Locator {
  return item.locator('[data-separator] [data-expand-button][data-expand-up]').filter({ visible: true }).first();
}
function expandAboveNextHunk(item: Locator): Locator {
  return item.locator('[data-separator] [data-expand-button][data-expand-down]').filter({ visible: true }).first();
}

// The rows from line 1 through 52 (first line of hunk 2), with separators,
// in visual order.
function rows(page: Page, item: Locator) {
  return scanDiffRows(page, item, 52);
}

// Click an expand control once it is the hit target, then wait for the
// separator label to change (Pierre re-renders the gap after a click, and a
// second click before that would land on the re-mounting separator). Shiki's
// first highlighted render also replaces the separators, so wait for it
// (line 14 sits just above the gap) before clicking once.
async function expand(page: Page, item: Locator, control: Locator, remaining: string | null) {
  await waitHighlighted(item, 14);
  await clickWhenHittable(page, control);
  if (remaining) await expect(gapLabel(item)).toHaveText(remaining);
  await waitForScrollStable(page);
}

test.describe('Incremental Expand — Split Mode (default)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('expanding below the previous hunk reveals 20 lines, then the rest', async ({ page }) => {
    await loadPage(page);
    const item = await routesSection(page);
    await expect(item.locator('code[data-additions]')).toBeVisible();

    // Collapsed: one separator between 14 and 52, with both controls.
    await expect(gapLabel(item)).toHaveText('37 unmodified lines');
    await expect(expandBelowPreviousHunk(item)).toBeVisible();
    await expect(expandAboveNextHunk(item)).toBeVisible();
    expect((await rows(page, item)).slice(0, 16)).toEqual([...lineRange(1, 14), 'sep', '52']);

    // First click: lines 15..34 appear below hunk 1, as context; the remainder
    // stays collapsed behind a separator.
    await expand(page, item, expandBelowPreviousHunk(item), '17 unmodified lines');
    expect((await rows(page, item)).slice(0, 36)).toEqual([...lineRange(1, 34), 'sep', '52']);
    await showLine(page, diffLine(item, 15));
    await expect(diffLine(item, 15)).toHaveAttribute('data-line-type', /^context/);

    // Expanded lines have the comment gutter (+ button) on hover.
    await showLine(page, diffLine(item, 20));
    await expect(await hoverLine(page, item, 20)).toBeVisible();

    // The separator still offers a control; the remaining 17 lines fit in one step.
    const again = item.locator('[data-separator] [data-expand-button]:not([data-expand-all-button])').filter({ visible: true }).first();
    await expect(again).toBeVisible();
    await expand(page, item, again, null);
    const all = await rows(page, item);
    expect(all.slice(0, 52)).toEqual(lineRange(1, 52));
    await expect(item.locator('[data-separator]:not([data-separator-last]) [data-unmodified-lines]').filter({ visible: true })).toHaveCount(0);
  });

  test('expanding above the next hunk reveals the 20 lines before it', async ({ page }) => {
    await loadPage(page);
    const item = await routesSection(page);
    await expect(gapLabel(item)).toHaveText('37 unmodified lines');

    await expand(page, item, expandAboveNextHunk(item), '17 unmodified lines');
    expect((await rows(page, item)).slice(0, 36)).toEqual([...lineRange(1, 14), 'sep', ...lineRange(32, 52)]);
  });
});

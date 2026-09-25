import { test, expect, type Page, type Locator } from '@playwright/test';
import { clearAllComments, loadPage, revealFile, diffLine } from './helpers';

// routes.go: hunk 1 ends at new line 14 (old 10), hunk 2 starts at new 52
// (old 48). The 37 unchanged lines between them (new 15..51) are collapsed
// behind one separator; Crit asks Pierre to expand 20 lines per click.
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

async function expectCollapsedGap(item: Locator) {
  await expect(gapLabel(item)).toHaveText('37 unmodified lines');
  await expect(diffLine(item, 14)).toBeVisible();
  await expect(diffLine(item, 15)).toHaveCount(0);
  await expect(diffLine(item, 51)).toHaveCount(0);
  await expect(diffLine(item, 52)).toBeAttached();
}

async function expectLines(item: Locator, from: number, to: number) {
  for (let n = from; n <= to; n++) await expect(diffLine(item, n)).toBeAttached();
}

// Shared body for split and unified.
function defineExpandTests() {
  test('large gap separator shows expand-down and expand-up controls', async ({ page }) => {
    const item = await routesSection(page);
    await expectCollapsedGap(item);
    await expect(expandBelowPreviousHunk(item)).toBeVisible();
    await expect(expandAboveNextHunk(item)).toBeVisible();
  });

  test('expanding below the previous hunk reveals the next 20 lines', async ({ page }) => {
    const item = await routesSection(page);
    await expectCollapsedGap(item);

    await clickWhenHittable(page, expandBelowPreviousHunk(item));

    await expectLines(item, 15, 34);
    await expect(diffLine(item, 35)).toHaveCount(0);
    await expect(diffLine(item, 15)).toHaveAttribute('data-line-type', /^context/);
    // The remainder stays collapsed behind a separator.
    await expect(gapLabel(item)).toHaveText('17 unmodified lines');
  });

  test('expanding above the next hunk reveals the 20 lines before it', async ({ page }) => {
    const item = await routesSection(page);
    await expectCollapsedGap(item);

    await clickWhenHittable(page, expandAboveNextHunk(item));

    await expectLines(item, 32, 51);
    await expect(diffLine(item, 31)).toHaveCount(0);
    await expect(diffLine(item, 15)).toHaveCount(0);
    await expect(gapLabel(item)).toHaveText('17 unmodified lines');
  });

  test('after partial expansion, separator still expands the rest', async ({ page }) => {
    const item = await routesSection(page);
    await expectCollapsedGap(item);

    await clickWhenHittable(page, expandBelowPreviousHunk(item));
    await expect(gapLabel(item)).toHaveText('17 unmodified lines');

    // A control is still offered; the remaining 17 lines fit in one step.
    const again = item.locator('[data-separator] [data-expand-button]:not([data-expand-all-button])').filter({ visible: true }).first();
    await expect(again).toBeVisible();
    await clickWhenHittable(page, again);

    await expectLines(item, 15, 51);
    await expect(item.locator('[data-separator]:not([data-separator-last]) [data-unmodified-lines]').filter({ visible: true })).toHaveCount(0);
  });
}

test.describe('Incremental Expand — Split Mode (default)', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    const item = await routesSection(page);
    await expect(item.locator('code[data-additions]')).toBeVisible();
  });

  defineExpandTests();
});

test.describe('Incremental Expand — Unified Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await routesSection(page);
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    const item = await routesSection(page);
    await expect(item.locator('code[data-unified]')).toBeVisible();
  });

  defineExpandTests();
});

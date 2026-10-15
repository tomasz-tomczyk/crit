import { test, expect, type Page } from '@playwright/test';
import { loadPage } from './helpers';

function tocLink(page: Page, text: string) {
  return page.locator('.toc-list a', { hasText: text });
}

// The document block a TOC link points at.
async function blockFor(page: Page, text: string) {
  const startLine = await tocLink(page, text).getAttribute('data-start-line');
  expect(startLine, `${text} link has a start line`).toBeTruthy();
  return page.locator(`.line-block[data-start-line="${startLine}"]`);
}

test.describe('Table of Contents — Single File Mode', () => {
  test('TOC lists nested headings and clicking entries scrolls to them', async ({ page }) => {
    await loadPage(page);

    // The toggle is shown for single-file markdown; the panel starts hidden.
    const toggle = page.locator('#tocToggle');
    await expect(toggle).toBeVisible();
    await expect(page.locator('#toc')).toHaveClass(/toc-hidden/);
    await toggle.click();
    const tocList = page.locator('.toc-list');
    await expect(tocList).toBeVisible();

    // Entries for the headings in plan.md, top heading first.
    const items = tocList.locator('a');
    await expect(items).not.toHaveCount(0);
    await expect(items.first()).toContainText('Authentication Plan');

    // h3 entries are indented further than h2 entries.
    const overviewPadding = await tocLink(page, 'Overview').evaluate(el => parseInt((el as HTMLElement).style.paddingLeft));
    const stepPadding = await tocLink(page, 'Step 1: Auth Middleware').evaluate(el => parseInt((el as HTMLElement).style.paddingLeft));
    expect(stepPadding).toBeGreaterThan(overviewPadding);

    // Clicking entries (far down, back up, then down again) scrolls each heading into view.
    for (const heading of ['Timeline', 'Overview', 'Open Questions']) {
      const block = await blockFor(page, heading);
      await tocLink(page, heading).click();
      await expect(block, heading).toBeInViewport();
    }
  });

  test('TOC open and closed state persist across page reload', async ({ page }) => {
    await loadPage(page);
    const toc = page.locator('#toc');

    // Starts hidden; opening it survives a reload.
    await expect(toc).toHaveClass(/toc-hidden/);
    await page.locator('#tocToggle').click();
    await expect(toc).not.toHaveClass(/toc-hidden/);
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(toc).not.toHaveClass(/toc-hidden/);

    // Closing it via the close button also survives a reload.
    await page.locator('.toc-close').click();
    await expect(toc).toHaveClass(/toc-hidden/);
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(toc).toHaveClass(/toc-hidden/);
  });
});

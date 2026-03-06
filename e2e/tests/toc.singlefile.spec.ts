import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

test.describe('Table of Contents — Single File Mode', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
  });

  test('TOC toggle button is visible for single-file markdown', async ({ page }) => {
    const tocToggle = page.locator('#tocToggle');
    await expect(tocToggle).toBeVisible();
  });

  test('TOC panel contains heading entries', async ({ page }) => {
    // Open TOC
    await page.locator('#tocToggle').click();
    const tocList = page.locator('.toc-list');
    await expect(tocList).toBeVisible();

    // Should have entries for all headings in plan.md
    const items = tocList.locator('a');
    await expect(items).not.toHaveCount(0);
    await expect(items.first()).toContainText('Authentication Plan');
  });

  test('clicking a TOC entry scrolls to that heading', async ({ page }) => {
    // Open TOC
    await page.locator('#tocToggle').click();
    const tocList = page.locator('.toc-list');
    await expect(tocList).toBeVisible();

    // Find a heading that's further down the page (not already visible at top)
    const timelineLink = tocList.locator('a', { hasText: 'Timeline' });
    await expect(timelineLink).toBeVisible();

    // Get the line block for "Timeline" heading
    const startLine = await timelineLink.getAttribute('data-start-line');

    // Click the TOC entry
    await timelineLink.click();

    // The corresponding line-block should be scrolled into view
    const targetBlock = page.locator(`.line-block[data-start-line="${startLine}"]`);
    await expect(targetBlock).toBeInViewport({ timeout: 3000 });
  });

  test('clicking different TOC entries navigates to different headings', async ({ page }) => {
    await page.locator('#tocToggle').click();
    const tocList = page.locator('.toc-list');

    // Click "Overview" heading
    const overviewLink = tocList.locator('a', { hasText: 'Overview' });
    await overviewLink.click();
    const overviewLine = await overviewLink.getAttribute('data-start-line');
    const overviewBlock = page.locator(`.line-block[data-start-line="${overviewLine}"]`);
    await expect(overviewBlock).toBeInViewport({ timeout: 3000 });

    // Now click "Open Questions" — a heading much further down
    const questionsLink = tocList.locator('a', { hasText: 'Open Questions' });
    await questionsLink.click();
    const questionsLine = await questionsLink.getAttribute('data-start-line');
    const questionsBlock = page.locator(`.line-block[data-start-line="${questionsLine}"]`);
    await expect(questionsBlock).toBeInViewport({ timeout: 3000 });
  });

  test('TOC open state persists across page reload', async ({ page }) => {
    const toc = page.locator('#toc');

    // TOC starts hidden
    await expect(toc).toHaveClass(/toc-hidden/);

    // Open it
    await page.locator('#tocToggle').click();
    await expect(toc).not.toHaveClass(/toc-hidden/);

    // Reload the page — TOC should still be open
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(page.locator('#toc')).not.toHaveClass(/toc-hidden/);
  });

  test('TOC closed state persists across page reload', async ({ page }) => {
    const toc = page.locator('#toc');

    // Open TOC first
    await page.locator('#tocToggle').click();
    await expect(toc).not.toHaveClass(/toc-hidden/);

    // Close it via the close button
    await page.locator('.toc-close').click();
    await expect(toc).toHaveClass(/toc-hidden/);

    // Reload — TOC should stay closed
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(page.locator('#toc')).toHaveClass(/toc-hidden/);
  });

  test('TOC shows nested headings with indentation', async ({ page }) => {
    await page.locator('#tocToggle').click();
    const tocList = page.locator('.toc-list');

    // h3 headings should have more padding than h2 headings
    const overviewLink = tocList.locator('a', { hasText: 'Overview' });
    const stepLink = tocList.locator('a', { hasText: 'Step 1: Auth Middleware' });

    const overviewPadding = await overviewLink.evaluate(el => parseInt(el.style.paddingLeft));
    const stepPadding = await stepLink.evaluate(el => parseInt(el.style.paddingLeft));

    expect(stepPadding).toBeGreaterThan(overviewPadding);
  });
});

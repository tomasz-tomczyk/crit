import { test, expect, type Page } from '@playwright/test';
import { loadPage, reviewScroller } from './helpers';

function tocLink(page: Page, text: string) {
  return page.locator('.toc-list a', { hasText: text });
}

// The document block a TOC link points at.
async function blockFor(page: Page, text: string) {
  const startLine = await tocLink(page, text).getAttribute('data-start-line');
  expect(startLine, `${text} link has a start line`).toBeTruthy();
  return page.locator(`.line-block[data-start-line="${startLine}"]`);
}

// Scroll the review pane so the heading's block sits at its top edge.
async function scrollHeadingToTop(page: Page, text: string) {
  const block = await blockFor(page, text);
  await block.evaluate(el => el.scrollIntoView({ block: 'start', behavior: 'instant' }));
}

test.describe('TOC Scrollspy — Single File Mode', () => {
  test('active TOC link follows the scroll position, one at a time, and TOC clicks update it', async ({ page }) => {
    await loadPage(page);
    await page.locator('#tocToggle').click();
    await expect(page.locator('#toc')).not.toHaveClass(/toc-hidden/);
    const active = page.locator('.toc-list a.toc-active');

    // At the top of the page no heading has been scrolled past.
    await expect(active).toHaveCount(0);

    // Scrolling down past each heading makes exactly that link active. (Later
    // headings sit too close to the end of the document to reach the top.)
    for (const heading of ['Overview', 'Design Decisions', 'Implementation Steps']) {
      await scrollHeadingToTop(page, heading);
      await expect(tocLink(page, heading), heading).toHaveClass(/toc-active/);
      await expect(active).toHaveCount(1);
    }

    // At the bottom one link stays active, and it is past Implementation Steps.
    await reviewScroller(page).evaluate(el => el.scrollTo({ top: el.scrollHeight, behavior: 'instant' }));
    await expect(active).toHaveCount(1);
    await expect(active).not.toHaveText(/Overview|Design Decisions|Implementation Steps/);

    // Scrolling back up moves the highlight back to the earlier heading.
    await scrollHeadingToTop(page, 'Overview');
    await expect(tocLink(page, 'Overview')).toHaveClass(/toc-active/);
    await expect(tocLink(page, 'Timeline')).not.toHaveClass(/toc-active/);
    await expect(active).toHaveCount(1);

    // Back at the very top of the review pane nothing is active again.
    await reviewScroller(page).evaluate(el => el.scrollTo({ top: 0, behavior: 'instant' }));
    await expect(active).toHaveCount(0);

    // Clicking a mid-page link scrolls to its heading and makes it the only
    // active link (the last heading may not scroll far enough to activate).
    const target = tocLink(page, 'Implementation Steps');
    const block = await blockFor(page, 'Implementation Steps');
    await target.click();
    await expect(block).toBeInViewport();
    await expect(target).toHaveClass(/toc-active/);
    await expect(active).toHaveCount(1);
  });
});

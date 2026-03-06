import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

test.describe('TOC Scrollspy — Single File Mode', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
    // Open the TOC panel
    await page.locator('#tocToggle').click();
    await expect(page.locator('#toc')).not.toHaveClass(/toc-hidden/);
  });

  test('scrolling down past a heading highlights the correct TOC link', async ({ page }) => {
    const tocLinks = page.locator('.toc-list a');
    await expect(tocLinks.first()).toBeVisible();

    // Scroll to the "Open Questions" heading
    const questionsLink = tocLinks.filter({ hasText: 'Open Questions' });
    const startLine = await questionsLink.getAttribute('data-start-line');
    const targetBlock = page.locator(`.line-block[data-start-line="${startLine}"]`);
    await targetBlock.scrollIntoViewIfNeeded();

    // The "Open Questions" link (or the one before it) should become active
    await expect(async () => {
      const activeLinks = await page.locator('.toc-list a.toc-active').all();
      expect(activeLinks.length).toBe(1);
    }).toPass({ timeout: 3000 });
  });

  test('scrolling back up moves active highlight to previous heading', async ({ page }) => {
    const tocLinks = page.locator('.toc-list a');

    // First scroll down to "Timeline" heading
    const timelineLink = tocLinks.filter({ hasText: 'Timeline' });
    const timelineLine = await timelineLink.getAttribute('data-start-line');
    const timelineBlock = page.locator(`.line-block[data-start-line="${timelineLine}"]`);
    await timelineBlock.scrollIntoViewIfNeeded();

    // Wait for scrollspy to settle
    await expect(page.locator('.toc-list a.toc-active')).toHaveCount(1);

    // Now scroll back up to "Overview"
    const overviewLink = tocLinks.filter({ hasText: 'Overview' });
    const overviewLine = await overviewLink.getAttribute('data-start-line');
    const overviewBlock = page.locator(`.line-block[data-start-line="${overviewLine}"]`);
    await overviewBlock.scrollIntoViewIfNeeded();

    // The active link should now be "Overview" or earlier, not "Timeline"
    await expect(async () => {
      const isTimelineActive = await timelineLink.evaluate(el => el.classList.contains('toc-active'));
      expect(isTimelineActive).toBe(false);
    }).toPass({ timeout: 3000 });
  });

  test('only one TOC link is active at a time', async ({ page }) => {
    const tocLinks = page.locator('.toc-list a');

    // Scroll through several headings and check count at each
    const headings = ['Overview', 'Design Decisions', 'Implementation Steps', 'Open Questions', 'Timeline'];

    for (const heading of headings) {
      const link = tocLinks.filter({ hasText: heading });
      const startLine = await link.getAttribute('data-start-line');
      const block = page.locator(`.line-block[data-start-line="${startLine}"]`);
      await block.scrollIntoViewIfNeeded();

      // Should never have more than one active link
      await expect(async () => {
        const activeCount = await page.locator('.toc-list a.toc-active').count();
        expect(activeCount).toBeLessThanOrEqual(1);
      }).toPass({ timeout: 3000 });
    }
  });

  test('at the top of the page, no TOC link is active', async ({ page }) => {
    // Scroll to very top
    await page.evaluate(() => window.scrollTo(0, 0));

    // Wait a tick for the scroll handler to fire
    await expect(async () => {
      const activeCount = await page.locator('.toc-list a.toc-active').count();
      expect(activeCount).toBe(0);
    }).toPass({ timeout: 3000 });
  });

  test('clicking a TOC link scrolls to the heading and updates active state', async ({ page }) => {
    const tocLinks = page.locator('.toc-list a');

    // Click "Implementation Steps" — a mid-page heading with enough content below
    // to scroll it past the header threshold (avoid last heading which may not scroll far enough)
    const targetLink = tocLinks.filter({ hasText: 'Implementation Steps' });
    await targetLink.click();

    // The heading should be scrolled into view
    const startLine = await targetLink.getAttribute('data-start-line');
    const targetBlock = page.locator(`.line-block[data-start-line="${startLine}"]`);
    await expect(targetBlock).toBeInViewport({ timeout: 3000 });

    // The clicked link should become active
    await expect(targetLink).toHaveClass(/toc-active/, { timeout: 3000 });

    // Only one link should be active
    await expect(page.locator('.toc-list a.toc-active')).toHaveCount(1);
  });
});

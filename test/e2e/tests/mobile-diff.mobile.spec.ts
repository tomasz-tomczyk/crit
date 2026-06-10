import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, goSection } from './helpers';

// F5: unified diff on mobile — at ≤768px the diff renders in unified mode
// only, and the page has no horizontal overflow even on files with long
// lines. Split mode is unusable in a narrow viewport.
test.describe('Mobile diff layout (F5)', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('code file renders in unified diff mode, not split', async ({ page }) => {
    const section = goSection(page);
    await expect(section).toBeVisible();

    // Unified diff container should be present
    await expect(section.locator('.diff-container.unified')).toBeVisible();

    // Split diff container should NOT be present
    await expect(section.locator('.diff-container.split')).toHaveCount(0);
  });

  test('diff content stays within its container width', async ({ page }) => {
    // The Chrome union-bounding-box gotcha on wrapping inline spans (e.g.
    // .hljs-string that spans multiple visual lines) would push the diff's
    // own scrollWidth past its clientWidth, which in turn widens the page.
    // The CSS fix (overflow:clip on .diff-content + display:inline-block
    // max-width:100% overflow:hidden on .diff-content span) constrains each
    // span's reported width to its container. This test catches a regression
    // of that fix more reliably than asserting CSS properties.
    await expect(goSection(page).locator('.diff-container')).toBeVisible();
    const widths = await page.evaluate(() => {
      const sec = document.querySelector('.diff-container');
      if (!sec) return null;
      return { scroll: sec.scrollWidth, client: sec.clientWidth };
    });
    expect(widths).not.toBeNull();
    // Allow 1px tolerance for sub-pixel rounding on some platforms.
    expect(widths!.scroll).toBeLessThanOrEqual(widths!.client + 1);
  });
});

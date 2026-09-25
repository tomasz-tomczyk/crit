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
    const section = await goSection(page);

    // Unified diff column should be present
    await expect(section.locator('code[data-unified]')).toBeVisible();

    // Split columns should NOT be present
    await expect(section.locator('code[data-additions], code[data-deletions]')).toHaveCount(0);
  });

  test('diff content stays within its container width', async ({ page }) => {
    // Long code lines (and wrapping Shiki token spans) must scroll or wrap
    // inside the diff, never widen the file item or the page.
    const section = await goSection(page);
    await expect(section.locator('code[data-unified] [data-line]').first()).toBeVisible();
    const widths = await section.evaluate((el) => {
      const pane = document.getElementById('filesContainer')!;
      return {
        item: { scroll: el.scrollWidth, client: el.clientWidth },
        pane: { scroll: pane.scrollWidth, client: pane.clientWidth },
        page: { scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth },
      };
    });
    // Allow 1px tolerance for sub-pixel rounding on some platforms.
    expect(widths.item.scroll).toBeLessThanOrEqual(widths.item.client + 1);
    expect(widths.pane.scroll).toBeLessThanOrEqual(widths.pane.client + 1);
    expect(widths.page.scroll).toBeLessThanOrEqual(widths.page.client + 1);
  });
});

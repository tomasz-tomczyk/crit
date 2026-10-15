import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, goSection } from './helpers';

// F5 + F6: mobile diff and file-header layout. At ≤768px the diff renders in
// unified mode only, the file header has room for the filename, and nothing
// (long lines, long paths, the viewed checkbox) scrolls the page sideways.
test.describe('Mobile diff and file-header layout (F5, F6)', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('file header: viewed checkbox hidden, filename has width, and it lays out as two rows', async ({ page }) => {
    const section = await goSection(page);
    const header = section.locator('.pierre-file-header');

    // The "Viewed" checkbox has margin-left:auto which consumes all flex
    // space in the header and pushes the filename past the viewport. It is
    // rendered, but hidden on mobile so the filename has room.
    const viewed = header.locator('.file-header-viewed');
    await expect(viewed).toHaveCount(1);
    await expect(viewed).toBeHidden();

    const filename = header.locator('.file-header-name');
    await expect(filename).toBeVisible();
    await expect.poll(async () => (await filename.boundingBox())?.width ?? 0).toBeGreaterThan(0);

    // Row 1 = chevron + icon + name + comment-btn. Row 2 = toggle (markdown
    // only) + badge + stats. The badge starts on its own row even on code
    // files with no toggle: the badge sits below the comment button.
    const commentBtn = header.locator('.file-comment-btn');
    const badge = header.locator('.file-header-badge');
    await expect(commentBtn).toBeVisible();
    await expect(badge).toBeVisible();
    await expect.poll(async () => {
      const [b, c] = [await badge.boundingBox(), await commentBtn.boundingBox()];
      return !!b && !!c && b.y > c.y;
    }).toBe(true);
  });

  test('unified diff only, and no horizontal overflow — even with a very long path', async ({ page }) => {
    const section = await goSection(page);
    // Unified column present, split columns absent.
    await expect(section.locator('code[data-unified] [data-line]').first()).toBeVisible();
    await expect(section.locator('code[data-additions], code[data-deletions]')).toHaveCount(0);

    // Long code lines (and wrapping Shiki token spans) must scroll or wrap
    // inside the diff, never widen the file item, the pane or the page.
    // Allow 1px tolerance for sub-pixel rounding on some platforms.
    const widths = await section.evaluate((el) => {
      const pane = document.getElementById('filesContainer')!;
      return {
        item: { scroll: el.scrollWidth, client: el.clientWidth },
        pane: { scroll: pane.scrollWidth, client: pane.clientWidth },
        page: { scroll: document.documentElement.scrollWidth, client: document.documentElement.clientWidth },
      };
    });
    expect(widths.item.scroll).toBeLessThanOrEqual(widths.item.client + 1);
    expect(widths.pane.scroll).toBeLessThanOrEqual(widths.pane.client + 1);
    expect(widths.page.scroll).toBeLessThanOrEqual(widths.page.client + 1);

    // Real sessions can have long nested paths (.claude/rules/frontend-
    // architecture.md) that, without `min-width: 0` + `overflow: hidden` on
    // .file-header-name, push the page wider than the viewport.
    // Inject and measure in one evaluate: Pierre can re-render the header at
    // any time, which would silently restore the short name before a later
    // measurement.
    const after = await section.locator('.file-header-name').evaluate((el) => {
      el.innerHTML =
        '<span class="dir">.claude/rules/very/deeply/nested/directory/structure/</span>' +
        '<span class="filename">extremely-long-filename-that-would-overflow-the-mobile-viewport.md</span>' +
        '<button class="file-header-copy-path">copy</button>';
      return {
        injected: (el.textContent || '').includes('extremely-long-filename'),
        scroll: document.documentElement.scrollWidth,
        client: document.documentElement.clientWidth,
      };
    });
    expect(after.injected).toBe(true);
    expect(after.scroll).toBeLessThanOrEqual(after.client + 1);
  });
});

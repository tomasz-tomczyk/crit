import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, goSection } from './helpers';

// F6: mobile review-mode controls — the file-header has room for the
// filename, doesn't crowd, and the page no longer scrolls horizontally
// because of file-header-viewed checkboxes that overflow.
test.describe('Mobile file-header layout (F6)', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('file-header-viewed checkbox is hidden on mobile', async ({ page }) => {
    // The "Viewed" checkbox has margin-left:auto which consumes all flex
    // space in the header and pushes the filename past the viewport. Hide
    // it on mobile so the filename has room.
    const viewed = page.locator('.file-header-viewed').first();
    await expect(viewed).toBeHidden();
  });

  test('filename has positive visible width on mobile', async ({ page }) => {
    const section = goSection(page);
    await expect(section).toBeVisible();
    const filename = section.locator('.file-header-name').first();
    const box = await filename.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.width).toBeGreaterThan(0);
  });

  test('file-header-name does not cause page overflow even with very long paths', async ({ page }) => {
    // The git-mode fixture has short paths (server.go, plan.md). Real-world
    // sessions can have long deeply-nested paths (.claude/rules/frontend-
    // architecture.md) that, without `min-width: 0` + `overflow: hidden` on
    // .file-header-name, push the entire page wider than viewport. Reproduce
    // by injecting a long path into the first file-header-name and asserting
    // page width stays within viewport.
    const fileHeader = page.locator('.file-header-name').first();
    await expect(fileHeader).toBeVisible();
    await fileHeader.evaluate((el) => {
      el.innerHTML =
        '<span class="dir">.claude/rules/very/deeply/nested/directory/structure/</span>' +
        '<span class="filename">extremely-long-filename-that-would-overflow-the-mobile-viewport.md</span>' +
        '<button class="file-header-copy-path">copy</button>';
    });
    const widths = await page.evaluate(() => ({
      scroll: document.documentElement.scrollWidth,
      client: document.documentElement.clientWidth,
    }));
    expect(widths.scroll).toBeLessThanOrEqual(widths.client + 1);
  });

  test('file-header lays out as two rows on mobile', async ({ page }) => {
    // Row 1 = chevron + icon + name + comment-btn. Row 2 = toggle (markdown
    // only) + badge + stats. The badge starts on its own row even on code
    // files with no toggle. Assert via vertical positioning: the comment
    // button on row 1 has a smaller y than the badge on row 2.
    const fileHeader = page.locator('.file-header').first();
    await expect(fileHeader).toBeVisible();
    const positions = await fileHeader.evaluate((el) => {
      const commentBtn = el.querySelector('.file-comment-btn');
      const badge = el.querySelector('.file-header-badge');
      return {
        commentBtnTop: commentBtn ? commentBtn.getBoundingClientRect().top : null,
        badgeTop: badge ? badge.getBoundingClientRect().top : null,
      };
    });
    // Only meaningful if both elements rendered (file may be untracked
    // without a badge, but the git-mode fixture's first file has both).
    if (positions.commentBtnTop !== null && positions.badgeTop !== null) {
      expect(positions.badgeTop).toBeGreaterThan(positions.commentBtnTop);
    }
  });

  test('page has no horizontal scroll at mobile viewport', async ({ page }) => {
    // The full assertion that was deferred from F1 and F5. With F6's
    // .file-header-viewed hide, no remaining element should push the page
    // past the viewport.
    await expect(goSection(page).locator('.diff-container')).toBeVisible();
    const widths = await page.evaluate(() => ({
      scroll: document.documentElement.scrollWidth,
      client: document.documentElement.clientWidth,
    }));
    expect(widths.scroll).toBeLessThanOrEqual(widths.client + 1);
  });
});

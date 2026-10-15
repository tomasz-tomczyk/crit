import { test, expect, type Page } from '@playwright/test';
import { clearAllComments, loadPage, fileHeader, goSection, openLineComment, reviewScroller } from './helpers';

function treeFile(page: Page, name: string) {
  return page.locator('.tree-file', { has: page.locator('.tree-file-name', { hasText: name }) });
}

// ============================================================
// File Tree Panel — Git Mode
// ============================================================
test.describe('File Tree — Git Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('tree lists every session file with names, stats and status icons', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('#fileTreePanel')).toBeVisible();

    // Git fixture has: plan.md, skill.md, server.go, handler.js, deleted.txt, routes.go, legacy.go (committed)
    // + utils.go, login.feature (staged), config.yaml (untracked) = 10 total
    await expect(page.locator('.tree-file')).toHaveCount(10);
    const tree = page.locator('#fileTreePanel');
    for (const name of ['plan.md', 'server.go', 'handler.js', 'deleted.txt', 'utils.go', 'config.yaml']) {
      await expect(tree.locator('.tree-file-name', { hasText: name })).toBeVisible();
    }

    // Header: file count, addition and deletion stats.
    const stats = page.locator('#fileTreeStats');
    await expect(stats).toBeVisible();
    await expect(stats).toContainText('10');
    await expect(page.locator('#fileTreeStats .tree-stat-add')).toHaveText(/\+\d+/);
    await expect(page.locator('#fileTreeStats .tree-stat-del')).toHaveText(/-\d+/);

    // plan.md, skill.md, handler.js, login.feature, and config.yaml (untracked) are added
    await expect(page.locator('.tree-file-status-icon.added')).toHaveCount(5);
    // server.go, routes.go, legacy.go, and utils.go (staged modification) are modified
    await expect(page.locator('.tree-file-status-icon.modified')).toHaveCount(4);
    // deleted.txt is deleted
    await expect(page.locator('.tree-file-status-icon.deleted')).toHaveCount(1);
  });

  test('header toggle hides the tree, content takes the space, and the state survives a reload', async ({ page }) => {
    await loadPage(page);
    const panel = page.locator('#fileTreePanel');
    const toggle = page.locator('#fileTreeToggle');
    await expect(panel).toBeVisible();
    const openBox = (await page.locator('.main-content').boundingBox())!;

    await toggle.click();
    await expect(panel).toBeHidden();
    await expect(page.locator('#fileTreeResizer')).toBeHidden();
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');

    // Content expands into the vacated sidebar: left edge moves left (not
    // toward center), width grows. toPass() lets the slide transition settle.
    await expect(async () => {
      const layout = await page.locator('.main-layout').boundingBox();
      const main = await page.locator('.main-content').boundingBox();
      if (!layout || !main) throw new Error('missing layout boxes');
      expect(main.width).toBeGreaterThan(openBox.width - 1);
      expect(main.x).toBeLessThan(openBox.x - 10);
      const left = main.x - layout.x;
      expect(left).toBeLessThan(2);
      // No equal gutters — spare space (if any) is on the right only.
      const right = (layout.x + layout.width) - (main.x + main.width);
      expect(right).toBeGreaterThanOrEqual(left - 1);
    }).toPass();

    // The collapsed resizer is not tabbable.
    await expect(page.locator('#fileTreeResizer')).toHaveCSS('visibility', 'hidden');
    const focused = await page.evaluate(() => {
      const r = document.getElementById('fileTreeResizer');
      if (!r) return 'missing';
      r.focus();
      return document.activeElement === r ? 'focused' : 'skipped';
    });
    expect(focused).toBe('skipped');

    // Collapsed state survives a reload (persisted in the crit-settings cookie).
    await loadPage(page);
    await expect(page.locator('#fileTreePanel')).toBeHidden();
    await page.locator('#fileTreeToggle').click();
    await expect(page.locator('#fileTreePanel')).toBeVisible();
  });

  test('b toggles the file tree, and is a no-op when the toggle is CSS-hidden (mobile)', async ({ page }) => {
    await loadPage(page);
    const panel = page.locator('#fileTreePanel');
    await expect(panel).toBeVisible();
    await page.locator('body').press('b');
    await expect(panel).toBeHidden();
    await page.locator('body').press('b');
    await expect(panel).toBeVisible();

    await page.setViewportSize({ width: 375, height: 812 });
    await expect(page.locator('#fileTreeToggle')).toBeHidden();
    await page.locator('body').press('b');
    // Mobile already hides the tree via CSS; ensure we did not write a
    // collapsed cookie that would stick after restoring desktop width.
    await page.setViewportSize({ width: 1280, height: 800 });
    await expect(page.locator('#fileTreePanel')).toBeVisible();
    await expect(page.locator('body')).not.toHaveClass(/file-tree-collapsed/);
  });

  test('clicking a file scrolls its header to the top and marks only it active', async ({ page }) => {
    await loadPage(page);

    await treeFile(page, 'handler.js').click();
    await expect(fileHeader(page, 'handler.js')).toBeInViewport();

    const serverFile = treeFile(page, 'server.go');
    await serverFile.click();
    await expect(serverFile).toHaveClass(/active/);
    const handlerFile = treeFile(page, 'handler.js');
    await handlerFile.click();
    await expect(handlerFile).toHaveClass(/active/);
    await expect(serverFile).not.toHaveClass(/active/);

    // From the bottom, plan.md's header lands just below the sticky header.
    // CodeView scrolls #filesContainer, not the window.
    await reviewScroller(page).evaluate(el => { el.scrollTop = el.scrollHeight; });
    await expect(fileHeader(page, 'plan.md')).not.toBeInViewport();
    await treeFile(page, 'plan.md').click();
    await expect(async () => {
      const box = await fileHeader(page, 'plan.md').boundingBox();
      expect(box).toBeTruthy();
      // Should be positioned just below the sticky header (~49px + 8px padding)
      expect(box!.y).toBeLessThan(100);
    }).toPass();
  });
});

// ============================================================
// File Tree Comment Badges — Git Mode
// ============================================================
test.describe('File Tree Comment Badges — Git Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('comment badge appears when a comment is added and goes when it is deleted', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('.tree-file').first()).toBeVisible();
    await expect(page.locator('.tree-comment-badge')).toHaveCount(0);

    const section = await goSection(page);
    const form = await openLineComment(page, section, 1);
    await form.locator('textarea').fill('Badge test');
    await form.locator('.btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();

    const badge = treeFile(page, 'server.go').locator('.tree-comment-badge');
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText('1');

    await section.locator('.comment-actions .delete-btn').click();
    await expect(section.locator('.comment-card')).toHaveCount(0);
    await expect(badge).toHaveCount(0);
  });
});

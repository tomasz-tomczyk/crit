import { test, expect, type Page } from '@playwright/test';
import {
  addComment,
  clearAllComments,
  dragBetween,
  goSection,
  loadPage,
} from './helpers';

/**
 * Always-on code-diff virtualization safety net.
 *
 * Existing suites (diff-rendering, drag-selection, comments, draft-autosave,
 * comment-nav, scroll-preservation) now hit the virtual path by default.
 * This file covers contracts unique to windowing: the virtualized class,
 * remount-safe comments/forms, and split↔unified restore.
 */

/** Sidebar jump mounts deferred / off-window bodies before assertions. */
async function focusServerGo(page: Page) {
  const tree = page.locator('.tree-file[data-tree-path="server.go"]');
  await expect(tree).toBeVisible();
  await tree.click();
  const section = goSection(page);
  await expect(section).toBeVisible();
  await expect(section.locator('.file-body:not([data-body-deferred])')).toBeVisible({
    timeout: 10_000,
  });
}

test.describe('Diff virtualization', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('code diffs mount through the virtualized surface', async ({ page }) => {
    await loadPage(page);
    await focusServerGo(page);

    const split = goSection(page).locator('.diff-container.split.virtualized');
    await expect(split).toBeVisible();
    await expect(split).toHaveClass(/diff-virtual-surface|virtualized/);

    // Surface owns a controller and only a window of rows (spacers allowed).
    const hasController = await split.evaluate((el) => !!el._critVirtualWindow);
    expect(hasController).toBe(true);

    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    const unified = goSection(page).locator('.diff-container.unified.virtualized');
    await expect(unified).toBeVisible();
    expect(await unified.evaluate((el) => !!el._critVirtualWindow)).toBe(true);
  });

  test('API comment survives scroll and remains reachable via comment nav', async ({ page, request }) => {
    await loadPage(page);
    // server.go line 2 is a stable early hunk in the git fixture.
    const comment = await addComment(request, 'server.go', 2, 'Virtualization e2e: scroll remount');
    expect(comment.id).toBeTruthy();

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await focusServerGo(page);

    const section = goSection(page);
    const card = section.locator(`.comment-card[data-comment-id="${comment.id}"]`);
    await expect(card).toBeVisible({ timeout: 5_000 });

    // Force a long scroll so windowing would recycle distant rows if present.
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
    await page.waitForTimeout(100);
    await page.evaluate(() => window.scrollTo(0, 0));

    // Comment nav is model-first: after scroll-away it must remount and highlight.
    await expect(page.locator('#commentNavGroup')).toHaveClass(/has-comments/);
    await page.locator('#commentNavNext').click();
    await expect(card).toBeVisible();
    await expect(card).toHaveClass(/comment-nav-highlight/);
    await expect(card).toContainText('Virtualization e2e: scroll remount');
  });

  test('open draft form stays filled after split↔unified toggle', async ({ page }) => {
    await loadPage(page);
    await focusServerGo(page);
    const section = goSection(page);

    // Same affordance as drag-selection: hover addition side → click comment btn.
    const additionSide = section.locator('.diff-split-side.addition').first();
    await expect(additionSide).toBeVisible();
    await additionSide.hover();
    const commentBtn = additionSide.locator('.diff-comment-btn');
    await expect(commentBtn).toBeVisible();
    await commentBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible({ timeout: 5_000 });
    await textarea.fill('Pinned draft across remount');

    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    await expect(goSection(page).locator('.diff-container.unified.virtualized')).toBeVisible();

    const restored = page.locator('.comment-form textarea');
    await expect(restored).toBeVisible({ timeout: 5_000 });
    await expect(restored).toHaveValue('Pinned draft across remount');

    await page.locator('#diffModeToggle .toggle-btn[data-mode="split"]').click();
    await expect(goSection(page).locator('.diff-container.split.virtualized')).toBeVisible();
    await expect(page.locator('.comment-form textarea')).toHaveValue('Pinned draft across remount');
  });

  test('gutter drag still opens a multi-line form on a virtualized split diff', async ({ page }) => {
    await loadPage(page);
    await focusServerGo(page);
    const section = goSection(page);
    await expect(section.locator('.diff-container.split.virtualized')).toBeVisible();

    const additionSides = section.locator('.diff-split-side.addition');
    await expect(additionSides.first()).toBeVisible();
    await expect(additionSides.nth(2)).toBeVisible();

    const firstBtn = additionSides.nth(0).locator('.diff-comment-btn');
    const thirdBtn = additionSides.nth(2).locator('.diff-comment-btn');
    await additionSides.nth(0).hover();
    await expect(firstBtn).toBeAttached();

    await dragBetween(page, firstBtn, thirdBtn);

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();
    await expect(page.locator('.comment-form-header')).toContainText(/Line/);
  });

  test('file-list virtualizer module is loaded for large-review path', async ({ page }) => {
    // Small git fixture (<40 files) stays on the classic deferred-body path;
    // assert the module loaded and the controller API is present for large reviews.
    await loadPage(page);
    const api = await page.evaluate(() => {
      const FL = (window as unknown as {
        crit?: { fileListVirtualizer?: { FileListVirtualizer: unknown; FILE_HEADER_ESTIMATE: number } };
      }).crit?.fileListVirtualizer;
      return FL
        ? { hasCtor: typeof FL.FileListVirtualizer === 'function', header: FL.FILE_HEADER_ESTIMATE }
        : null;
    });
    expect(api).toBeTruthy();
    expect(api!.hasCtor).toBe(true);
    expect(api!.header).toBeGreaterThan(0);
  });
});

import { test, expect, Page, Locator } from '@playwright/test';
import { addComment, clearAllComments, getMdPath, loadPage } from './helpers';

// ============================================================
// Sidebar Resize — drag handles + persistence (git mode)
// ============================================================
test.describe('Sidebar resize', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    // Reset persisted widths between tests. Cookies hold the settings;
    // localStorage is cleared too in case future migrations move keys there.
    await page.context().clearCookies();
    await page.addInitScript(() => localStorage.clear());
  });

  // Drag a handle horizontally by `dx` pixels using its current center.
  async function dragHandle(page: Page, handle: Locator, dx: number) {
    await handle.scrollIntoViewIfNeeded();
    const box = (await handle.boundingBox())!;
    const x = box.x + box.width / 2;
    const y = box.y + box.height / 2;
    await page.mouse.move(x, y);
    await page.mouse.down();
    await page.mouse.move(x + dx, y, { steps: 10 });
    await page.mouse.up();
  }

  async function widthOf(panel: Locator): Promise<number> {
    return (await panel.boundingBox())!.width;
  }

  // Make the comments panel visible without going through the UI — sidebar
  // tests shouldn't depend on the comment-icon click path.
  async function showCommentsPanel(page: Page) {
    await page.evaluate(() => {
      document.getElementById('commentsPanel')?.classList.remove('comments-panel-hidden');
    });
    await expect(page.locator('#commentsPanel')).toBeVisible();
  }

  test('file tree default width is 280px when no setting saved', async ({ page }) => {
    await loadPage(page);
    await expect.poll(() => widthOf(page.locator('#fileTreePanel'))).toBe(280);
  });

  test('drag file-tree handle resizes panel and persists across reload', async ({ page }) => {
    await loadPage(page);
    const panel = page.locator('#fileTreePanel');
    const handle = page.locator('#fileTreeResizer');
    const before = await widthOf(panel);

    await dragHandle(page, handle, 60);
    await expect.poll(() => widthOf(panel)).toBe(before + 60);

    const afterDrag = await widthOf(panel);
    await page.reload();
    await expect.poll(() => widthOf(panel)).toBe(afterDrag);
  });

  test('file-tree handle clamps to min 180px and grows freely beyond defaults', async ({ page }) => {
    await loadPage(page);
    const panel = page.locator('#fileTreePanel');
    const handle = page.locator('#fileTreeResizer');

    // Drag far left past viewport edge -> clamp at 180.
    await dragHandle(page, handle, -5000);
    await expect.poll(() => widthOf(panel)).toBe(180);

    // From 180, drag right by 30 — verify the clamp didn't "stick" the handle.
    await dragHandle(page, handle, 30);
    await expect.poll(() => widthOf(panel)).toBe(210);

    // No upper bound: dragging far right grows past the old 600px cap.
    await dragHandle(page, handle, 600);
    await expect.poll(() => widthOf(panel)).toBe(810);
  });

  test('comments-panel resizer is hidden until panel is open, hidden again when closed', async ({ page }) => {
    await loadPage(page);
    const handle = page.locator('#commentsPanelResizer');
    await expect(handle).toBeHidden();

    await showCommentsPanel(page);
    await expect(handle).toBeVisible();

    await page.evaluate(() => {
      document.getElementById('commentsPanel')?.classList.add('comments-panel-hidden');
    });
    await expect(handle).toBeHidden();
  });

  test('drag comments-panel handle (left edge) resizes panel and persists', async ({ page }) => {
    await loadPage(page);
    await showCommentsPanel(page);
    const panel = page.locator('#commentsPanel');
    const handle = page.locator('#commentsPanelResizer');
    const before = await widthOf(panel);

    // Dragging the LEFT-edge handle leftward grows the panel.
    await dragHandle(page, handle, -80);
    await expect.poll(() => widthOf(panel)).toBe(before + 80);

    const after = await widthOf(panel);
    await page.reload();
    await showCommentsPanel(page);
    await expect.poll(() => widthOf(panel)).toBe(after);
  });

  test('both sidebar widths persist independently across reload', async ({ page }) => {
    await loadPage(page);
    await showCommentsPanel(page);
    const fileTree = page.locator('#fileTreePanel');
    const comments = page.locator('#commentsPanel');

    await dragHandle(page, page.locator('#fileTreeResizer'), 40);
    await dragHandle(page, page.locator('#commentsPanelResizer'), -50);

    const ftAfter = await widthOf(fileTree);
    const cAfter = await widthOf(comments);

    await page.reload();
    await showCommentsPanel(page);
    await expect.poll(() => widthOf(fileTree)).toBe(ftAfter);
    await expect.poll(() => widthOf(comments)).toBe(cAfter);
  });

  // Sanity: the user-facing path (open panel by clicking comment icon) still
  // shows the resizer. Kept minimal — sidebar mechanics live in the tests above.
  test('clicking comment icon opens panel and reveals its resizer', async ({ page, request }) => {
    const path = await getMdPath(request);
    await addComment(request, path, 1, 'hello');
    await loadPage(page);
    await page.locator('.comment-count-icon').first().click();
    await expect(page.locator('#commentsPanel')).toBeVisible();
    await expect(page.locator('#commentsPanelResizer')).toBeVisible();
  });
});

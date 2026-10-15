import { test, expect, Page, Locator } from '@playwright/test';
import { addComment, clearAllComments, getMdPath, loadPage } from './helpers';

// ============================================================
// Sidebar Resize — drag handles + persistence (git mode)
// ============================================================
// Widths persist in the crit-settings cookie; every test starts from a fresh
// browser context, so no width is saved yet.
test.describe('Sidebar resize', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
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

  test('file tree defaults to 280px, clamps at 180px and grows freely', async ({ page }) => {
    await loadPage(page);
    const panel = page.locator('#fileTreePanel');
    const handle = page.locator('#fileTreeResizer');
    await expect.poll(() => widthOf(panel)).toBe(280);

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

  test('dragging both handles resizes each panel, and both widths persist independently across reload', async ({ page }) => {
    await loadPage(page);
    await showCommentsPanel(page);
    const fileTree = page.locator('#fileTreePanel');
    const comments = page.locator('#commentsPanel');
    const ftBefore = await widthOf(fileTree);
    const cBefore = await widthOf(comments);

    await dragHandle(page, page.locator('#fileTreeResizer'), 40);
    await expect.poll(() => widthOf(fileTree)).toBe(ftBefore + 40);
    // Dragging the comments panel's LEFT-edge handle leftward grows it.
    await dragHandle(page, page.locator('#commentsPanelResizer'), -50);
    await expect.poll(() => widthOf(comments)).toBe(cBefore + 50);

    await page.reload();
    await showCommentsPanel(page);
    await expect.poll(() => widthOf(fileTree)).toBe(ftBefore + 40);
    await expect.poll(() => widthOf(comments)).toBe(cBefore + 50);
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

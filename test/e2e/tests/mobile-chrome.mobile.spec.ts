import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// F1: mobile chrome layout — at ≤768px viewport, the file-tree and comments
// sidebars are hidden and a sticky <select> file picker takes the file-tree's
// place so multi-file reviews stay navigable on phones.
test.describe('Mobile chrome layout (F1)', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('file-tree sidebar is hidden at mobile viewport', async ({ page }) => {
    const fileTree = page.locator('#fileTreePanel');
    await expect(fileTree).toBeHidden();
  });

  test('comments-panel sidebar is hidden at mobile viewport', async ({ page }) => {
    const commentsPanel = page.locator('#commentsPanel');
    await expect(commentsPanel).toBeHidden();
  });

  test('secondary header controls are hidden at mobile viewport', async ({ page }) => {
    // app.js init runs style.display = '' on these unconditionally; the
    // mobile breakpoint uses !important to win.
    await expect(page.locator('#branchContext')).toBeHidden();
    await expect(page.locator('#diffModeToggle')).toBeHidden();
    await expect(page.locator('.scope-toggle')).toBeHidden();
  });

  test('mobile file picker is visible when sidebar is hidden', async ({ page }) => {
    const pickerBar = page.locator('#mobileFilePickerBar');
    await expect(pickerBar).toBeVisible();

    const picker = page.locator('#mobileFilePicker');
    await expect(picker).toBeVisible();
  });

  test('mobile file picker lists all session files', async ({ page }) => {
    const picker = page.locator('#mobileFilePicker');
    await expect(picker).toBeVisible();

    // git-mode fixture has multiple files. Wait until the picker has been
    // populated (renderFileTree runs after the session loads).
    await expect(async () => {
      expect(await picker.locator('option').count()).toBeGreaterThanOrEqual(2);
    }).toPass();
  });

  test('selecting a file scrolls that file section into view', async ({ page }) => {
    const picker = page.locator('#mobileFilePicker');
    await expect(picker).toBeVisible();

    // Need at least 2 options to test a meaningful change.
    await expect(async () => {
      expect(await picker.locator('option').count()).toBeGreaterThanOrEqual(2);
    }).toPass();

    const options = await picker.locator('option').allTextContents();
    expect(options.length).toBeGreaterThanOrEqual(2);

    // Pick the LAST file in the list so it's most likely to be below the fold.
    const targetPath = options[options.length - 1];
    await picker.selectOption(targetPath);

    // The file's section should be visible after the scroll. Use an attribute
    // selector instead of an ID selector so we don't need CSS.escape (which
    // isn't available in the Node test runtime).
    const targetSection = page.locator(`[id="file-section-${targetPath}"]`);
    await expect(targetSection).toBeInViewport();
  });
});

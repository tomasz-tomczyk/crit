import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection } from './helpers';

test.describe('File Picker Autocomplete — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    // Wait for the file list API to complete during page load
    const filesListPromise = page.waitForResponse(resp =>
      resp.url().includes('/api/files/list') && resp.status() === 200
    );
    await loadPage(page);
    await filesListPromise;
    // In file mode, plan.md is already in document view — no toggle needed
    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
  });

  /** Helper: open a comment form on the first markdown line block and return the textarea. */
  async function openCommentForm(page: import('@playwright/test').Page) {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    const gutterBtn = section.locator('.line-comment-gutter').first();
    await expect(gutterBtn).toBeVisible();
    await gutterBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toBeFocused();
    return textarea;
  }

  test('file picker works in file mode', async ({ page }) => {
    const textarea = await openCommentForm(page);

    await textarea.pressSequentially('@');

    const dropdown = page.locator('.file-picker-dropdown');
    await expect(dropdown).toBeVisible();
  });

  test('file picker shows files', async ({ page }) => {
    const textarea = await openCommentForm(page);

    await textarea.pressSequentially('@');

    const dropdown = page.locator('.file-picker-dropdown');
    await expect(dropdown).toBeVisible();

    await expect(dropdown.locator('.file-picker-item').first()).toBeVisible();
  });
});

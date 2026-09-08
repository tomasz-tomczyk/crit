import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection } from './helpers';

test.describe('File Picker Autocomplete — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
  });

  test('file picker opens and shows files in file mode', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    const gutterBtn = section.locator('.line-comment-gutter').first();
    await expect(gutterBtn).toBeVisible();
    await gutterBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await textarea.pressSequentially('@');

    const dropdown = page.locator('.file-picker-dropdown');
    await expect(dropdown).toBeVisible();
    await expect(dropdown.locator('.file-picker-item').first()).toBeVisible();
  });
});

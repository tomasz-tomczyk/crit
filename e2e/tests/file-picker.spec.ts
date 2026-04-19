import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, switchToDocumentView } from './helpers';

test.describe('File Picker Autocomplete — Git Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    // Wait for the file list API to complete during page load
    const filesListPromise = page.waitForResponse(resp =>
      resp.url().includes('/api/files/list') && resp.status() === 200
    );
    await loadPage(page);
    await filesListPromise;
    await switchToDocumentView(page);
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

  test('typing @ shows file picker dropdown', async ({ page }) => {
    const textarea = await openCommentForm(page);

    await textarea.pressSequentially('@');

    const dropdown = page.locator('.file-picker-dropdown');
    await expect(dropdown).toBeVisible();

    await expect(dropdown.locator('.file-picker-item').first()).toBeVisible();
  });

  test('file picker filters results as you type', async ({ page }) => {
    const textarea = await openCommentForm(page);

    // Wait for the final filtered API response after typing the full query.
    // pressSequentially fires a fetch per keystroke; without waiting for the
    // last one (q=server), the dropdown may still show intermediate results
    // (e.g. q=s) — causing a race condition that flakes in CI.
    const filteredResponse = page.waitForResponse(resp =>
      resp.url().includes('/api/files/list') &&
      resp.url().includes('q=server') &&
      resp.status() === 200
    );
    await textarea.pressSequentially('@server');
    await filteredResponse;

    const dropdown = page.locator('.file-picker-dropdown');
    await expect(dropdown).toBeVisible();

    // All visible items should contain "server" (case-insensitive)
    const items = dropdown.locator('.file-picker-item');
    await expect(items.first()).toBeVisible();
    const count = await items.count();
    for (let i = 0; i < count; i++) {
      const text = await items.nth(i).textContent();
      expect(text!.toLowerCase()).toContain('server');
    }
  });

  test('selecting a file inserts path into textarea', async ({ page }) => {
    const textarea = await openCommentForm(page);

    await textarea.pressSequentially('@');

    const dropdown = page.locator('.file-picker-dropdown');
    await expect(dropdown).toBeVisible();

    // Get the first item's path before clicking
    const firstItem = dropdown.locator('.file-picker-item').first();
    const filePath = await firstItem.getAttribute('data-path');
    expect(filePath).toBeTruthy();

    await firstItem.click();

    // Dropdown should close
    await expect(dropdown).toBeHidden();

    // Textarea should contain @filepath
    await expect(textarea).toHaveValue(new RegExp(`@${filePath!.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`));
  });

  test('keyboard navigation with ArrowDown and Enter selects item', async ({ page }) => {
    const textarea = await openCommentForm(page);

    await textarea.pressSequentially('@');

    const dropdown = page.locator('.file-picker-dropdown');
    await expect(dropdown).toBeVisible();

    // First item should be active by default
    const items = dropdown.locator('.file-picker-item');
    await expect(items.first()).toHaveClass(/active/);

    // ArrowDown should move to second item
    await textarea.press('ArrowDown');
    await expect(items.nth(1)).toHaveClass(/active/);
    await expect(items.first()).not.toHaveClass(/active/);

    // Get the path of the now-active item
    const activePath = await items.nth(1).getAttribute('data-path');
    expect(activePath).toBeTruthy();

    // Enter should select it
    await textarea.press('Enter');

    // Dropdown should close
    await expect(dropdown).toBeHidden();

    // Textarea should contain the selected path
    await expect(textarea).toHaveValue(new RegExp(`@${activePath!.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`));
  });

  test('Escape closes file picker without inserting', async ({ page }) => {
    const textarea = await openCommentForm(page);

    await textarea.pressSequentially('@');

    const dropdown = page.locator('.file-picker-dropdown');
    await expect(dropdown).toBeVisible();

    await textarea.press('Escape');

    // Dropdown should close
    await expect(dropdown).toBeHidden();

    // Textarea should still have just the @ character
    await expect(textarea).toHaveValue('@');
  });

  test('file picker does not trigger mid-word', async ({ page }) => {
    const textarea = await openCommentForm(page);

    // Type a word then @ — the @ is mid-word so no dropdown
    await textarea.pressSequentially('email@');

    const dropdown = page.locator('.file-picker-dropdown');
    // Give a moment for any potential dropdown to appear, then assert it's not there
    await expect(dropdown).toBeHidden();
  });
});

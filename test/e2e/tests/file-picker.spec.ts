import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, switchToDocumentView, openDocLineComment } from './helpers';

function escapeRegExp(s: string) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

test.describe('File Picker Autocomplete — Git Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('@ opens the picker; it filters, selects by keyboard or click, closes on Escape, and ignores mid-word @', async ({ page }) => {
    // Wait for the file list API to complete during page load
    const filesListPromise = page.waitForResponse(resp =>
      resp.url().includes('/api/files/list') && resp.status() === 200
    );
    await loadPage(page);
    await filesListPromise;
    const doc = await switchToDocumentView(page);
    const textarea = await openDocLineComment(page, doc);
    await expect(textarea).toBeFocused();

    const dropdown = page.locator('.file-picker-dropdown');
    const items = dropdown.locator('.file-picker-item');

    // Typing @ shows the dropdown, first item active.
    await textarea.pressSequentially('@');
    await expect(dropdown).toBeVisible();
    await expect(items.first()).toBeVisible();
    await expect(items.first()).toHaveClass(/active/);

    // ArrowDown moves to the second item; Enter selects it.
    await textarea.press('ArrowDown');
    await expect(items.nth(1)).toHaveClass(/active/);
    await expect(items.first()).not.toHaveClass(/active/);
    const activePath = await items.nth(1).getAttribute('data-path');
    expect(activePath).toBeTruthy();
    await textarea.press('Enter');
    await expect(dropdown).toBeHidden();
    await expect(textarea).toHaveValue(new RegExp(`@${escapeRegExp(activePath!)}`));

    // Clicking an item inserts its path.
    await textarea.fill('');
    await textarea.pressSequentially('@');
    await expect(dropdown).toBeVisible();
    const firstItem = items.first();
    const filePath = await firstItem.getAttribute('data-path');
    expect(filePath).toBeTruthy();
    await firstItem.click();
    await expect(dropdown).toBeHidden();
    await expect(textarea).toHaveValue(new RegExp(`@${escapeRegExp(filePath!)}`));

    // Results filter as you type. pressSequentially fires a fetch per
    // keystroke; wait for the last one (q=server), or the dropdown may still
    // show intermediate results.
    await textarea.fill('');
    const filteredResponse = page.waitForResponse(resp =>
      resp.url().includes('/api/files/list') && resp.url().includes('q=server') && resp.status() === 200
    );
    await textarea.pressSequentially('@server');
    await filteredResponse;
    await expect(dropdown).toBeVisible();
    await expect(items.first()).toBeVisible();
    await expect(async () => {
      const texts = await items.allTextContents();
      expect(texts.length).toBeGreaterThan(0);
      for (const text of texts) expect(text.toLowerCase()).toContain('server');
    }).toPass();

    // Escape closes the picker without inserting anything.
    await textarea.press('Escape');
    await expect(dropdown).toBeHidden();
    await expect(textarea).toHaveValue('@server');

    // A mid-word @ does not trigger the picker: no lookup is made for it.
    // The following word-start @ does, which proves the input was handled.
    await textarea.fill('');
    const lookups: string[] = [];
    page.on('request', req => {
      if (req.url().includes('/api/files/list')) lookups.push(new URL(req.url()).search);
    });
    await textarea.pressSequentially('email@');
    await textarea.pressSequentially(' @');
    await expect(dropdown).toBeVisible();
    expect(lookups).toEqual(['?q=']);
  });
});

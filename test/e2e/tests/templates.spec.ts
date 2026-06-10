import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, switchToDocumentView } from './helpers';

// Helper: open comment form on the first markdown line block
async function openCommentForm(page: import('@playwright/test').Page) {
  const section = mdSection(page);
  const lineBlock = section.locator('.line-block').first();
  await lineBlock.hover();
  const gutterBtn = section.locator('.line-comment-gutter').first();
  await expect(gutterBtn).toBeVisible();
  await gutterBtn.click();
  await expect(page.locator('.comment-form')).toBeVisible();
}

// Helper: clear template cookie
async function clearTemplates(page: import('@playwright/test').Page) {
  await page.evaluate(() => {
    document.cookie = 'crit-templates=; path=/; max-age=0';
  });
}

test.describe('Comment Templates — Git Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await clearTemplates(page);
    await switchToDocumentView(page);
  });

  test('no template bar visible on fresh start', async ({ page }) => {
    await openCommentForm(page);
    const bar = page.locator('.comment-template-bar');
    await expect(bar).toBeHidden();
  });

  test('Save as template button visible in actions row', async ({ page }) => {
    await openCommentForm(page);
    const saveBtn = page.locator('.comment-form-actions button', { hasText: '+ Template' });
    await expect(saveBtn).toBeVisible();
  });

  test('Save as template opens dialog with textarea content', async ({ page }) => {
    await openCommentForm(page);
    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Consider using X instead');

    const saveBtn = page.locator('.comment-form-actions button', { hasText: '+ Template' });
    await saveBtn.click();

    const overlay = page.locator('.save-template-overlay');
    await expect(overlay).toBeVisible();
    const input = overlay.locator('.save-template-input');
    await expect(input).toHaveValue('Consider using X instead');
  });

  test('saving template makes chip appear in template bar', async ({ page }) => {
    await openCommentForm(page);
    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Needs a test for this');

    const saveBtn = page.locator('.comment-form-actions button', { hasText: '+ Template' });
    await saveBtn.click();

    const overlay = page.locator('.save-template-overlay');
    await overlay.locator('button', { hasText: 'Save' }).click();

    await expect(overlay).toBeHidden();

    const bar = page.locator('.comment-template-bar');
    await expect(bar).toBeVisible();
    const chip = bar.locator('.template-chip');
    await expect(chip).toHaveCount(1);
    await expect(chip.locator('.template-chip-label')).toHaveText('Needs a test for this');
  });

  test('clicking chip inserts text into textarea', async ({ page }) => {
    await openCommentForm(page);
    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('My template text');

    // Save a template first
    const saveBtn = page.locator('.comment-form-actions button', { hasText: '+ Template' });
    await saveBtn.click();
    await page.locator('.save-template-overlay button', { hasText: 'Save' }).click();

    // Clear textarea
    await textarea.fill('');

    // Click the chip
    const chip = page.locator('.template-chip').first();
    await chip.click();

    await expect(textarea).toHaveValue('My template text');
  });

  test('deleting chip via × removes it and hides bar when empty', async ({ page }) => {
    await openCommentForm(page);
    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Temp template');

    // Save a template
    const saveBtn = page.locator('.comment-form-actions button', { hasText: '+ Template' });
    await saveBtn.click();
    await page.locator('.save-template-overlay button', { hasText: 'Save' }).click();

    const bar = page.locator('.comment-template-bar');
    await expect(bar).toBeVisible();

    // Click × to delete chip
    const chip = bar.locator('.template-chip').first();
    const del = chip.locator('.template-chip-delete');
    await expect(del).toBeVisible();
    await del.click();

    await expect(bar).toBeHidden();
  });

  test('templates persist across form close and reopen', async ({ page }) => {
    await openCommentForm(page);
    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Persistent template');

    // Save template
    const saveBtn = page.locator('.comment-form-actions button', { hasText: '+ Template' });
    await saveBtn.click();
    await page.locator('.save-template-overlay button', { hasText: 'Save' }).click();

    // Cancel form
    await page.locator('.comment-form-actions button', { hasText: 'Cancel' }).click();
    await expect(page.locator('.comment-form')).toBeHidden();

    // Reopen form
    await openCommentForm(page);

    // Template bar should still have the chip
    const bar = page.locator('.comment-template-bar');
    await expect(bar).toBeVisible();
    await expect(bar.locator('.template-chip')).toHaveCount(1);
    await expect(bar.locator('.template-chip-label').first()).toHaveText('Persistent template');
  });

  test('save dialog does nothing when textarea is empty', async ({ page }) => {
    await openCommentForm(page);
    // textarea is empty by default
    const saveBtn = page.locator('.comment-form-actions button', { hasText: '+ Template' });
    await saveBtn.click();

    // Dialog should not appear
    const overlay = page.locator('.save-template-overlay');
    await expect(overlay).toBeHidden();
  });

  test('save dialog can be cancelled', async ({ page }) => {
    await openCommentForm(page);
    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Cancel me');

    const saveBtn = page.locator('.comment-form-actions button', { hasText: '+ Template' });
    await saveBtn.click();

    const overlay = page.locator('.save-template-overlay');
    await expect(overlay).toBeVisible();

    await overlay.locator('button', { hasText: 'Cancel' }).click();
    await expect(overlay).toBeHidden();

    // No template bar should appear
    const bar = page.locator('.comment-template-bar');
    await expect(bar).toBeHidden();
  });
});

import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection } from './helpers';

// Helper: open comment form on the first markdown line block
async function openCommentForm(page: any) {
  const section = mdSection(page);
  const lineBlock = section.locator('.line-block').first();
  await lineBlock.hover();
  const gutterBtn = section.locator('.line-comment-gutter').first();
  await expect(gutterBtn).toBeVisible();
  await gutterBtn.click();
  await expect(page.locator('.comment-form')).toBeVisible();
}

// Helper: clear template cookie
async function clearTemplates(page: any) {
  await page.evaluate(() => {
    document.cookie = 'crit-templates=; path=/; max-age=0';
  });
}

test.describe('Comment Templates — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await clearTemplates(page);
    // In file mode, plan.md is already in document view
    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
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

  test('full save-insert-delete cycle', async ({ page }) => {
    await openCommentForm(page);
    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('This duplicates logic in …');

    // Save as template
    const saveBtn = page.locator('.comment-form-actions button', { hasText: '+ Template' });
    await saveBtn.click();
    const overlay = page.locator('.save-template-overlay');
    await expect(overlay).toBeVisible();
    await overlay.locator('button', { hasText: 'Save' }).click();
    await expect(overlay).toBeHidden();

    // Chip appears
    const bar = page.locator('.comment-template-bar');
    await expect(bar).toBeVisible();
    const chip = bar.locator('.template-chip');
    await expect(chip).toHaveCount(1);

    // Clear textarea and insert via chip
    await textarea.fill('');
    await chip.click();
    await expect(textarea).toHaveValue('This duplicates logic in …');

    // Click × to delete chip
    const del = chip.locator('.template-chip-delete');
    await expect(del).toBeVisible();
    await del.click();
    await expect(bar).toBeHidden();
  });

  test('multiple templates render as separate chips', async ({ page }) => {
    await openCommentForm(page);
    const textarea = page.locator('.comment-form textarea');

    // Save first template
    await textarea.fill('Template A');
    await page.locator('.comment-form-actions button', { hasText: '+ Template' }).click();
    await page.locator('.save-template-overlay button', { hasText: 'Save' }).click();

    // Save second template
    await textarea.fill('Template B');
    await page.locator('.comment-form-actions button', { hasText: '+ Template' }).click();
    await page.locator('.save-template-overlay button', { hasText: 'Save' }).click();

    const chips = page.locator('.comment-template-bar .template-chip');
    await expect(chips).toHaveCount(2);
    await expect(chips.nth(0).locator('.template-chip-label')).toHaveText('Template A');
    await expect(chips.nth(1).locator('.template-chip-label')).toHaveText('Template B');
  });

  test('templates persist after page reload', async ({ page }) => {
    await openCommentForm(page);
    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Survives reload');

    await page.locator('.comment-form-actions button', { hasText: '+ Template' }).click();
    await page.locator('.save-template-overlay button', { hasText: 'Save' }).click();

    // Reload page
    await loadPage(page);
    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Reopen form
    await openCommentForm(page);
    const bar = page.locator('.comment-template-bar');
    await expect(bar).toBeVisible();
    await expect(bar.locator('.template-chip-label').first()).toHaveText('Survives reload');
  });
});

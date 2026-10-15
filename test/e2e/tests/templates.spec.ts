import { test, expect, type Page } from '@playwright/test';
import { clearAllComments, loadPage, switchToDocumentView, openDocLineComment } from './helpers';

// Templates live in the crit-templates cookie; each test gets a fresh
// browser context, so none exist when a test starts.

// Open a comment form on the first markdown line block.
async function openCommentForm(page: Page) {
  await openDocLineComment(page, await switchToDocumentView(page));
}

const saveBtn = (page: Page) => page.locator('.comment-form-actions button', { hasText: '+ Template' });
const bar = (page: Page) => page.locator('.comment-template-bar');
const overlay = (page: Page) => page.locator('.save-template-overlay');

test.describe('Comment Templates — Git Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('save a template, insert it from its chip, keep it across forms, and delete it', async ({ page }) => {
    await openCommentForm(page);
    // No template bar on a fresh start; "+ Template" sits in the actions row.
    await expect(bar(page)).toBeHidden();
    await expect(saveBtn(page)).toBeVisible();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Needs a test for this');
    await saveBtn(page).click();

    // The save dialog opens with the textarea content.
    await expect(overlay(page)).toBeVisible();
    await expect(overlay(page).locator('.save-template-input')).toHaveValue('Needs a test for this');
    await overlay(page).locator('button', { hasText: 'Save' }).click();
    await expect(overlay(page)).toBeHidden();

    // The chip appears in the template bar.
    await expect(bar(page)).toBeVisible();
    const chip = bar(page).locator('.template-chip');
    await expect(chip).toHaveCount(1);
    await expect(chip.locator('.template-chip-label')).toHaveText('Needs a test for this');

    // Clicking the chip inserts its text.
    await textarea.fill('');
    await chip.click();
    await expect(textarea).toHaveValue('Needs a test for this');

    // Templates persist across form close and reopen.
    await page.locator('.comment-form-actions button', { hasText: 'Cancel' }).click();
    await expect(page.locator('.comment-form')).toBeHidden();
    await openCommentForm(page);
    await expect(bar(page)).toBeVisible();
    await expect(bar(page).locator('.template-chip')).toHaveCount(1);
    await expect(bar(page).locator('.template-chip-label').first()).toHaveText('Needs a test for this');

    // × deletes the chip and hides the empty bar.
    const del = bar(page).locator('.template-chip').first().locator('.template-chip-delete');
    await expect(del).toBeVisible();
    await del.click();
    await expect(bar(page)).toBeHidden();
  });

  test('the save dialog does nothing for an empty form, and can be cancelled', async ({ page }) => {
    await openCommentForm(page);

    // textarea is empty by default: no dialog.
    await saveBtn(page).click();
    await expect(overlay(page)).toBeHidden();

    await page.locator('.comment-form textarea').fill('Cancel me');
    await saveBtn(page).click();
    await expect(overlay(page)).toBeVisible();
    await overlay(page).locator('button', { hasText: 'Cancel' }).click();
    await expect(overlay(page)).toBeHidden();
    // No template saved.
    await expect(bar(page)).toBeHidden();
  });
});

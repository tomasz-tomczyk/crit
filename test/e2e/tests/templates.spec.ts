import { test, expect, type Page } from '@playwright/test';
import { clearAllComments, loadPage, switchToDocumentView, openDocLineComment } from './helpers';

// Templates live in the crit-templates cookie, their title, emoji, shortcut,
// display and pin in crit-template-meta. Each test gets a fresh browser
// context: the first use seeds 👍 Accepted and 👎 Rejected, pinned.

// Open a comment form on the first markdown line block.
async function openCommentForm(page: Page) {
  await openDocLineComment(page, await switchToDocumentView(page));
}

const saveBtn = (page: Page) => page.locator('.comment-form-actions button', { hasText: '+ Template' });
const bar = (page: Page) => page.locator('.comment-form .comment-template-bar');
const chips = (page: Page) => bar(page).locator('.template-chip');
const dialog = (page: Page) => page.locator('.save-template-dialog');
const displayButtons = (page: Page) => dialog(page).locator('.template-display .settings-pill-btn');

async function openSettings(page: Page) {
  await page.locator('#settingsToggle').click();
  await expect(page.locator('#settingsTemplates .settings-template-row').first()).toBeVisible();
}

test.describe('Comment Templates — Git Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('a fresh start offers 👍 Accepted and 👎 Rejected, pinned first, with an edit button and no ×', async ({ page }) => {
    await openCommentForm(page);
    await expect(chips(page)).toHaveCount(2);
    await expect(chips(page).nth(0)).toHaveText('👍Accepted');
    await expect(chips(page).nth(1)).toHaveText('👎Rejected');
    await expect(chips(page).nth(0)).toHaveClass(/template-chip--pinned/);
    await expect(chips(page).locator('.template-chip-edit')).toHaveCount(2);
    await expect(chips(page).locator('.template-chip-delete')).toHaveCount(0);
  });

  test('save a template with a title, emoji, display and pin; its chip inserts the text', async ({ page }) => {
    await openCommentForm(page);
    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Please add a unit test for this branch.');
    await saveBtn(page).click();

    await expect(dialog(page).locator('h3')).toHaveText('Save as template');
    await expect(dialog(page).locator('.save-template-input')).toHaveValue('Please add a unit test for this branch.');
    await expect(dialog(page).locator('.save-template-emoji')).not.toHaveAttribute('placeholder', /.+/);
    // The toolbar button choice only shows under a checked pin.
    await expect(dialog(page).locator('.template-display')).toBeHidden();
    await dialog(page).getByLabel('Pin to the selection toolbar').check();
    await expect(dialog(page).locator('.template-display')).toBeVisible();
    // Without an emoji, only Text can be chosen.
    await expect(displayButtons(page).nth(0)).toBeDisabled();
    await expect(displayButtons(page).nth(1)).toHaveClass(/active/);
    await expect(displayButtons(page).nth(2)).toBeDisabled();

    await dialog(page).locator('.save-template-title').fill('Ask for tests');
    await dialog(page).locator('.save-template-emoji').fill('🧪');
    await expect(displayButtons(page).nth(0)).toHaveClass(/active/);
    await dialog(page).locator('[data-display="both"]').click();
    await dialog(page).locator('button', { hasText: 'Save' }).click();
    await expect(dialog(page)).toBeHidden();

    // Pinned, so it joins the pinned chips, by its title.
    await expect(chips(page)).toHaveCount(3);
    await expect(chips(page).nth(2)).toHaveText('🧪Ask for tests');
    await textarea.fill('');
    await chips(page).nth(2).click();
    await expect(textarea).toHaveValue('Please add a unit test for this branch.');

    const meta = await page.evaluate(() => window.crit.commentTemplates.getTemplateItems()
      .find((i: { text: string }) => i.text === 'Please add a unit test for this branch.'));
    expect(meta).toMatchObject({ title: 'Ask for tests', emoji: '🧪', display: 'both', pinned: true });
  });

  test('the edit button reopens the dialog filled in, and saves on the same template', async ({ page }) => {
    await openCommentForm(page);
    await chips(page).nth(1).locator('.template-chip-edit').click();
    await expect(dialog(page).locator('h3')).toHaveText('Edit template');
    await expect(dialog(page).locator('.save-template-input')).toHaveValue('Rejected');
    await expect(dialog(page).locator('.save-template-emoji')).toHaveValue('👎');
    await expect(dialog(page).getByLabel('Pin to the selection toolbar')).toBeChecked();

    await dialog(page).locator('.save-template-title').fill('No');
    await dialog(page).getByLabel('Pin to the selection toolbar').uncheck();
    await dialog(page).locator('button', { hasText: 'Save' }).click();

    // Unpinned: it moves after the pinned chips, under its title.
    await expect(chips(page)).toHaveCount(2);
    await expect(chips(page).nth(1)).toHaveText('👎No');
    await expect(chips(page).nth(1)).not.toHaveClass(/template-chip--pinned/);
  });

  test('delete only after the confirmation; Keep and a reflex Enter keep the template', async ({ page }) => {
    await openCommentForm(page);
    await chips(page).nth(0).locator('.template-chip-edit').click();
    await dialog(page).locator('.save-template-delete').click();
    await expect(dialog(page).locator('.save-template-delete-question')).toHaveText('Delete this template?');
    await expect(dialog(page).locator('button', { hasText: 'Keep' })).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(dialog(page).locator('.save-template-delete')).toBeVisible();
    await expect(chips(page)).toHaveCount(2);

    await dialog(page).locator('.save-template-delete').click();
    await dialog(page).locator('.btn-danger', { hasText: 'Delete' }).click();
    await expect(dialog(page)).toBeHidden();
    await expect(chips(page)).toHaveCount(1);
    await expect(chips(page).first()).toHaveText('👎Rejected');

    // A deleted default does not come back.
    await page.reload();
    await openCommentForm(page);
    await expect(chips(page)).toHaveCount(1);
  });

  test('the save dialog does nothing for an empty form, and can be cancelled', async ({ page }) => {
    await openCommentForm(page);
    await saveBtn(page).click();
    await expect(dialog(page)).toHaveCount(0);

    await page.locator('.comment-form textarea').fill('Cancel me');
    await saveBtn(page).click();
    await expect(dialog(page)).toBeVisible();
    await dialog(page).locator('button', { hasText: 'Cancel' }).click();
    await expect(dialog(page)).toHaveCount(0);
    await expect(chips(page)).toHaveCount(2);
  });

  test('a shortcut already taken by Crit is refused, a free one is kept', async ({ page }) => {
    await openCommentForm(page);
    await chips(page).nth(0).locator('.template-chip-edit').click();
    const shortcut = dialog(page).locator('.template-shortcut');
    await shortcut.click();
    await page.keyboard.press('c');
    await expect(page.locator('.mini-toast', { hasText: 'c already does' })).toBeVisible();
    await expect(shortcut).toHaveText('No shortcut');
    await shortcut.click();
    await page.keyboard.press('y');
    await expect(shortcut).toHaveText('y');
    await dialog(page).locator('button', { hasText: 'Save' }).click();
    const accepted = await page.evaluate(() => window.crit.commentTemplates.getTemplateItems()[0]);
    expect(accepted.shortcut).toBe('y');
  });

  test('Settings list templates without ×, edits and creates them in the dialog, and reorders by drag', async ({ page }) => {
    await openCommentForm(page);
    await page.locator('.comment-form textarea').evaluate((el: HTMLTextAreaElement) => el.blur());
    await openSettings(page);
    const rows = page.locator('#settingsTemplates .settings-template-row');
    await expect(rows).toHaveCount(2);
    await expect(rows.locator('button')).toHaveCount(2); // one edit button per row
    await expect(rows.nth(0).locator('.settings-template-pinned')).toHaveText('Pinned');

    await page.locator('.settings-template-new').click();
    await dialog(page).locator('.save-template-input').fill('Out of scope for this change.');
    await dialog(page).locator('.save-template-title').fill('Out of scope');
    await page.keyboard.press('Control+Enter');
    await expect(rows).toHaveCount(3);
    await expect(rows.nth(2).locator('.settings-template-name')).toHaveText('Out of scope');
    await expect(rows.nth(2).locator('.settings-template-body')).toHaveText('Out of scope for this change.');

    // Drag Rejected above Accepted; Up/Down on a handle moves a row too.
    await rows.nth(1).locator('.settings-template-handle').dragTo(rows.nth(0), { targetPosition: { x: 40, y: 3 } });
    await expect(rows.locator('.settings-template-name')).toHaveText(['Rejected', 'Accepted', 'Out of scope']);
    await rows.nth(2).locator('.settings-template-handle').focus();
    await page.keyboard.press('ArrowUp');
    await expect(rows.locator('.settings-template-name')).toHaveText(['Rejected', 'Out of scope', 'Accepted']);

    // The open form follows once Settings close, without a reload.
    await page.locator('#settingsClose').click();
    await expect(chips(page).locator('.template-chip-label')).toHaveText(['Rejected', 'Accepted', 'Out of scope']);
  });
});

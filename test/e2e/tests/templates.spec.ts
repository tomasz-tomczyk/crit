import { test, expect, type Page } from '@playwright/test';
import { clearAllComments, loadPage, switchToDocumentView, openDocLineComment } from './helpers';

// Templates live in the crit-templates cookie, their title, emoji, shortcut,
// display and pin in crit-template-meta. Each test gets a fresh browser
// context: the first use seeds 👍 Accepted (Alt+P) and 👎 Rejected (Alt+C),
// shown in the selection toolbar. They are edited in Settings > Comment
// templates, unfolded in place: there is no template dialog.

// Open a comment form on the first markdown line block.
async function openCommentForm(page: Page) {
  await openDocLineComment(page, await switchToDocumentView(page));
}

const saveBtn = (page: Page) => page.locator('.comment-form-actions button', { hasText: '+ Template' });
const bar = (page: Page) => page.locator('.comment-form .comment-template-bar');
const chips = (page: Page) => bar(page).locator('.template-chip');
const pane = (page: Page) => page.locator('#templatesPane');
const rows = (page: Page) => pane(page).locator('.settings-template-row');
const editor = (page: Page) => pane(page).locator('.settings-template-editor');
const displayButtons = (page: Page) => editor(page).locator('.template-display .settings-pill-btn');
// "Hide from selection toolbar", off by default like "Hide resolved comments".
const hideSwitch = (page: Page) => editor(page).getByLabel('Hide from the selection toolbar');
const hideLine = (page: Page) => editor(page).locator('.settings-template-line', { hasText: 'Hide from selection toolbar' });
// The switch input is visually hidden: click its label.
const hideSwitchLabel = (page: Page) => hideLine(page).locator('label.comments-panel-switch');

async function openTemplatesTab(page: Page) {
  await page.locator('#settingsToggle').click();
  await page.locator('#tab-templates').click();
  await expect(rows(page).first()).toBeVisible();
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

  test('+ Template opens the tab on a new template filled from the form, and the form keeps its draft', async ({ page }) => {
    await openCommentForm(page);
    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Please add a unit test for this branch.');
    await saveBtn(page).click();

    await expect(pane(page)).toBeVisible();
    await expect(editor(page).locator('.settings-template-editor-head')).toHaveText('New template');
    await expect(editor(page).locator('.settings-template-text-input')).toHaveValue('Please add a unit test for this branch.');
    await expect(editor(page).locator('.settings-template-emoji-input')).not.toHaveAttribute('placeholder', /.+/);
    await expect(hideSwitch(page)).not.toBeChecked();
    // Without an emoji, only Text can be chosen.
    await expect(displayButtons(page)).toHaveText(['Icon', 'Text', 'Both']);
    await expect(displayButtons(page).nth(0)).toBeDisabled();
    await expect(displayButtons(page).nth(1)).toHaveClass(/active/);
    await expect(displayButtons(page).nth(2)).toBeDisabled();

    await editor(page).locator('.settings-template-name-input').fill('Ask for tests');
    await editor(page).locator('.settings-template-emoji-input').fill('🧪');
    await expect(displayButtons(page).nth(0)).toHaveClass(/active/);
    await editor(page).locator('[data-display="both"]').click();
    await editor(page).locator('button', { hasText: 'Save' }).click();
    await expect(editor(page)).toHaveCount(0);
    await expect(rows(page)).toHaveCount(3);

    await page.locator('#settingsClose').click();
    await expect(textarea).toHaveValue('Please add a unit test for this branch.');
    // In the toolbar, so it joins the pinned chips, by its name.
    await expect(chips(page)).toHaveCount(3);
    await expect(chips(page).nth(2)).toHaveText('🧪Ask for tests');
    await textarea.fill('');
    await chips(page).nth(2).click();
    await expect(textarea).toHaveValue('Please add a unit test for this branch.');

    const meta = await page.evaluate(() => window.crit.commentTemplates.getTemplateItems()
      .find((i: { text: string }) => i.text === 'Please add a unit test for this branch.'));
    expect(meta).toMatchObject({ title: 'Ask for tests', emoji: '🧪', display: 'both', pinned: true });
  });

  test('a chip\'s edit button opens its template unfolded, and saves on the same template', async ({ page }) => {
    await openCommentForm(page);
    await chips(page).nth(1).locator('.template-chip-edit').click();
    await expect(editor(page)).toHaveCount(1);
    await expect(editor(page).locator('.settings-template-text-input')).toHaveValue('Rejected');
    await expect(editor(page).locator('.settings-template-emoji-input')).toHaveValue('👎');
    await expect(editor(page).locator('.template-shortcut')).toHaveText('Alt+c');
    await expect(hideSwitch(page)).not.toBeChecked();

    await editor(page).locator('.settings-template-name-input').fill('No');
    await hideSwitchLabel(page).click();
    await expect(editor(page).locator('.template-display')).toBeHidden();
    await editor(page).locator('button', { hasText: 'Save' }).click();
    await page.locator('#settingsClose').click();

    // Hidden from the toolbar: it moves after the pinned chips, under its name.
    await expect(chips(page)).toHaveCount(2);
    await expect(chips(page).nth(1)).toHaveText('👎No');
    await expect(chips(page).nth(1)).not.toHaveClass(/template-chip--pinned/);
  });

  test('delete only after the confirmation; Keep and a reflex Enter keep the template', async ({ page }) => {
    await openTemplatesTab(page);
    await rows(page).nth(0).locator('.settings-template-action').click();
    await editor(page).locator('.settings-template-delete').click();
    await expect(editor(page).locator('.settings-template-delete-question')).toHaveText('Delete this template?');
    await expect(editor(page).locator('button', { hasText: 'Keep' })).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(editor(page).locator('.settings-template-delete')).toBeVisible();

    await editor(page).locator('.settings-template-delete').click();
    await editor(page).locator('.btn-danger', { hasText: 'Delete' }).click();
    await expect(editor(page)).toHaveCount(0);
    await expect(rows(page)).toHaveCount(1);
    await expect(rows(page).locator('.settings-template-name')).toHaveText('Rejected');

    // A deleted default does not come back.
    await page.reload();
    await openCommentForm(page);
    await expect(chips(page)).toHaveCount(1);
  });

  test('Esc and Cancel fold the editor without closing Settings or saving', async ({ page }) => {
    await openTemplatesTab(page);
    await rows(page).nth(0).locator('.settings-template-action').click();
    await editor(page).locator('.settings-template-name-input').fill('Changed?');
    await page.keyboard.press('Escape');
    await expect(editor(page)).toHaveCount(0);
    await expect(pane(page)).toBeVisible();
    await expect(rows(page).nth(0).locator('.settings-template-name')).toHaveText('Accepted');

    await pane(page).locator('.settings-template-new').click();
    await expect(pane(page).locator('.settings-template-new')).toBeHidden();
    await editor(page).locator('.settings-template-text-input').fill('Cancel me?');
    // "?" typed in a field stays in the field, not Settings' jump to Shortcuts.
    await expect(pane(page)).toBeVisible();
    await editor(page).locator('button', { hasText: 'Cancel' }).click();
    await expect(rows(page)).toHaveCount(2);
    await expect(pane(page).locator('.settings-template-new')).toBeVisible();
  });

  test('a shortcut already taken by Crit is refused, a free one is kept', async ({ page }) => {
    await openTemplatesTab(page);
    await rows(page).nth(0).locator('.settings-template-action').click();
    const shortcut = editor(page).locator('.template-shortcut');
    await expect(shortcut).toHaveText('Alt+p');
    await shortcut.click();
    await page.keyboard.press('c');
    await expect(page.locator('.mini-toast', { hasText: 'c already does' })).toBeVisible();
    await expect(shortcut).toHaveText('Alt+p');
    await shortcut.click();
    await page.keyboard.press('y');
    await expect(shortcut).toHaveText('y');
    await editor(page).locator('button', { hasText: 'Save' }).click();
    const accepted = await page.evaluate(() => window.crit.commentTemplates.getTemplateItems()[0]);
    expect(accepted.shortcut).toBe('y');
  });

  test('the tab lists templates without switch nor ×, adds one, and reorders by drag; open forms follow', async ({ page }) => {
    await openCommentForm(page);
    await page.locator('.comment-form textarea').evaluate((el: HTMLTextAreaElement) => el.blur());
    await openTemplatesTab(page);
    await expect(rows(page)).toHaveCount(2);
    await expect(rows(page).locator('button')).toHaveCount(2); // one edit button per row
    await expect(rows(page).locator('.comments-panel-switch')).toHaveCount(0);
    await expect(rows(page).nth(0).locator('.template-shortcut')).toHaveText('Alt+p');

    await pane(page).locator('.settings-template-new').click();
    await editor(page).locator('.settings-template-text-input').fill('Out of scope for this change.');
    await editor(page).locator('.settings-template-name-input').fill('Out of scope');
    await page.keyboard.press('Control+Enter');
    await expect(rows(page)).toHaveCount(3);
    await expect(rows(page).nth(2).locator('.settings-template-name')).toHaveText('Out of scope');
    await expect(rows(page).nth(2).locator('.settings-template-body')).toHaveText('Out of scope for this change.');

    // Drag Rejected above Accepted; Up/Down on a handle moves a row too.
    await rows(page).nth(1).locator('.settings-template-handle').dragTo(rows(page).nth(0), { targetPosition: { x: 40, y: 3 } });
    await expect(rows(page).locator('.settings-template-name')).toHaveText(['Rejected', 'Accepted', 'Out of scope']);
    await rows(page).nth(2).locator('.settings-template-handle').focus();
    await page.keyboard.press('ArrowUp');
    await expect(rows(page).locator('.settings-template-name')).toHaveText(['Rejected', 'Out of scope', 'Accepted']);

    // The open form follows without a reload.
    await page.locator('#settingsClose').click();
    await expect(chips(page).locator('.template-chip-label')).toHaveText(['Rejected', 'Out of scope', 'Accepted']);
  });

  test('Hide from selection toolbar is locked while the toolbar hides comment templates', async ({ page }) => {
    await page.evaluate(() => window.crit.shared.setSetting('selectionToolbarTemplates', false));
    await openTemplatesTab(page);
    await rows(page).nth(0).locator('.settings-template-action').click();
    await expect(hideSwitch(page)).toBeDisabled();
    await expect(hideSwitch(page)).not.toBeChecked();
    await expect(hideLine(page)).toHaveAttribute('title', /Hide comment templates/);
    await page.keyboard.press('Escape');

    await page.locator('#tab-settings').click();
    await page.locator('#toolbarAdvanced summary').click();
    await page.locator('label.comments-panel-switch:has(#toolbarTemplatesToggle)').click();
    await page.locator('#tab-templates').click();
    await rows(page).nth(0).locator('.settings-template-action').click();
    await expect(hideSwitch(page)).toBeEnabled();
    await expect(hideLine(page)).not.toHaveAttribute('title', /.+/);
  });

  test('template shortcuts close the Shortcuts tab, change there, and Crit keys cannot take them', async ({ page }) => {
    await page.locator('#settingsToggle').click();
    await page.locator('#tab-shortcuts').click();
    const templateKeys = page.locator('#shortcutsPane [data-template-index]');
    await expect(templateKeys).toHaveCount(2);
    await expect(templateKeys.nth(0)).toHaveText('Alt+p');
    await expect(page.locator('#shortcutsPane tr').filter({ has: page.locator('[data-template-index="1"]') }))
      .toContainText('Comment the selection: Rejected');

    await templateKeys.nth(1).click();
    await page.keyboard.press('Alt+n');
    await expect(templateKeys.nth(1)).toHaveText('Alt+n');
    expect(await page.evaluate(() => window.crit.commentTemplates.getTemplateItems()[1].shortcut)).toBe('Alt+n');

    // Next block (j) cannot move to a template's key.
    const nextBlock = page.locator('#shortcutsPane .shortcut-binding[data-shortcut-action="Next block"]');
    await nextBlock.click();
    await page.keyboard.press('Alt+p');
    await expect(page.locator('.mini-toast', { hasText: 'Alt+p already comments' })).toBeVisible();
    await expect(nextBlock).toHaveText('j');
  });
});

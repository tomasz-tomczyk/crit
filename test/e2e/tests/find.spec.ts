import { test, expect, type Page } from '@playwright/test';
import { loadPage, clearAllComments, revealFile, openLineComment, fileHeader } from './helpers';

// Find is Pierre's CodeView search panel; Crit opens it from anywhere on the
// page and keeps its own file state in step. Git fixture: login.feature adds
// `alice@example.com` twice and `Welcome back, Alice` once; no other code
// file mentions alice.
const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';

const panel = (page: Page) => page.locator('[data-search-panel-overlay]');
const input = (page: Page) => panel(page).locator('input[data-search]');
const count = (page: Page) => panel(page).locator('[data-matches]');
const caseBtn = (page: Page) => panel(page).getByRole('button', { name: 'Match Case' });

// Blur to body, as after a click on a non-focusable part of the page.
const blur = (page: Page) => page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());

async function openFind(page: Page) {
  await blur(page);
  await page.keyboard.press(`${MOD}+f`);
  await expect(input(page)).toBeVisible();
  await expect(input(page)).toBeFocused();
}

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
});

test('Cmd/Ctrl+F opens find from the page; Enter, Shift+Enter and Cmd/Ctrl+G step; match case narrows', async ({ page }) => {
  await loadPage(page);
  await openFind(page);
  await input(page).fill('Alice');
  await expect(count(page)).toHaveText('1/3');

  await page.keyboard.press('Enter');
  await expect(count(page)).toHaveText('2/3');
  await page.keyboard.press('Shift+Enter');
  await expect(count(page)).toHaveText('1/3');
  await page.keyboard.press('Shift+Enter');
  await expect(count(page)).toHaveText('3/3');

  // Find-again works with focus outside the panel too.
  await blur(page);
  await page.keyboard.press(`${MOD}+g`);
  await expect(count(page)).toHaveText('1/3');
  await page.keyboard.press(`${MOD}+Shift+g`);
  await expect(count(page)).toHaveText('3/3');

  await caseBtn(page).click();
  await expect(caseBtn(page)).toHaveAttribute('aria-pressed', 'true');
  await expect(count(page)).toHaveText(/\/1$/);
});

test('typed letters reach the search box instead of firing page shortcuts', async ({ page }) => {
  await loadPage(page);
  await openFind(page);
  // Real key presses (fill() would skip the page's keydown handlers); every
  // letter, so a single-key shortcut can't swallow one.
  const text = 'the quick brown fox jumps over the lazy dog';
  await page.keyboard.type(text);
  await expect(input(page)).toHaveValue(text);
  await expect(input(page)).toBeFocused();
});

test('Esc in the panel closes it and clears the highlights', async ({ page }) => {
  await loadPage(page);
  await openFind(page);
  await input(page).fill('Alice');
  await expect(count(page)).toHaveText('1/3');
  await expect.poll(() => page.evaluate(() => CSS.highlights.has('diffs-search-current'))).toBe(true);

  await input(page).press('Escape');
  await expect(panel(page)).toHaveCount(0);
  await expect.poll(() => page.evaluate(() =>
    (CSS.highlights.get('diffs-search-match')?.size ?? 0) + (CSS.highlights.get('diffs-search-current')?.size ?? 0),
  )).toBe(0);
});

test('Esc in a comment textarea cancels the form and leaves find open', async ({ page }) => {
  await loadPage(page);
  await openFind(page);
  await input(page).fill('Alice');
  await expect(count(page)).toHaveText('1/3');

  const item = await revealFile(page, 'login.feature');
  const form = await openLineComment(page, item, 2);
  const textarea = form.locator('textarea');
  await expect(textarea).toBeFocused();
  await textarea.press('Escape');
  await expect(item.locator('.comment-form')).toHaveCount(0);
  await expect(input(page)).toBeVisible();
});

test('a match in a collapsed file opens it, and its header shows it open', async ({ page }) => {
  await loadPage(page);
  await revealFile(page, 'login.feature');
  const header = fileHeader(page, 'login.feature');
  await header.locator('.file-header-chevron').click();
  await expect(header).toHaveClass(/collapsed/);

  await openFind(page);
  await input(page).fill('Alice');
  await expect(count(page)).toHaveText('1/3');
  await expect(header).not.toHaveClass(/collapsed/);
  await expect(header.locator('.file-header-chevron')).toHaveAttribute('aria-expanded', 'true');

  // Crit recorded the change: a later re-render keeps the file open.
  await input(page).press('Escape');
  await page.keyboard.press(`${MOD}+f`);
  await input(page).press('Escape');
  await expect(header).not.toHaveClass(/collapsed/);
});

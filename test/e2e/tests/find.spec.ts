import { test, expect, type Page } from '@playwright/test';
import { loadPage, clearAllComments, revealFile, openLineComment } from './helpers';

// Find in diff: stepping, match case and Esc ordering on the git fixture.
// login.feature adds `alice@example.com` twice and `Welcome back, Alice`
// once; no other code file mentions alice.
const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';

const bar = (page: Page) => page.locator('.crit-find-bar');
const input = (page: Page) => page.locator('.crit-find-input');
const count = (page: Page) => page.locator('.crit-find-count');
const caseBtn = (page: Page) => page.locator('.crit-find-case');

async function openFind(page: Page) {
  await page.locator('#filesContainer').click({ position: { x: 5, y: 5 } });
  await page.keyboard.press(`${MOD}+f`);
  await expect(bar(page)).toBeVisible();
  await expect(input(page)).toBeFocused();
}

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
});

test('Enter, Shift+Enter and Cmd/Ctrl+G step through matches; match case narrows them', async ({ page }) => {
  await loadPage(page);
  await openFind(page);
  await input(page).fill('Alice');
  await expect(count(page)).toHaveText('1 of 3');

  await page.keyboard.press('Enter');
  await expect(count(page)).toHaveText('2 of 3');
  await page.keyboard.press('Shift+Enter');
  await expect(count(page)).toHaveText('1 of 3');
  await page.keyboard.press('Shift+Enter');
  await expect(count(page)).toHaveText('3 of 3');
  await page.keyboard.press(`${MOD}+g`);
  await expect(count(page)).toHaveText('1 of 3');
  await page.keyboard.press(`${MOD}+Shift+g`);
  await expect(count(page)).toHaveText('3 of 3');

  await caseBtn(page).click();
  await expect(caseBtn(page)).toHaveAttribute('aria-pressed', 'true');
  await expect(count(page)).toHaveText('1 of 1');
  await expect(input(page)).toBeFocused();

  await page.keyboard.press('Alt+c');
  await expect(caseBtn(page)).toHaveAttribute('aria-pressed', 'false');
  await expect(count(page)).toHaveText(/ of 3$/);
});

test('Esc closes the bar and returns focus to where find was opened', async ({ page }) => {
  await loadPage(page);
  await expect(page.locator('#commitDropdown')).toBeVisible();
  const opener = page.locator('#commitDropdownBtn');
  await opener.focus();
  await page.keyboard.press(`${MOD}+f`);
  await expect(input(page)).toBeFocused();

  await page.keyboard.press('Escape');
  await expect(bar(page)).toBeHidden();
  await expect(opener).toBeFocused();
  await expect(page.locator('#commitDropdown')).not.toHaveClass(/open/);
});

// Blur to body, as after a click on a non-focusable part of the page.
const blur = (page: Page) => page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());

// Keyboard-focused diff rows (Pierre marks them [data-selected-line]).
const selectedRows = (page: Page) =>
  page.locator('diffs-container [data-content] > [data-selected-line][data-line]').count();

test('Esc closes an open menu before the find bar', async ({ page }) => {
  await loadPage(page);
  await openFind(page);
  await input(page).fill('Alice');
  await expect(count(page)).toHaveText('1 of 3');

  await page.click('#commitDropdownBtn');
  await expect(page.locator('#commitDropdown')).toHaveClass(/open/);
  // From body, both the menu and the bar would take Esc: the menu goes first.
  await blur(page);
  await page.keyboard.press('Escape');
  await expect(page.locator('#commitDropdown')).not.toHaveClass(/open/);
  await expect(bar(page)).toBeVisible();

  // With nothing else open, Esc from the page closes the bar.
  await page.keyboard.press('Escape');
  await expect(bar(page)).toBeHidden();
});

test('Esc closes only the bar and keeps the focused line; the next Esc clears it', async ({ page }) => {
  await loadPage(page);
  await openFind(page);
  await input(page).fill('Alice');
  await expect(count(page)).toHaveText('1 of 3');

  await blur(page);
  await page.keyboard.press('j');
  await expect.poll(() => selectedRows(page)).toBeGreaterThan(0);

  await page.keyboard.press('Escape');
  await expect(bar(page)).toBeHidden();
  await expect.poll(() => selectedRows(page)).toBeGreaterThan(0);

  await page.keyboard.press('Escape');
  await expect.poll(() => selectedRows(page)).toBe(0);
});

test('Esc in a comment textarea cancels the form and leaves the bar open', async ({ page }) => {
  await loadPage(page);
  await openFind(page);
  await input(page).fill('Alice');
  await expect(count(page)).toHaveText('1 of 3');

  const item = await revealFile(page, 'login.feature');
  const form = await openLineComment(page, item, 2);
  const textarea = form.locator('textarea');
  await expect(textarea).toBeFocused();
  await textarea.press('Escape');
  await expect(item.locator('.comment-form')).toHaveCount(0);
  await expect(bar(page)).toBeVisible();
});

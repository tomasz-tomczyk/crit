import { test, expect, type Page } from '@playwright/test';
import { clearAllComments, loadPage, realCommitItems } from './helpers';

const card = (page: Page) => page.locator('#reviewConversation .commit-context');
const counter = (page: Page) => card(page).locator('.commit-context-counter');

// Click a picker row (optionally with Alt) and wait for the session refetch.
async function pick(page: Page, index: number, alt = false) {
  await page.click('#commitDropdownBtn');
  const response = page.waitForResponse(r => r.url().includes('/api/session') && r.status() === 200);
  await realCommitItems(page).nth(index).click(alt ? { modifiers: ['Alt'] } : undefined);
  await response;
  await page.keyboard.press('Escape');
}

test.describe('Commit context card', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('pinning a commit shows its subject; , and . step between commits', async ({ page }) => {
    await loadPage(page);
    await expect(card(page)).toHaveCount(0);

    await pick(page, 0); // newest
    await expect(counter(page)).toHaveText('Commit 2 of 2');
    await expect(card(page).locator('.commit-context-title')).toContainText('add auth');
    await expect(card(page).locator('[data-commit-ctl="next"]')).toBeDisabled();

    await page.locator('.main-content').click({ position: { x: 5, y: 5 } });
    await page.keyboard.press(',');
    await expect(counter(page)).toHaveText('Commit 1 of 2');
    await expect(card(page).locator('.commit-context-title')).toContainText('notification handler');
    await expect(page.locator('#commitDropdownLabel')).toContainText('only');

    await page.keyboard.press('.');
    await expect(counter(page)).toHaveText('Commit 2 of 2');
  });

  test('stepping with the card buttons keeps keyboard focus on the card', async ({ page }) => {
    await loadPage(page);
    await pick(page, 1); // oldest
    await expect(counter(page)).toHaveText('Commit 1 of 2');

    await card(page).locator('[data-commit-ctl="next"]').focus();
    await page.keyboard.press('Enter');
    await expect(counter(page)).toHaveText('Commit 2 of 2');
    // "next" is disabled on the newest commit, so focus moves to "prev".
    await expect(card(page).locator('[data-commit-ctl="prev"]')).toBeFocused();
  });

  test('a range lists its commits oldest first; clearing pins or changing scope removes the card', async ({ page }) => {
    await loadPage(page);
    await pick(page, 1);
    await pick(page, 0, true);
    const items = card(page).locator('.commit-context-list-msg');
    await expect(items).toHaveCount(2);
    await expect(items.first()).toContainText('notification handler');
    await expect(items.last()).toContainText('add auth');

    await page.click('#commitDropdownBtn');
    await page.locator('.commit-picker-item[data-commit=""]').click();
    await page.keyboard.press('Escape');
    await expect(card(page)).toHaveCount(0);

    // Clicking the active scope releases a pinned commit.
    await pick(page, 0);
    await expect(card(page)).toHaveCount(1);
    const response = page.waitForResponse(r => r.url().includes('/api/session') && r.status() === 200);
    await page.locator('#scopeToggle .toggle-btn.active').click();
    await response;
    await expect(card(page)).toHaveCount(0);
  });
});

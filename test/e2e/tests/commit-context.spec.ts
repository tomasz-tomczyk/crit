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

  test('with one commit beside uncommitted changes, . and , pin it', async ({ page, request }) => {
    const commits: Array<{ virtual?: boolean }> = await (await request.get('/api/commits')).json();
    const virtual = commits.filter(c => c.virtual);
    expect(virtual.length).toBeGreaterThan(0);
    await page.route('**/api/commits', route => route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([...virtual, commits.find(c => !c.virtual)]),
    }));
    const commitsResponse = page.waitForResponse(r => r.url().includes('/api/commits'));
    await loadPage(page);
    await commitsResponse;
    await expect(page.locator('#commitDropdown')).toBeVisible();

    await page.locator('.main-content').click({ position: { x: 5, y: 5 } });
    let response = page.waitForResponse(r => r.url().includes('/api/session') && r.status() === 200);
    await page.keyboard.press('.');
    await response;
    await expect(counter(page)).toHaveText('Commit 1 of 1');
    await expect(page.locator('#commitDropdownLabel')).toContainText('only');

    // Unpin, then the other direction pins it too.
    await page.locator('#scopeToggle .toggle-btn[data-scope="all"]').click();
    await expect(card(page)).toHaveCount(0);
    response = page.waitForResponse(r => r.url().includes('/api/session') && r.status() === 200);
    await page.keyboard.press(',');
    await response;
    await expect(counter(page)).toHaveText('Commit 1 of 1');
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

    // Clicking the scope set aside by the pin (marked .remembered, not
    // .active, while pinned) releases the pinned commit.
    await pick(page, 0);
    await expect(card(page)).toHaveCount(1);
    const response = page.waitForResponse(r => r.url().includes('/api/session') && r.status() === 200);
    await page.locator('#scopeToggle .toggle-btn.remembered').click();
    await response;
    await expect(card(page)).toHaveCount(0);
  });
});

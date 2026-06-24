import { test, expect, type Page } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

async function openCommitPicker(page: Page) {
  await page.click('#commitDropdownBtn');
  await expect(page.locator('#commitDropdown')).toHaveClass(/open/);
}


test.describe('Commit Selection', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('commit picker visible in sidebar on All scope', async ({ page }) => {
    await expect(page.locator('#commitDropdown')).toBeVisible();
  });

  test('dropdown label shows "All commits" by default', async ({ page }) => {
    await expect(page.locator('#commitDropdownLabel')).toHaveText('All commits');
  });

  test('dropdown opens on click and shows commits', async ({ page }) => {
    await openCommitPicker(page);

    const allItem = page.locator('.commit-picker-item[data-commit=""]');
    await expect(allItem).toBeVisible();
    await expect(allItem).toHaveClass(/active/);

    const firstCommit = page.locator('#commitDropdownList .commit-picker-item').first();
    await expect(firstCommit).toBeVisible();
    await expect(firstCommit.locator('.commit-picker-item-sha')).toBeVisible();
    await expect(firstCommit.locator('.commit-picker-item-msg')).toBeVisible();
    await expect(firstCommit.locator('.commit-picker-item-msg')).toContainText('add auth');
  });

  test('dropdown closes on Escape', async ({ page }) => {
    await openCommitPicker(page);
    await page.keyboard.press('Escape');
    await expect(page.locator('#commitDropdown')).not.toHaveClass(/open/);
  });

  test('dropdown closes on outside click', async ({ page }) => {
    await openCommitPicker(page);
    await page.click('.main-content');
    await expect(page.locator('#commitDropdown')).not.toHaveClass(/open/);
  });

  test('clicking a commit sets from pin and filters files', async ({ page }) => {
    await openCommitPicker(page);

    const commitItem = page.locator('#commitDropdownList .commit-picker-item').first();
    const responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await commitItem.click();
    await responsePromise;

    await expect(page.locator('#commitDropdown')).toHaveClass(/open/);
    await expect(page.locator('#commitDropdownLabel')).toContainText('only');
    await expect(commitItem).toHaveClass(/is-from/);

    const fileSections = page.locator('.file-section');
    await expect(async () => {
      const count = await fileSections.count();
      expect(count).toBeLessThanOrEqual(5);
      expect(count).toBeGreaterThan(0);
    }).toPass();
  });

  test('selecting "All commits" restores full view', async ({ page }) => {
    await openCommitPicker(page);
    const commitItem = page.locator('#commitDropdownList .commit-picker-item').first();
    const firstResponsePromise = page.waitForResponse(r => r.url().includes('/api/session'));
    await commitItem.click();
    await firstResponsePromise;
    await expect(page.locator('#commitDropdownLabel')).not.toHaveText('All commits');

    const allItem = page.locator('.commit-picker-item[data-commit=""]');
    const responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await allItem.click();
    await responsePromise;

    await expect(page.locator('#commitDropdownLabel')).toHaveText('All commits');
  });

  test('commit picker hidden when switching to Staged scope', async ({ page }) => {
    await expect(page.locator('#commitDropdown')).toBeVisible();

    const responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await page.click('#scopeToggle .toggle-btn[data-scope="staged"]');
    await responsePromise;

    await expect(page.locator('#commitDropdown')).toBeHidden();
  });

  test('commit picker reappears when switching back to All scope', async ({ page }) => {
    let responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await page.click('#scopeToggle .toggle-btn[data-scope="staged"]');
    await responsePromise;
    await expect(page.locator('#commitDropdown')).toBeHidden();

    responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await page.click('#scopeToggle .toggle-btn[data-scope="all"]');
    await responsePromise;

    await expect(page.locator('#commitDropdown')).toBeVisible();
  });

  test('commit picker visible on Branch scope', async ({ page }) => {
    let responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await page.click('#scopeToggle .toggle-btn[data-scope="staged"]');
    await responsePromise;
    await expect(page.locator('#commitDropdown')).toBeHidden();

    responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await page.click('#scopeToggle .toggle-btn[data-scope="branch"]');
    await responsePromise;

    await expect(page.locator('#commitDropdown')).toBeVisible();
  });

  test('selected commit resets on page reload', async ({ page }) => {
    await openCommitPicker(page);
    const commitItem = page.locator('#commitDropdownList .commit-picker-item').first();
    const responsePromise = page.waitForResponse(r => r.url().includes('/api/session'));
    await commitItem.click();
    await responsePromise;

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    await expect(page.locator('#commitDropdownLabel')).toHaveText('All commits');
  });

  test('commit picker hidden when only one commit exists', async ({ page }) => {
    await expect(page.locator('#commitDropdown')).toBeVisible();

    await page.route('**/api/commits', async (route) => {
      const response = await route.fetch();
      const commits = await response.json();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(commits.slice(0, 1)),
      });
    });

    const commitsResponse = page.waitForResponse(r => r.url().includes('/api/commits'));
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await commitsResponse;
    await expect(page.locator('#commitDropdown')).toBeHidden();
  });

  test('from pin marks selected commit, "All" loses active class', async ({ page }) => {
    await openCommitPicker(page);
    const commitItem = page.locator('#commitDropdownList .commit-picker-item').first();
    const responsePromise = page.waitForResponse(r => r.url().includes('/api/session'));
    await commitItem.click();
    await responsePromise;

    await expect(page.locator('.commit-picker-item[data-commit=""]')).not.toHaveClass(/active/);
    await expect(page.locator('#commitDropdownList .commit-picker-item.is-from')).toHaveCount(1);
  });

  test('alt+click sets through pin and sends a range param', async ({ page }) => {
    await openCommitPicker(page);

    const commits = page.locator('#commitDropdownList .commit-picker-item');
    await expect(commits).toHaveCount(2);

    // List is newest-first; pick older commit as from, newer as through.
    const firstResponse = page.waitForResponse(r => r.url().includes('/api/session'));
    await commits.nth(1).click();
    await firstResponse;

    const rangeRequest = page.waitForRequest(
      r => r.url().includes('/api/session') && /[?&]commit=[^&]*\.\.[^&]*/.test(r.url())
    );
    await commits.nth(0).click({ modifiers: ['Alt'] });
    await rangeRequest;

    await expect(page.locator('#commitDropdownLabel')).toContainText('\u2192');
    await expect(commits.nth(1)).toHaveClass(/is-from/);
    await expect(commits.nth(0)).toHaveClass(/is-through/);
  });

  test('alt+click through again clears the through pin', async ({ page }) => {
    await openCommitPicker(page);

    const commits = page.locator('#commitDropdownList .commit-picker-item');
    await expect(commits).toHaveCount(2);

    const firstResponse = page.waitForResponse(r => r.url().includes('/api/session'));
    await commits.nth(1).click();
    await firstResponse;

    const rangeResponse = page.waitForResponse(r => r.url().includes('/api/session'));
    await commits.nth(0).click({ modifiers: ['Alt'] });
    await rangeResponse;
    await expect(page.locator('#commitDropdownLabel')).toContainText('\u2192');

    const clearThroughResponse = page.waitForResponse(r => r.url().includes('/api/session'));
    await commits.nth(0).click({ modifiers: ['Alt'] });
    await clearThroughResponse;

    await expect(page.locator('#commitDropdownLabel')).toContainText('only');
    await expect(page.locator('#commitDropdownList .commit-picker-item.is-through')).toHaveCount(0);
  });

  test('"All commits" row clears from/through pins', async ({ page }) => {
    await openCommitPicker(page);

    const commits = page.locator('#commitDropdownList .commit-picker-item');
    await expect(commits).toHaveCount(2);

    const firstResponse = page.waitForResponse(r => r.url().includes('/api/session'));
    await commits.nth(1).click();
    await firstResponse;

    const rangeResponse = page.waitForResponse(r => r.url().includes('/api/session'));
    await commits.nth(0).click({ modifiers: ['Alt'] });
    await rangeResponse;

    const clearResponse = page.waitForResponse(r => r.url().includes('/api/session'));
    await page.locator('.commit-picker-item[data-commit=""]').click();
    await clearResponse;

    await expect(page.locator('#commitDropdownLabel')).toHaveText('All commits');
    await expect(page.locator('#commitDropdownList .commit-picker-item.is-from')).toHaveCount(0);
    await expect(page.locator('#commitDropdownList .commit-picker-item.is-through')).toHaveCount(0);
  });

  test('dropdown stays open across multiple selections', async ({ page }) => {
    await openCommitPicker(page);

    const commits = page.locator('#commitDropdownList .commit-picker-item');
    await expect(commits).toHaveCount(2);

    const firstResponse = page.waitForResponse(r => r.url().includes('/api/session'));
    await commits.nth(1).click();
    await firstResponse;

    const secondResponse = page.waitForResponse(r => r.url().includes('/api/session'));
    await commits.nth(0).click({ modifiers: ['Alt'] });
    await secondResponse;

    await expect(page.locator('#commitDropdown')).toHaveClass(/open/);
  });
});

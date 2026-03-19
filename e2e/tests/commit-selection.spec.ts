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

    // "All commits" item should be active
    const allItem = page.locator('.commit-picker-item[data-commit=""]');
    await expect(allItem).toBeVisible();
    await expect(allItem).toHaveClass(/active/);

    // Should show at least one commit
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

  test('selecting a commit filters files and updates label', async ({ page }) => {
    await openCommitPicker(page);

    const commitItem = page.locator('#commitDropdownList .commit-picker-item').first();
    const responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await commitItem.click();
    await responsePromise;

    // Dropdown should close after selection
    await expect(page.locator('#commitDropdown')).not.toHaveClass(/open/);

    // Label should update to show the selected commit
    await expect(page.locator('#commitDropdownLabel')).not.toHaveText('All commits');

    // The commit only has 4 files (server.go, deleted.txt, plan.md, handler.js)
    // whereas "All" has more (includes staged utils.go and unstaged config.yaml)
    const fileSections = page.locator('.file-section');
    await expect(async () => {
      const count = await fileSections.count();
      expect(count).toBeLessThanOrEqual(4);
      expect(count).toBeGreaterThan(0);
    }).toPass();
  });

  test('selecting "All commits" restores full view', async ({ page }) => {
    // First select a commit
    await openCommitPicker(page);
    const commitItem = page.locator('#commitDropdownList .commit-picker-item').first();
    const firstResponsePromise = page.waitForResponse(r => r.url().includes('/api/session'));
    await commitItem.click();
    await firstResponsePromise;

    // Now open again and select "All commits"
    await openCommitPicker(page);
    const allItem = page.locator('.commit-picker-item[data-commit=""]');
    const responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await allItem.click();
    await responsePromise;

    // Label should be back to "All commits"
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
    // Switch to staged
    let responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await page.click('#scopeToggle .toggle-btn[data-scope="staged"]');
    await responsePromise;
    await expect(page.locator('#commitDropdown')).toBeHidden();

    // Switch back to all
    responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await page.click('#scopeToggle .toggle-btn[data-scope="all"]');
    await responsePromise;

    await expect(page.locator('#commitDropdown')).toBeVisible();
  });

  test('commit picker visible on Branch scope', async ({ page }) => {
    const responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await page.click('#scopeToggle .toggle-btn[data-scope="branch"]');
    await responsePromise;

    await expect(page.locator('#commitDropdown')).toBeVisible();
  });

  test('selected commit resets on page reload', async ({ page }) => {
    // Select a commit
    await openCommitPicker(page);
    const commitItem = page.locator('#commitDropdownList .commit-picker-item').first();
    const responsePromise = page.waitForResponse(r => r.url().includes('/api/session'));
    await commitItem.click();
    await responsePromise;

    // Reload — selection should reset (commit selection is session-scoped)
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Label should show "All commits" after reload
    await expect(page.locator('#commitDropdownLabel')).toHaveText('All commits');
  });

  test('selected commit item gets active class, "All" loses it', async ({ page }) => {
    // Select a commit
    await openCommitPicker(page);
    const commitItem = page.locator('#commitDropdownList .commit-picker-item').first();
    const responsePromise = page.waitForResponse(r => r.url().includes('/api/session'));
    await commitItem.click();
    await responsePromise;

    // Open dropdown again to inspect state
    await openCommitPicker(page);

    // "All commits" should no longer be active
    await expect(page.locator('.commit-picker-item[data-commit=""]')).not.toHaveClass(/active/);
    // The selected commit should be active
    await expect(page.locator('#commitDropdownList .commit-picker-item.active')).toHaveCount(1);
  });
});

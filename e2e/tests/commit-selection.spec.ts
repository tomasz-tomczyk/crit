import { test, expect, type Page } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

test.afterEach(async ({ page }) => {
  // Reset commit cookie so other test files aren't affected
  await page.evaluate(() => {
    document.cookie = 'crit-diff-commit=; path=/; max-age=31536000; SameSite=Strict';
  });
});

test.describe('Commit Selection', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('commit list visible in sidebar on All scope', async ({ page }) => {
    await expect(page.locator('#commitDropdown')).toBeVisible();
  });

  test('commit list shows "All commits" as default active item', async ({ page }) => {
    await expect(page.locator('.commit-list-item[data-commit=""]')).toHaveClass(/active/);
    await expect(page.locator('.commit-list-item[data-commit=""]')).toHaveText('All commits');
  });

  test('commit list shows commits with SHA and message', async ({ page }) => {
    const firstCommit = page.locator('#commitDropdownList .commit-list-item').first();
    await expect(firstCommit).toBeVisible();
    await expect(firstCommit.locator('.commit-list-item-sha')).toBeVisible();
    await expect(firstCommit.locator('.commit-list-item-msg')).toBeVisible();
    // The commit message should contain "add auth"
    await expect(firstCommit.locator('.commit-list-item-msg')).toContainText('add auth');
  });

  test('selecting a commit filters files', async ({ page }) => {
    const commitItem = page.locator('#commitDropdownList .commit-list-item').first();
    const responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await commitItem.click();
    await responsePromise;

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
    const commitItem = page.locator('#commitDropdownList .commit-list-item').first();
    await commitItem.click();
    await page.waitForResponse(r => r.url().includes('/api/session'));

    // Now select "All commits"
    const allItem = page.locator('.commit-list-item[data-commit=""]');
    const responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await allItem.click();
    await responsePromise;

    await expect(allItem).toHaveClass(/active/);
  });

  test('commit list hidden when switching to Staged scope', async ({ page }) => {
    await expect(page.locator('#commitDropdown')).toBeVisible();

    const responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await page.click('#scopeToggle .toggle-btn[data-scope="staged"]');
    await responsePromise;

    await expect(page.locator('#commitDropdown')).toBeHidden();
  });

  test('commit list reappears when switching back to All scope', async ({ page }) => {
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

  test('commit list visible on Branch scope', async ({ page }) => {
    const responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await page.click('#scopeToggle .toggle-btn[data-scope="branch"]');
    await responsePromise;

    await expect(page.locator('#commitDropdown')).toBeVisible();
  });

  test('selected commit persists across page reload', async ({ page }) => {
    // Select a commit
    const commitItem = page.locator('#commitDropdownList .commit-list-item').first();
    await commitItem.click();
    await page.waitForResponse(r => r.url().includes('/api/session'));

    // Verify it's active
    await expect(commitItem).toHaveClass(/active/);

    // Reload and verify persistence via cookie
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(page.locator('#commitDropdownList .commit-list-item.active')).toHaveCount(1);
    await expect(page.locator('.commit-list-item[data-commit=""]')).not.toHaveClass(/active/);
  });

  test('selected commit item gets active class, "All" loses it', async ({ page }) => {
    // Select a commit
    const commitItem = page.locator('#commitDropdownList .commit-list-item').first();
    await commitItem.click();
    await page.waitForResponse(r => r.url().includes('/api/session'));

    // "All commits" should no longer be active
    await expect(page.locator('.commit-list-item[data-commit=""]')).not.toHaveClass(/active/);
    // The selected commit should be active
    await expect(page.locator('#commitDropdownList .commit-list-item.active')).toHaveCount(1);
  });
});

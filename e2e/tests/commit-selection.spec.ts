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

  test('commit dropdown visible on All scope', async ({ page }) => {
    await expect(page.locator('#commitDropdown')).toBeVisible();
  });

  test('commit dropdown shows correct label by default', async ({ page }) => {
    await expect(page.locator('#commitDropdownLabel')).toHaveText('All commits');
  });

  test('dropdown opens and shows commits on click', async ({ page }) => {
    await page.click('#commitDropdownBtn');
    await expect(page.locator('.commit-dropdown-menu')).toBeVisible();
    // "All commits" item + at least 1 real commit
    const items = page.locator('.commit-dropdown-item');
    await expect(items).not.toHaveCount(0);
    // Should have the "All commits" item
    await expect(page.locator('.commit-dropdown-item[data-commit=""]')).toBeVisible();
    // Should have at least one commit with a SHA
    await expect(page.locator('#commitDropdownList .commit-dropdown-item').first()).toBeVisible();
  });

  test('commit items show SHA, message, and time', async ({ page }) => {
    await page.click('#commitDropdownBtn');
    const firstCommit = page.locator('#commitDropdownList .commit-dropdown-item').first();
    await expect(firstCommit.locator('.commit-dropdown-item-sha')).toBeVisible();
    await expect(firstCommit.locator('.commit-dropdown-item-msg')).toBeVisible();
    await expect(firstCommit.locator('.commit-dropdown-item-time')).toBeVisible();
    // The commit message should contain "add auth"
    await expect(firstCommit.locator('.commit-dropdown-item-msg')).toContainText('add auth');
  });

  test('selecting a commit filters files and updates label', async ({ page }) => {
    // Open dropdown and select the commit
    await page.click('#commitDropdownBtn');
    const commitItem = page.locator('#commitDropdownList .commit-dropdown-item').first();
    const responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await commitItem.click();
    await responsePromise;

    // Dropdown should close
    await expect(page.locator('.commit-dropdown-menu')).toBeHidden();

    // Label should show commit SHA, not "All commits"
    await expect(page.locator('#commitDropdownLabel')).not.toHaveText('All commits');

    // The commit only has 4 files (server.go, deleted.txt, plan.md, handler.js)
    // whereas "All" has more (includes staged utils.go and unstaged config.yaml)
    // So file count should be <= 4
    const fileSections = page.locator('.file-section');
    await expect(async () => {
      const count = await fileSections.count();
      expect(count).toBeLessThanOrEqual(4);
      expect(count).toBeGreaterThan(0);
    }).toPass();
  });

  test('selecting "All commits" restores full view', async ({ page }) => {
    // First select a commit
    await page.click('#commitDropdownBtn');
    const commitItem = page.locator('#commitDropdownList .commit-dropdown-item').first();
    await commitItem.click();
    await page.waitForResponse(r => r.url().includes('/api/session'));

    // Now select "All commits"
    await page.click('#commitDropdownBtn');
    const allItem = page.locator('.commit-dropdown-item[data-commit=""]');
    const responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await allItem.click();
    await responsePromise;

    await expect(page.locator('#commitDropdownLabel')).toHaveText('All commits');
  });

  test('Escape closes dropdown', async ({ page }) => {
    await page.click('#commitDropdownBtn');
    await expect(page.locator('.commit-dropdown-menu')).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.locator('.commit-dropdown-menu')).toBeHidden();
  });

  test('outside click closes dropdown', async ({ page }) => {
    await page.click('#commitDropdownBtn');
    await expect(page.locator('.commit-dropdown-menu')).toBeVisible();
    await page.locator('.header-title').click();
    await expect(page.locator('.commit-dropdown-menu')).toBeHidden();
  });

  test('dropdown hidden when switching to Staged scope', async ({ page }) => {
    // Verify it's visible first
    await expect(page.locator('#commitDropdown')).toBeVisible();

    // Switch to staged scope
    const responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await page.click('#scopeToggle .toggle-btn[data-scope="staged"]');
    await responsePromise;

    await expect(page.locator('#commitDropdown')).toBeHidden();
  });

  test('dropdown reappears when switching back to All scope', async ({ page }) => {
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

  test('dropdown visible on Branch scope', async ({ page }) => {
    // Switch to branch scope — commit dropdown should remain visible
    const responsePromise = page.waitForResponse(r =>
      r.url().includes('/api/session') && r.status() === 200
    );
    await page.click('#scopeToggle .toggle-btn[data-scope="branch"]');
    await responsePromise;

    await expect(page.locator('#commitDropdown')).toBeVisible();
  });

  test('selected commit persists across page reload', async ({ page }) => {
    // Select a commit
    await page.click('#commitDropdownBtn');
    const commitItem = page.locator('#commitDropdownList .commit-dropdown-item').first();
    await commitItem.click();
    await page.waitForResponse(r => r.url().includes('/api/session'));

    // Grab the label text
    const labelText = await page.locator('#commitDropdownLabel').textContent();
    expect(labelText).not.toBe('All commits');

    // Reload and verify persistence via cookie
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(page.locator('#commitDropdownLabel')).toHaveText(labelText!);
  });

  test('"All commits" item gets active class when selected', async ({ page }) => {
    // By default "All commits" should be active
    await expect(page.locator('.commit-dropdown-item[data-commit=""]')).toHaveClass(/active/);
  });

  test('selected commit item gets active class', async ({ page }) => {
    // Select a commit
    await page.click('#commitDropdownBtn');
    const commitItem = page.locator('#commitDropdownList .commit-dropdown-item').first();
    await commitItem.click();
    await page.waitForResponse(r => r.url().includes('/api/session'));

    // Open dropdown again to check active state
    await page.click('#commitDropdownBtn');
    // The "All commits" item should no longer be active
    await expect(page.locator('.commit-dropdown-item[data-commit=""]')).not.toHaveClass(/active/);
    // The selected commit item should be active
    await expect(page.locator('#commitDropdownList .commit-dropdown-item.active')).toHaveCount(1);
  });
});

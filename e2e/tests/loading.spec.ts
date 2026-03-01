import { test, expect } from '@playwright/test';

test.describe('Page Loading', () => {
  test('page loads without errors, loading disappears, file sections appear', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(page.locator('.file-section')).not.toHaveCount(0);
  });

  test('branch name "feat/add-auth" is shown in header', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    const branchContext = page.locator('#branchContext');
    await expect(branchContext).toBeVisible();

    const branchName = page.locator('#branchName');
    await expect(branchName).toHaveText('feat/add-auth');
  });

  test('document title contains "Crit — feat/add-auth"', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    await expect(page).toHaveTitle(/Crit — feat\/add-auth/);
  });

  test('diff mode toggle is visible in git mode', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    const diffToggle = page.locator('#diffModeToggle');
    await expect(diffToggle).toBeVisible();
  });
});

test.describe('File Tree', () => {
  test('files have correct status icons', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Added files: plan.md and handler.js
    const addedIcons = page.locator('.tree-file-status-icon.added');
    await expect(addedIcons).toHaveCount(2);

    // Deleted file: deleted.txt
    const deletedIcons = page.locator('.tree-file-status-icon.deleted');
    await expect(deletedIcons).toHaveCount(1);

    // Modified file: server.go
    const modifiedIcons = page.locator('.tree-file-status-icon.modified');
    await expect(modifiedIcons).toHaveCount(1);
  });

  test('clicking a file in the tree scrolls to its section', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Find a tree-file entry for plan.md and click it
    const treeFile = page.locator('.tree-file', { has: page.locator('.tree-file-name', { hasText: 'plan.md' }) });
    await expect(treeFile).toBeVisible();
    await treeFile.click();

    // The corresponding file-section should be scrolled into view
    const section = page.locator('#file-section-plan\\.md');
    await expect(section).toBeVisible();
    await expect(section).toBeInViewport();
  });

  test('file tree header shows +/- stats', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    const stats = page.locator('#fileTreeStats');
    await expect(stats).toBeVisible();
    // Should contain the file count and addition/deletion stats
    await expect(stats).not.toBeEmpty();
    // Stats should show a "+" number for additions
    await expect(stats.locator('.tree-stat-add')).toBeVisible();
  });
});

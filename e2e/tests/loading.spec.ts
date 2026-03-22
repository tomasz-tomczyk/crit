import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

test.describe('Page Loading', () => {
  test('page loads without errors, loading disappears, file sections appear', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('.file-section')).not.toHaveCount(0);
  });

  test('branch name "feat/add-auth" is shown in header', async ({ page }) => {
    await loadPage(page);

    const branchContext = page.locator('#branchContext');
    await expect(branchContext).toBeVisible();

    const branchName = page.locator('#branchName');
    await expect(branchName).toHaveText('feat/add-auth');
  });

  test('document title contains "Crit — feat/add-auth"', async ({ page }) => {
    await loadPage(page);

    await expect(page).toHaveTitle(/Crit — feat\/add-auth/);
  });

  test('diff mode toggle is visible in git mode', async ({ page }) => {
    await loadPage(page);

    const diffToggle = page.locator('#diffModeToggle');
    await expect(diffToggle).toBeVisible();
  });

  test('does not show PR toggle when no PR exists', async ({ page, request }) => {
    await loadPage(page);
    await expect(page.locator('.pr-toggle-btn')).not.toBeVisible();
  });
});

test.describe('File Tree', () => {
  test('files have correct status icons', async ({ page }) => {
    await loadPage(page);

    // Added files: plan.md, handler.js, and config.yaml (untracked)
    const addedIcons = page.locator('.tree-file-status-icon.added');
    await expect(addedIcons).toHaveCount(3);

    // Deleted file: deleted.txt
    const deletedIcons = page.locator('.tree-file-status-icon.deleted');
    await expect(deletedIcons).toHaveCount(1);

    // Modified files: server.go and utils.go (staged modification)
    const modifiedIcons = page.locator('.tree-file-status-icon.modified');
    await expect(modifiedIcons).toHaveCount(2);
  });

  test('file tree header shows +/- stats', async ({ page }) => {
    await loadPage(page);

    const stats = page.locator('#fileTreeStats');
    await expect(stats).toBeVisible();
    // Should contain the file count and addition/deletion stats
    await expect(stats).not.toBeEmpty();
    // Stats should show a "+" number for additions
    await expect(stats.locator('.tree-stat-add')).toBeVisible();
  });
});

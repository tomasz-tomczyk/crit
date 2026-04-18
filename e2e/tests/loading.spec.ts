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

// File tree status icons and stats are covered by file-tree.spec.ts

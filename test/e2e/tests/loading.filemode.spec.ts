import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

test.describe('File Mode — Page Loading', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
  });

  test('page loads and shows file sections', async ({ page }) => {
    await expect(page.locator('.file-section')).toHaveCount(3);
  });

  test('no branch name shown in header (file mode)', async ({ page }) => {
    await expect(page.locator('#branchContext')).toBeHidden();
  });

  test('document title includes file names (no branch)', async ({ page }) => {
    await expect(page).toHaveTitle(/Crit — .*plan\.md/);
  });

  test('diff mode toggle is hidden in file mode', async ({ page }) => {
    await expect(page.locator('#diffModeToggle')).toBeHidden();
  });

  test('file headers do not show status badge in file mode', async ({ page }) => {
    await expect(page.locator('.file-header-badge')).toHaveCount(0);
  });

  test('stack breadcrumb and chip exit are hidden in file mode', async ({ page }) => {
    await expect(page.locator('#stackBreadcrumb')).toBeHidden();
    await expect(page.locator('#stackChipExit')).toBeHidden();
  });
});

test.describe('File Mode — Scope Cookie Resilience', () => {
  test('renders files even when git scope cookie is set', async ({ page }) => {
    await page.context().addCookies([
      { name: 'crit-diff-scope', value: 'staged', domain: 'localhost', path: '/' },
    ]);
    await page.goto('/');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(page.locator('.file-section')).toHaveCount(3);
  });
});

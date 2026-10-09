import { test, expect } from '@playwright/test';
import { loadPage, switchToDocumentView } from './helpers';

test.describe('Page Loading', () => {
  test('page loads without errors, loading disappears, file sections appear', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('.pierre-file-header').first()).toBeVisible();
    await expect(page.locator('diffs-container [data-line]').first()).toBeVisible();
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

  // Pluggable renderers (issue #989) load their libraries on first use: a
  // review without diagrams keeps today's page weight.
  test('a review without diagrams never requests mermaid.min.js', async ({ page }) => {
    const requested: string[] = [];
    page.on('request', req => requested.push(req.url()));
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await expect(doc.locator('.line-block').first()).toBeVisible();
    await expect(page.locator('.crit-render')).toHaveCount(0);
    expect(requested.some(u => u.endsWith('/app.js'))).toBe(true);
    expect(requested.filter(u => u.includes('mermaid.min.js'))).toEqual([]);
  });
});

// File tree status icons and stats are covered by file-tree.spec.ts

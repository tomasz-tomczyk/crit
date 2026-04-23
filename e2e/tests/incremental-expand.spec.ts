import { test, expect, type Page } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// routes.go has a large gap (>20 lines) between hunks, ideal for incremental expansion testing.
function routesSection(page: Page) {
  return page.locator('#file-section-routes\\.go');
}

test.describe('Incremental Expand — Split Mode (default)', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    // Click routes.go in the file tree to load its diff (may be lazy-loaded)
    const treeEntry = page.locator('.tree-file-name', { hasText: 'routes.go' });
    await treeEntry.click();
  });

  test('large gap spacer shows expand-down and expand-up controls', async ({ page }) => {
    const section = routesSection(page);
    await expect(section).toBeVisible();

    // The spacer between hunks with a gap > 20 should show directional controls
    const spacer = section.locator('.diff-spacer').first();
    await expect(spacer).toBeVisible();

    const expandDown = spacer.locator('[aria-label="Expand 20 lines down"]');
    const expandUp = spacer.locator('[aria-label="Expand 20 lines up"]');
    await expect(expandDown).toBeVisible();
    await expect(expandUp).toBeVisible();
  });

  test('clicking expand-down reveals 20 lines below previous hunk', async ({ page }) => {
    const section = routesSection(page);
    await expect(section).toBeVisible();

    // Count rows before expansion
    const rowsBefore = await section.locator('.diff-split-row').count();

    const spacer = section.locator('.diff-spacer').first();
    await expect(spacer).toBeVisible();

    // Get original spacer text to know the gap size
    const spacerText = await spacer.textContent();
    const gapMatch = spacerText?.match(/(\d+)/);
    expect(gapMatch).toBeTruthy();
    const originalGap = parseInt(gapMatch![1], 10);
    expect(originalGap).toBeGreaterThan(20);

    // Click expand-down
    const expandDown = spacer.locator('[aria-label="Expand 20 lines down"]');
    await expandDown.click();

    // After expansion, more rows should be visible
    await expect(async () => {
      const rowsAfter = await section.locator('.diff-split-row').count();
      expect(rowsAfter).toBe(rowsBefore + 20);
    }).toPass();

    // A spacer should still exist with updated remaining count
    const remainingSpacer = section.locator('.diff-spacer').first();
    await expect(remainingSpacer).toBeVisible();
    await expect(remainingSpacer).toContainText(`${originalGap - 20}`);
  });

  test('clicking expand-up reveals 20 lines above next hunk', async ({ page }) => {
    const section = routesSection(page);
    await expect(section).toBeVisible();

    const rowsBefore = await section.locator('.diff-split-row').count();

    const spacer = section.locator('.diff-spacer').first();
    await expect(spacer).toBeVisible();

    const spacerText = await spacer.textContent();
    const gapMatch = spacerText?.match(/(\d+)/);
    const originalGap = parseInt(gapMatch![1], 10);

    // Click expand-up
    const expandUp = spacer.locator('[aria-label="Expand 20 lines up"]');
    await expandUp.click();

    // After expansion, more rows should be visible
    await expect(async () => {
      const rowsAfter = await section.locator('.diff-split-row').count();
      expect(rowsAfter).toBe(rowsBefore + 20);
    }).toPass();

    // A spacer should still exist with updated remaining count
    const remainingSpacer = section.locator('.diff-spacer').first();
    await expect(remainingSpacer).toBeVisible();
    await expect(remainingSpacer).toContainText(`${originalGap - 20}`);
  });

  test('after partial expansion, spacer shows updated remaining count', async ({ page }) => {
    const section = routesSection(page);
    await expect(section).toBeVisible();

    const spacer = section.locator('.diff-spacer').first();
    const spacerText = await spacer.textContent();
    const originalGap = parseInt(spacerText!.match(/(\d+)/)![1], 10);

    // Expand down first
    await spacer.locator('[aria-label="Expand 20 lines down"]').click();

    // Spacer should show remaining gap
    const remainingGap = originalGap - 20;
    await expect(section.locator('.diff-spacer').first()).toContainText(
      `${remainingGap}`
    );
  });

});

test.describe('Incremental Expand — Unified Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    // Click routes.go in the file tree to load its diff (may be lazy-loaded)
    const treeEntry = page.locator('.tree-file-name', { hasText: 'routes.go' });
    await treeEntry.click();
    // Switch to unified mode
    const unifiedBtn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
    await unifiedBtn.click();
    await expect(page.locator('.diff-container.unified').first()).toBeVisible();
  });

  test('large gap spacer shows expand-down and expand-up controls in unified mode', async ({ page }) => {
    const section = routesSection(page);
    await expect(section).toBeVisible();

    const spacer = section.locator('.diff-spacer').first();
    await expect(spacer).toBeVisible();

    const expandDown = spacer.locator('[aria-label="Expand 20 lines down"]');
    const expandUp = spacer.locator('[aria-label="Expand 20 lines up"]');
    await expect(expandDown).toBeVisible();
    await expect(expandUp).toBeVisible();
  });

  test('clicking expand-down in unified mode adds context lines', async ({ page }) => {
    const section = routesSection(page);
    await expect(section).toBeVisible();

    const linesBefore = await section.locator('.diff-line').count();

    const spacer = section.locator('.diff-spacer').first();
    const spacerText = await spacer.textContent();
    const originalGap = parseInt(spacerText!.match(/(\d+)/)![1], 10);
    expect(originalGap).toBeGreaterThan(20);

    await spacer.locator('[aria-label="Expand 20 lines down"]').click();

    // After expansion, more diff lines should be visible
    await expect(async () => {
      const linesAfter = await section.locator('.diff-line').count();
      expect(linesAfter).toBe(linesBefore + 20);
    }).toPass();

    // Spacer should still exist with updated count
    await expect(section.locator('.diff-spacer').first()).toContainText(
      `${originalGap - 20}`
    );
  });

  test('clicking expand-up in unified mode adds context lines', async ({ page }) => {
    const section = routesSection(page);
    await expect(section).toBeVisible();

    const linesBefore = await section.locator('.diff-line').count();

    const spacer = section.locator('.diff-spacer').first();
    const spacerText = await spacer.textContent();
    const originalGap = parseInt(spacerText!.match(/(\d+)/)![1], 10);

    await spacer.locator('[aria-label="Expand 20 lines up"]').click();

    await expect(async () => {
      const linesAfter = await section.locator('.diff-line').count();
      expect(linesAfter).toBe(linesBefore + 20);
    }).toPass();

    await expect(section.locator('.diff-spacer').first()).toContainText(
      `${originalGap - 20}`
    );
  });

});

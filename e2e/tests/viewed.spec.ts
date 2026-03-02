import { test, expect, type Page } from '@playwright/test';

async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

// ============================================================
// Viewed Checkbox — Git Mode
// ============================================================
test.describe('Viewed Checkbox — Git Mode', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    // Clear any persisted viewed state
    await page.evaluate(() => {
      for (let i = localStorage.length - 1; i >= 0; i--) {
        const key = localStorage.key(i);
        if (key && key.startsWith('crit-viewed-')) localStorage.removeItem(key);
      }
    });
    await loadPage(page);
  });

  test('each file section has a viewed checkbox', async ({ page }) => {
    const checkboxes = page.locator('.file-header-viewed input[type="checkbox"]');
    const sections = page.locator('.file-section');
    const sectionCount = await sections.count();
    await expect(checkboxes).toHaveCount(sectionCount);
  });

  test('viewed checkbox starts unchecked', async ({ page }) => {
    const checkbox = page.locator('.file-header-viewed input[type="checkbox"]').first();
    await expect(checkbox).not.toBeChecked();
  });

  test('clicking viewed checkbox marks file as viewed', async ({ page }) => {
    const section = page.locator('#file-section-plan\\.md');
    const checkbox = section.locator('.file-header-viewed input[type="checkbox"]');
    await checkbox.click();
    await expect(checkbox).toBeChecked();
  });

  test('checking viewed collapses the file section', async ({ page }) => {
    const section = page.locator('#file-section-plan\\.md');
    await expect(section).toHaveAttribute('open', '');

    const checkbox = section.locator('.file-header-viewed input[type="checkbox"]');
    await checkbox.click();

    await expect(section).not.toHaveAttribute('open', '');
  });

  test('clicking viewed checkbox does not toggle section open/close on its own', async ({ page }) => {
    // First collapse the section manually
    const section = page.locator('#file-section-plan\\.md');
    const header = section.locator('summary.file-header');
    await header.click();
    await expect(section).not.toHaveAttribute('open', '');

    // Now uncheck viewed — section should stay collapsed (checkbox click doesn't toggle details)
    // First we need to check it to have something to uncheck
    const checkbox = section.locator('.file-header-viewed input[type="checkbox"]');
    // Re-open section so we can interact with checkbox
    await header.click();
    await expect(section).toHaveAttribute('open', '');

    // Check it — collapses
    await checkbox.click();
    await expect(section).not.toHaveAttribute('open', '');

    // Re-open manually
    await header.click();
    await expect(section).toHaveAttribute('open', '');

    // Uncheck — should NOT collapse (only checking collapses)
    await checkbox.click();
    await expect(checkbox).not.toBeChecked();
    await expect(section).toHaveAttribute('open', '');
  });

  test('viewed checkbox updates the tree indicator', async ({ page }) => {
    const section = page.locator('#file-section-plan\\.md');
    const checkbox = section.locator('.file-header-viewed input[type="checkbox"]');

    const treeFile = page.locator('.tree-file', {
      has: page.locator('.tree-file-name', { hasText: 'plan.md' }),
    });

    // No viewed indicator initially
    await expect(treeFile.locator('.tree-viewed-check')).toHaveCount(0);

    await checkbox.click();

    // Tree file should have viewed class and checkmark
    await expect(treeFile).toHaveClass(/viewed/);
    await expect(treeFile.locator('.tree-viewed-check')).toBeVisible();
  });

  test('viewed count updates in header', async ({ page }) => {
    const viewedCount = page.locator('#viewedCount');
    await expect(viewedCount).toContainText('0 /');

    // Check one file
    const section = page.locator('#file-section-plan\\.md');
    const checkbox = section.locator('.file-header-viewed input[type="checkbox"]');
    await checkbox.click();

    await expect(viewedCount).toContainText('1 /');
  });

  test('viewed state persists across page reload', async ({ page }) => {
    const section = page.locator('#file-section-plan\\.md');
    const checkbox = section.locator('.file-header-viewed input[type="checkbox"]');
    await checkbox.click();
    await expect(checkbox).toBeChecked();

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    const reloadedCheckbox = page.locator('#file-section-plan\\.md .file-header-viewed input[type="checkbox"]');
    await expect(reloadedCheckbox).toBeChecked();
  });
});

// ============================================================
// Collapse/Expand All — Git Mode
// ============================================================
test.describe('Collapse/Expand All — Git Mode', () => {
  test.beforeEach(async ({ page }) => {
    await loadPage(page);
  });

  test('collapse all button exists in file tree header', async ({ page }) => {
    const btn = page.locator('.file-tree-collapse-btn');
    await expect(btn).toBeVisible();
  });

  test('clicking collapse all closes all expanded file sections', async ({ page }) => {
    // Verify at least some sections are open
    const openSections = page.locator('.file-section[open]');
    const initialOpen = await openSections.count();
    expect(initialOpen).toBeGreaterThan(0);

    await page.locator('.file-tree-collapse-btn').click();

    // All sections should be closed
    await expect(page.locator('.file-section[open]')).toHaveCount(0);
  });

  test('clicking expand all after collapse opens all sections', async ({ page }) => {
    // Collapse all first
    await page.locator('.file-tree-collapse-btn').click();
    await expect(page.locator('.file-section[open]')).toHaveCount(0);

    // Now expand all
    await page.locator('.file-tree-collapse-btn').click();

    // All sections should be open
    const allSections = page.locator('.file-section');
    const totalCount = await allSections.count();
    await expect(page.locator('.file-section[open]')).toHaveCount(totalCount);
  });
});

import { test, expect } from '@playwright/test';
import * as fs from 'fs';
import { execSync } from 'child_process';
import { clearAllComments, loadPage } from './helpers';

// Read fixture state written by setup-fixtures.sh
function readFixtureState(): { fixtureDir: string } {
  const raw = fs.readFileSync('/tmp/crit-e2e-state-3123', 'utf8');
  const env: Record<string, string> = {};
  for (const line of raw.trim().split('\n')) {
    const eq = line.indexOf('=');
    if (eq >= 0) {
      env[line.slice(0, eq)] = line.slice(eq + 1);
    }
  }
  if (!env['CRIT_FIXTURE_DIR']) {
    throw new Error('CRIT_FIXTURE_DIR not set in state file');
  }
  return { fixtureDir: env['CRIT_FIXTURE_DIR'] };
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
  test('viewed state resets when file content changes between rounds', async ({ page, request }) => {
    // Reset server state
    await request.post('/api/round-complete');
    await clearAllComments(request);
    await loadPage(page);

    const { fixtureDir } = readFixtureState();

    // Mark plan.md as viewed
    const section = page.locator('#file-section-plan\\.md');
    const checkbox = section.locator('.file-header-viewed input[type="checkbox"]');
    await checkbox.click();
    await expect(checkbox).toBeChecked();
    await expect(section).not.toHaveAttribute('open', '');

    // Verify tree indicator shows viewed
    const treeFile = page.locator('.tree-file', {
      has: page.locator('.tree-file-name', { hasText: 'plan.md' }),
    });
    await expect(treeFile).toHaveClass(/viewed/);

    // Modify plan.md on disk and commit the change
    const planPath = `${fixtureDir}/plan.md`;
    const original = fs.readFileSync(planPath, 'utf8');
    fs.writeFileSync(planPath, original + '\n## Added by test\n\nNew content.\n');
    execSync('git add plan.md && git commit -q -m "test: modify plan.md"', { cwd: fixtureDir });

    // Trigger round-complete so the server picks up the file change
    await request.post('/api/round-complete');

    // Wait for UI to refresh — the viewed checkbox should be unchecked
    await expect(checkbox).not.toBeChecked({ timeout: 5_000 });

    // File section should be open (uncollapsed)
    await expect(section).toHaveAttribute('open', '');

    // Tree view should no longer show the viewed indicator
    await expect(treeFile).not.toHaveClass(/viewed/);
    await expect(treeFile.locator('.tree-viewed-check')).toHaveCount(0);
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

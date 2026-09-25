import { test, expect, type Page } from '@playwright/test';
import * as fs from 'fs';
import { execSync } from 'child_process';
import { clearAllComments, loadPage, fileHeader, fileItem, revealFile, treePaths } from './helpers';
import { stateFilePath } from './state-file';

// Read fixture state written by setup-fixtures.sh
function readFixtureState(): { fixtureDir: string } {
  const raw = fs.readFileSync(stateFilePath(process.env.CRIT_TEST_PORT || '3123'), 'utf8');
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

// Pierre items have no <details>: a collapsed file is a header with the
// `collapsed` class and no code rows in its shadow root.
function viewedBox(page: Page, filePath: string) {
  return fileHeader(page, filePath).locator('.file-header-viewed input[type="checkbox"]');
}

async function expectCollapsed(page: Page, filePath: string) {
  await expect(fileHeader(page, filePath)).toHaveClass(/\bcollapsed\b/);
  await expect(fileItem(page, filePath).locator('[data-line]')).toHaveCount(0);
}

async function expectExpanded(page: Page, filePath: string) {
  await expect(fileHeader(page, filePath)).not.toHaveClass(/\bcollapsed\b/);
  await expect(fileItem(page, filePath).locator('[data-line]').first()).toBeVisible();
}

// Collapse/expand through the header's chevron (a plain header click).
async function clickChevron(page: Page, filePath: string) {
  await fileHeader(page, filePath).locator('.file-header-chevron').click();
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
    // CodeView only mounts files near the viewport, so visit each one.
    const paths = await treePaths(page);
    expect(paths.length).toBe(10);
    for (const p of paths) {
      await revealFile(page, p);
      await expect(viewedBox(page, p)).toHaveCount(1);
    }
  });

  test('viewed checkbox starts unchecked', async ({ page }) => {
    const checkbox = page.locator('.pierre-file-header .file-header-viewed input[type="checkbox"]').first();
    await expect(checkbox).toBeAttached();
    await expect(checkbox).not.toBeChecked();
  });

  test('clicking viewed checkbox marks file as viewed', async ({ page }) => {
    await revealFile(page, 'plan.md');
    await viewedBox(page, 'plan.md').click();
    await expect(viewedBox(page, 'plan.md')).toBeChecked();
  });

  test('checking viewed collapses the file section', async ({ page }) => {
    await revealFile(page, 'plan.md');
    await expectExpanded(page, 'plan.md');

    await viewedBox(page, 'plan.md').click();

    await expectCollapsed(page, 'plan.md');
  });

  test('clicking viewed checkbox does not toggle section open/close on its own', async ({ page }) => {
    await revealFile(page, 'plan.md');
    // Collapse manually, then re-open
    await clickChevron(page, 'plan.md');
    await expectCollapsed(page, 'plan.md');
    await clickChevron(page, 'plan.md');
    await expectExpanded(page, 'plan.md');

    // Check it — collapses
    await viewedBox(page, 'plan.md').click();
    await expectCollapsed(page, 'plan.md');

    // Re-open manually
    await clickChevron(page, 'plan.md');
    await expectExpanded(page, 'plan.md');

    // Uncheck — should NOT collapse (only checking collapses)
    await viewedBox(page, 'plan.md').click();
    await expect(viewedBox(page, 'plan.md')).not.toBeChecked();
    await expectExpanded(page, 'plan.md');
  });

  test('viewed checkbox updates the tree indicator', async ({ page }) => {
    await revealFile(page, 'plan.md');
    const treeFile = page.locator('.tree-file', {
      has: page.locator('.tree-file-name', { hasText: 'plan.md' }),
    });

    // No viewed indicator initially
    await expect(treeFile.locator('.tree-viewed-check')).toHaveCount(0);

    await viewedBox(page, 'plan.md').click();

    // Tree file should have viewed class and checkmark
    await expect(treeFile).toHaveClass(/viewed/);
    await expect(treeFile.locator('.tree-viewed-check')).toBeVisible();
  });

  test('viewed count updates in header', async ({ page }) => {
    const viewedCount = page.locator('#viewedCount');
    await expect(viewedCount).toContainText('0 /');

    await revealFile(page, 'plan.md');
    await viewedBox(page, 'plan.md').click();

    await expect(viewedCount).toContainText('1 /');
  });

  test('viewed state persists across page reload', async ({ page }) => {
    await revealFile(page, 'plan.md');
    await viewedBox(page, 'plan.md').click();
    await expect(viewedBox(page, 'plan.md')).toBeChecked();

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    await revealFile(page, 'plan.md');
    await expect(viewedBox(page, 'plan.md')).toBeChecked();
  });

  test('viewed state resets when file content changes between rounds', async ({ page, request }) => {
    // Reset server state
    await request.post('/api/round-complete');
    await clearAllComments(request);
    await loadPage(page);

    const { fixtureDir } = readFixtureState();

    // Mark plan.md as viewed
    await revealFile(page, 'plan.md');
    await viewedBox(page, 'plan.md').click();
    await expect(viewedBox(page, 'plan.md')).toBeChecked();
    await expectCollapsed(page, 'plan.md');

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
    await expect(viewedBox(page, 'plan.md')).not.toBeChecked({ timeout: 5_000 });

    // File section should be open (uncollapsed)
    await revealFile(page, 'plan.md');
    await expectExpanded(page, 'plan.md');

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
    // Some sections start expanded
    await expect(page.locator('.pierre-file-header:not(.collapsed)').first()).toBeVisible();

    await page.locator('.file-tree-collapse-btn').click();

    // Collapsed files are header-only, so all ten fit on screen at once:
    // every file is mounted and every one is collapsed with no code rows.
    await expect(page.locator('.pierre-file-header')).toHaveCount(10);
    await expect(page.locator('.pierre-file-header.collapsed')).toHaveCount(10);
    await expect(page.locator('diffs-container [data-line]')).toHaveCount(0);
  });

  test('clicking expand all after collapse opens all sections', async ({ page }) => {
    // Collapse all first
    await page.locator('.file-tree-collapse-btn').click();
    await expect(page.locator('.pierre-file-header.collapsed')).toHaveCount(10);

    // Now expand all
    await page.locator('.file-tree-collapse-btn').click();

    // Every file is expanded again (visit each — only nearby files mount).
    const paths = await treePaths(page);
    expect(paths.length).toBe(10);
    for (const p of paths) {
      await revealFile(page, p);
      await expect(fileHeader(page, p)).not.toHaveClass(/\bcollapsed\b/);
    }
  });
});

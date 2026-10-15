import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import { execSync } from 'child_process';
import {
  clearAllComments, loadPage, fileHeader, fileItem, revealFile, treePaths,
  readFixtureState, roundComplete,
} from './helpers';

// Viewed state lives in per-origin localStorage (app.js viewedStorageKey), and
// every Playwright test gets a fresh browser context, so no test starts with
// a file already marked viewed.

// Pierre items have no <details>: a collapsed file is a header with the
// `collapsed` class and no code rows in its shadow root.
function viewedBox(page: Page, filePath: string) {
  return fileHeader(page, filePath).locator('.file-header-viewed input[type="checkbox"]');
}

function treeFile(page: Page, filePath: string) {
  return page.locator(`.tree-file[data-tree-path="${filePath}"]`);
}

async function expectCollapsed(page: Page, filePath: string) {
  await expect(fileHeader(page, filePath)).toHaveClass(/\bcollapsed\b/);
  await expect(fileItem(page, filePath).locator('[data-line]')).toHaveCount(0);
}

async function expectExpanded(page: Page, filePath: string) {
  await expect(fileHeader(page, filePath)).not.toHaveClass(/\bcollapsed\b/);
  await expect(fileItem(page, filePath).locator('[data-line]').first()).toBeVisible();
}

// Collapse/expand through the header's chevron button.
async function clickChevron(page: Page, filePath: string) {
  await fileHeader(page, filePath).locator('.file-header-chevron').click();
}

// The tree must list exactly the session's files: earlier specs can add (and
// remove) files in the shared fixture, so never hard-code the count.
async function sessionPaths(page: Page, request: APIRequestContext): Promise<string[]> {
  const session = await (await request.get('/api/session?scope=all')).json();
  const paths = session.files.map((f: { path: string }) => f.path).sort();
  await expect.poll(async () => (await treePaths(page)).slice().sort()).toEqual(paths);
  return paths;
}

// ============================================================
// Viewed Checkbox — Git Mode
// ============================================================
test.describe('Viewed Checkbox — Git Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('each file section has a viewed checkbox', async ({ page, request }) => {
    // CodeView only mounts files near the viewport, so visit each one.
    for (const p of await sessionPaths(page, request)) {
      await revealFile(page, p);
      await expect(viewedBox(page, p)).toHaveCount(1);
    }
  });

  test('checking viewed marks the file, collapses it, and updates the tree and header count', async ({ page }) => {
    const viewedCount = page.locator('#viewedCount');
    await expect(viewedCount).toContainText('0 /');

    await revealFile(page, 'plan.md');
    await expect(viewedBox(page, 'plan.md')).not.toBeChecked();
    await expectExpanded(page, 'plan.md');
    await expect(treeFile(page, 'plan.md')).not.toHaveClass(/viewed/);
    await expect(treeFile(page, 'plan.md').locator('.tree-viewed-check')).toHaveCount(0);

    await viewedBox(page, 'plan.md').click();

    await expect(viewedBox(page, 'plan.md')).toBeChecked();
    await expectCollapsed(page, 'plan.md');
    await expect(treeFile(page, 'plan.md')).toHaveClass(/viewed/);
    await expect(treeFile(page, 'plan.md').locator('.tree-viewed-check')).toBeVisible();
    await expect(viewedCount).toContainText('1 /');
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

  test('viewed state persists across page reload', async ({ page }) => {
    await revealFile(page, 'plan.md');
    await viewedBox(page, 'plan.md').click();
    await expect(viewedBox(page, 'plan.md')).toBeChecked();

    await loadPage(page);

    await revealFile(page, 'plan.md');
    await expect(viewedBox(page, 'plan.md')).toBeChecked();
  });

  test('viewed state resets when file content changes between rounds', async ({ page, request }) => {
    const fixtureDir = readFixtureState().CRIT_FIXTURE_DIR;
    expect(fixtureDir).toBeTruthy();

    // Mark plan.md as viewed
    await revealFile(page, 'plan.md');
    await viewedBox(page, 'plan.md').click();
    await expect(viewedBox(page, 'plan.md')).toBeChecked();
    await expectCollapsed(page, 'plan.md');
    await expect(treeFile(page, 'plan.md')).toHaveClass(/viewed/);

    // Modify plan.md on disk and commit only that file: the fixture keeps
    // staged changes (utils.go, login.feature) that a plain commit would sweep
    // in. The commit is undone below so every later spec (and the mobile
    // project) sees the original fixture.
    const git = (args: string) => execSync(`git ${args}`, { cwd: fixtureDir, encoding: 'utf8' });
    const headBefore = git('rev-parse HEAD');
    const statusBefore = git('status --porcelain');
    const planPath = `${fixtureDir}/plan.md`;
    const original = fs.readFileSync(planPath, 'utf8');
    fs.writeFileSync(planPath, original + '\n## Added by test\n\nNew content.\n');
    git('add plan.md');
    git('commit -q -m "test: modify plan.md" -- plan.md');
    try {
      // Round-complete so the server picks up the file change.
      await roundComplete(request);

      // The UI refreshes: the viewed checkbox is unchecked again.
      await expect(viewedBox(page, 'plan.md')).not.toBeChecked();

      // File section should be open (uncollapsed)
      await revealFile(page, 'plan.md');
      await expectExpanded(page, 'plan.md');

      // Tree view should no longer show the viewed indicator
      await expect(treeFile(page, 'plan.md')).not.toHaveClass(/viewed/);
      await expect(treeFile(page, 'plan.md').locator('.tree-viewed-check')).toHaveCount(0);
    } finally {
      // git revert refuses while other changes are staged, so drop the commit
      // instead: move HEAD back, then restore plan.md's index entry and text.
      git('reset -q --soft HEAD~1');
      git('reset -q HEAD -- plan.md');
      fs.writeFileSync(planPath, original);
      expect(git('rev-parse HEAD')).toBe(headBefore);
      expect(git('status --porcelain')).toBe(statusBefore);
      await roundComplete(request);
    }
  });
});

// ============================================================
// Collapse/Expand All — Git Mode
// ============================================================
test.describe('Collapse/Expand All — Git Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('collapse all closes every file section and expand all reopens them', async ({ page, request }) => {
    const btn = page.locator('.file-tree-collapse-btn');
    await expect(btn).toBeVisible();
    const paths = await sessionPaths(page, request);

    // Some sections start expanded
    await expect(page.locator('.pierre-file-header:not(.collapsed)').first()).toBeVisible();

    await btn.click();

    // Collapsed files are header-only, so all of them fit on screen at once:
    // every file is mounted and every one is collapsed with no code rows.
    await expect(page.locator('.pierre-file-header')).toHaveCount(paths.length);
    await expect(page.locator('.pierre-file-header.collapsed')).toHaveCount(paths.length);
    await expect(page.locator('diffs-container [data-line]')).toHaveCount(0);

    // Now expand all
    await btn.click();

    // Every file is expanded again (visit each — only nearby files mount).
    for (const p of paths) {
      await revealFile(page, p);
      await expect(fileHeader(page, p)).not.toHaveClass(/\bcollapsed\b/);
    }
  });
});

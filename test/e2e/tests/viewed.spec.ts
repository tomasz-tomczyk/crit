import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import { execSync } from 'child_process';
import {
  clearAllComments, loadPage, clearFocus, fileHeader, fileItem, focusedFile, focusedRows, hoverLine, pressUntil,
  hoveredFile, reviewScroller, revealFile, treePaths, waitForScrollStable,
  readFixtureState, roundComplete, switchToDocumentView,
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

// Load the review with no persisted viewed state and no keyboard focus.
async function loadFreshReview(page: Page) {
  await page.goto('/');
  await page.evaluate(() => {
    for (let i = localStorage.length - 1; i >= 0; i--) {
      const key = localStorage.key(i);
      if (key && key.startsWith('crit-viewed-')) localStorage.removeItem(key);
    }
  });
  await loadPage(page);
  await clearFocus(page);
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
// Viewed Shortcut (a) — Git Mode
// ============================================================
test.describe('Viewed Shortcut — Git Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadFreshReview(page);
  });

  test('a toggles viewed on the keyboard-focused file', async ({ page }) => {
    const [first] = await treePaths(page);
    await page.keyboard.press('j');
    await expect(fileItem(page, first).locator('[data-selected-line]').first()).toBeAttached();

    const status = page.locator('#copyStatus');
    await page.keyboard.press('a');
    await expect(viewedBox(page, first)).toBeChecked();
    await expectCollapsed(page, first);
    await expect(status).toHaveText(`Marked ${first} viewed`);

    // Focus stays on the collapsed file, so a second press un-views it.
    await page.keyboard.press('a');
    await expect(viewedBox(page, first)).not.toBeChecked();
    await expect(status).toHaveText(`Unmarked ${first} viewed`);
  });

  test('a toggles viewed on the file under the mouse when nothing is focused', async ({ page }) => {
    const item = await revealFile(page, 'server.go');
    await hoverLine(page, item, 1);

    await page.keyboard.press('a');
    await expect(viewedBox(page, 'server.go')).toBeChecked();
    await expectCollapsed(page, 'server.go');
  });

  test('a follows the mouse once it moves after j', async ({ page }) => {
    const [first] = await treePaths(page);
    await page.keyboard.press('j');
    await expect(fileItem(page, first).locator('[data-selected-line]').first()).toBeAttached();

    const item = await revealFile(page, 'server.go');
    await hoverLine(page, item, 1);

    await page.keyboard.press('a');
    await expect(viewedBox(page, 'server.go')).toBeChecked();
    await revealFile(page, first);
    await expect(viewedBox(page, first)).not.toBeChecked();
  });

  test('keyboard focus wins while the mouse rests still over the list', async ({ page }) => {
    // j scrolls the list under the pointer; that must not hand `a` to the mouse.
    const box = await reviewScroller(page).boundingBox();
    expect(box).toBeTruthy();
    await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height - 40);
    await page.keyboard.press('j');
    await expect.poll(async () => (await focusedRows(page)).length).toBeGreaterThan(0);
    const current = focusedFile(await focusedRows(page));
    // Only meaningful if the pointer rests on a different file.
    await expect.poll(() => hoveredFile(page)).not.toBe(current);
    expect(await hoveredFile(page)).toBeTruthy();

    await page.keyboard.press('a');
    await expect(viewedBox(page, current)).toBeChecked();
    await expect(page.locator('#viewedCount')).toContainText('1 /');
  });

  test('after Esc clears the keyboard focus, a falls back to the mouse', async ({ page }) => {
    const item = await revealFile(page, 'server.go');
    await hoverLine(page, item, 1);
    await page.keyboard.press('j');
    await expect.poll(async () => (await focusedRows(page)).length).toBeGreaterThan(0);
    await page.keyboard.press('Escape');
    await expect.poll(async () => (await focusedRows(page)).length).toBe(0);

    // The pointer has not moved; whichever file is under it now gets toggled.
    const hovered = await hoveredFile(page);
    expect(hovered).toBeTruthy();
    await page.keyboard.press('a');
    await expect(viewedBox(page, hovered!)).toBeChecked();
    await expect(page.locator('#viewedCount')).toContainText('1 /');
  });

  test('once the mouse leaves the list, a falls back to the keyboard focus', async ({ page }) => {
    const [first] = await treePaths(page);
    await page.keyboard.press('j');
    await expect(fileItem(page, first).locator('[data-selected-line]').first()).toBeAttached();
    // Cross the list, then rest outside it (the file tree).
    const box = (await reviewScroller(page).boundingBox())!;
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.move(box.x - 40, box.y + box.height / 2);

    await page.keyboard.press('a');
    await expect(viewedBox(page, first)).toBeChecked();
  });

  test('holding a toggles viewed once', async ({ page }) => {
    const [first] = await treePaths(page);
    await page.keyboard.press('j');
    await expect(fileItem(page, first).locator('[data-selected-line]').first()).toBeAttached();

    // A second keydown without a keyup is an auto-repeat (repeat: true).
    await page.keyboard.down('a');
    await page.keyboard.down('a');
    await page.keyboard.up('a');
    await expect(viewedBox(page, first)).toBeChecked();
  });

  test('a in visual mode leaves visual mode', async ({ page }) => {
    const [first] = await treePaths(page);
    await page.keyboard.press('j');
    await expect(fileItem(page, first).locator('[data-selected-line]').first()).toBeAttached();
    await page.keyboard.press('Shift+V');
    await expect(page.locator('body')).toHaveClass(/\bvisual-mode\b/);

    await page.keyboard.press('a');
    await expect(viewedBox(page, first)).toBeChecked();
    await expect(page.locator('body')).not.toHaveClass(/\bvisual-mode\b/);
  });

  test('a in visual mode keeps the focused line when the file stays open', async ({ page }) => {
    const [first] = await treePaths(page);
    await page.keyboard.press('j');
    await expect(fileItem(page, first).locator('[data-selected-line]').first()).toBeAttached();
    // Viewed and collapsed, then expanded again: un-viewing it leaves it open.
    await page.keyboard.press('a');
    await page.keyboard.press('o');
    await expectExpanded(page, first);
    await page.keyboard.press('Shift+V');
    await expect(page.locator('body')).toHaveClass(/\bvisual-mode\b/);

    await page.keyboard.press('a');
    await expect(viewedBox(page, first)).not.toBeChecked();
    await expect(page.locator('body')).not.toHaveClass(/\bvisual-mode\b/);
    await expect(fileItem(page, first).locator('[data-selected-line]').first()).toBeAttached();
  });

  test('a does nothing without keyboard focus or a hovered file', async ({ page }) => {
    await page.keyboard.press('a');
    await expect(page.locator('#viewedCount')).toContainText('0 /');
  });

  test('j after a continues past the viewed file, not from the top', async ({ page }) => {
    const paths = await treePaths(page);
    await page.keyboard.press('j');
    await expect.poll(async () => (await focusedRows(page)).length).toBeGreaterThan(0);
    const firstFile = focusedFile(await focusedRows(page));
    // Walk into the next file so there is a focusable file on both sides.
    await pressUntil(page, 'j', async () => focusedFile(await focusedRows(page)) !== firstFile, { state: () => focusedRows(page) });
    const current = focusedFile(await focusedRows(page));

    await page.keyboard.press('a');
    await expectCollapsed(page, current);

    await page.keyboard.press('j');
    await expect.poll(async () => paths.indexOf(focusedFile(await focusedRows(page))))
      .toBeGreaterThan(paths.indexOf(current));
  });

  test('viewed checkbox tooltip names the shortcut', async ({ page }) => {
    const [first] = await treePaths(page);
    await revealFile(page, first);
    await expect(fileHeader(page, first).locator('.file-header-viewed')).toHaveAttribute('title', 'Viewed (a)');
  });
});

// ============================================================
// Collapse Shortcut (o) — Git Mode
// ============================================================
test.describe('Collapse Shortcut — Git Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadFreshReview(page);
  });

  test('o collapses and expands the keyboard-focused file without marking it viewed', async ({ page }) => {
    await page.keyboard.press('j');
    await expect.poll(async () => (await focusedRows(page)).length).toBeGreaterThan(0);
    const current = focusedFile(await focusedRows(page));

    const status = page.locator('#copyStatus');
    await page.keyboard.press('o');
    await expectCollapsed(page, current);
    await expect(viewedBox(page, current)).not.toBeChecked();
    await expect(status).toHaveText(`Collapsed ${current}`);

    await page.keyboard.press('o');
    await expectExpanded(page, current);
    await expect(status).toHaveText(`Expanded ${current}`);
  });

  test('o collapses the file under the mouse when nothing is focused', async ({ page }) => {
    const item = await revealFile(page, 'server.go');
    await hoverLine(page, item, 1);

    await page.keyboard.press('o');
    await expectCollapsed(page, 'server.go');
  });

  test('chevron tooltip names the shortcut', async ({ page }) => {
    const [first] = await treePaths(page);
    await revealFile(page, first);
    await expect(fileHeader(page, first).locator('.file-header-chevron')).toHaveAttribute('title', 'Collapse or expand (o)');
  });

  for (const how of ['o', 'viewed checkbox'] as const) {
    test(`collapsing mid-file with ${how} brings its header back to the top`, async ({ page }) => {
      const item = await revealFile(page, 'server.go');
      // Scroll partway into the file so its header is stuck at the top.
      const scroller = reviewScroller(page);
      await scroller.evaluate((el, path) => {
        const host = Array.from(el.querySelectorAll('diffs-container'))
          .find(h => h.querySelector(`.pierre-file-header[data-file-path="${path}"]`))!;
        el.scrollTop += host.getBoundingClientRect().top - el.getBoundingClientRect().top + 300;
      }, 'server.go');
      await waitForScrollStable(page);
      const listTop = (await scroller.boundingBox())!.y;
      expect((await item.boundingBox())!.y).toBeLessThan(listTop - 100);

      if (how === 'o') {
        await hoverLine(page, item, 30);
        await page.keyboard.press('o');
      } else {
        await viewedBox(page, 'server.go').click();
      }
      await expectCollapsed(page, 'server.go');
      await waitForScrollStable(page);
      await expect.poll(async () => Math.abs((await item.boundingBox())!.y - listTop)).toBeLessThan(2);
    });
  }

  // A mounted rendered document keeps its reader anchor while it resizes.
  // Collapsing hides it, and a hidden section must not pull the list away
  // from the collapsed header.
  test('collapsing a document mid-file with o brings its header back to the top', async ({ page }) => {
    const doc = await switchToDocumentView(page);
    const item = fileItem(page, 'plan.md');
    const scroller = reviewScroller(page);
    await scroller.evaluate((el) => {
      const host = Array.from(el.querySelectorAll('diffs-container'))
        .find(h => h.querySelector('.pierre-file-header[data-file-path="plan.md"]'))!;
      el.scrollTop += host.getBoundingClientRect().top - el.getBoundingClientRect().top + 300;
    });
    await waitForScrollStable(page);
    const listTop = (await scroller.boundingBox())!.y;
    expect((await item.boundingBox())!.y).toBeLessThan(listTop - 100);

    const box = (await doc.locator('.document-wrapper').boundingBox())!;
    await page.mouse.move(box.x + box.width / 2, listTop + 200);
    await page.keyboard.press('o');
    await expect(fileHeader(page, 'plan.md')).toHaveClass(/\bcollapsed\b/);
    await waitForScrollStable(page);
    await expect.poll(async () => Math.abs((await item.boundingBox())!.y - listTop)).toBeLessThan(2);
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

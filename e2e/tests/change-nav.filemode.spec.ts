import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { clearAllComments, loadPage, mdSection } from './helpers';

// Get the fixture directory path from .crit.json location
async function getFixtureDir(request: APIRequestContext): Promise<string> {
  const finishRes = await request.post('/api/finish');
  const finishData = await finishRes.json();
  return path.dirname(finishData.review_file);
}

// Perform a round-complete cycle: finish, modify file, trigger round-complete, wait for UI refresh
async function doRoundWithEdit(
  page: Page,
  request: APIRequestContext,
  fixtureDir: string,
  filePath: string,
  newContent: string,
) {
  // Click finish to enter waiting state
  await page.locator('#finishBtn').click();
  await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);

  // Modify the file on disk
  fs.writeFileSync(path.join(fixtureDir, filePath), newContent);

  // Wait for the file watcher to detect the edit (polls every 1s).
  // The watcher snapshots PreviousContent on first detection, which is
  // required for inter-round diffs. The UI shows edit count when detected.
  await expect(page.locator('#waitingEdits')).toContainText('edit', { timeout: 5_000 });

  // Trigger round-complete
  await request.post('/api/round-complete');

  // Wait for UI to refresh
  await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/, { timeout: 5_000 });
}

// Generate a unique modification of the original content.
// Each call produces a different version to avoid stale-diff issues when
// the server's in-memory content matches a previous modification.
let modCounter = 0;
function makeModified(original: string, areas: 'single' | 'multi' = 'single'): string {
  modCounter++;
  let result = original.replace(
    "We're adding API key authentication to the server. This is phase 1 of the auth system.",
    `We're adding method-${modCounter} authentication to the server. This is variant ${modCounter} of the auth system.`,
  );
  if (areas === 'multi') {
    result = result.replace(
      '- **Week 1**: Middleware + key model',
      `- **Week 1**: Method-${modCounter} middleware + token model`,
    );
  }
  return result;
}

// ============================================================
// Change Navigation — File Mode
// ============================================================
test.describe('Change Navigation — File Mode', () => {
  let fixtureDir: string;
  let originalContent: string;

  test.beforeAll(async ({ request }) => {
    fixtureDir = await getFixtureDir(request);
    originalContent = fs.readFileSync(path.join(fixtureDir, 'plan.md'), 'utf-8');
  });

  test.afterAll(() => {
    // Restore original file content for other test suites
    if (fixtureDir && originalContent) {
      fs.writeFileSync(path.join(fixtureDir, 'plan.md'), originalContent);
    }
  });

  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
    // NOTE: We intentionally do NOT restore the file here. The server's
    // in-memory content must match the disk to avoid phantom edit detection
    // by the file watcher. Each test uses a unique modification instead.
  });

  test('no change indicators in round 1 (before any edits)', async ({ page }) => {
    await loadPage(page);
    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // No blocks should have any change indicator
    await expect(section.locator('.line-block-added, .line-block-modified, .deletion-marker')).toHaveCount(0);
  });

  test('no change-nav widget in round 1', async ({ page }) => {
    await loadPage(page);
    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();

    await expect(section.locator('.change-nav')).toHaveCount(0);
  });

  test('changed blocks get color-coded indicators after round-complete with edits', async ({ page, request }) => {
    await loadPage(page);

    const modified = makeModified(originalContent);
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', modified);

    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // At least one block should have a change indicator (modified = amber for replacements)
    const changedBlocks = section.locator('.line-block-added, .line-block-modified');
    await expect(changedBlocks.first()).toBeVisible();
    const count = await changedBlocks.count();
    expect(count).toBeGreaterThan(0);
  });

  test('change-nav widget appears after round-complete with edits', async ({ page, request }) => {
    await loadPage(page);

    const modified = makeModified(originalContent);
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', modified);

    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Change nav widget should be visible
    const changeNav = section.locator('.change-nav');
    await expect(changeNav).toBeVisible();

    // Should have up/down buttons and a label
    await expect(changeNav.locator('.change-nav-btn')).toHaveCount(2);
    await expect(changeNav.locator('.change-nav-label')).toBeVisible();
  });

  test('change-nav label shows change count', async ({ page, request }) => {
    await loadPage(page);

    // Make changes in two different areas of the file
    const modified = makeModified(originalContent, 'multi');
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', modified);

    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();

    const label = section.locator('.change-nav-label');
    await expect(label).toBeVisible();
    // Label should contain "change" and a count > 0
    const text = await label.textContent();
    expect(text).toMatch(/\d+\s*change/);
  });

  test('n key navigates to next change', async ({ page, request }) => {
    await loadPage(page);

    const modified = makeModified(originalContent);
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', modified);

    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await expect(section.locator('.line-block-added, .line-block-modified')).not.toHaveCount(0);

    // Press n to navigate to first change
    await page.keyboard.press('n');

    // A changed block should have the flash animation
    const flashed = section.locator('.line-block.change-flash');
    await expect(flashed.first()).toBeVisible();
  });

  test('N key navigates to previous change', async ({ page, request }) => {
    await loadPage(page);

    const modified = makeModified(originalContent);
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', modified);

    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
    await expect(section.locator('.line-block-added, .line-block-modified')).not.toHaveCount(0);

    // Press Shift+N to navigate to previous change (wraps to last)
    await page.keyboard.press('Shift+N');

    const flashed = section.locator('.line-block.change-flash');
    await expect(flashed.first()).toBeVisible();
  });

  test('n wraps to first change when scrolled past all changes', async ({ page, request }) => {
    await loadPage(page);

    const modified = makeModified(originalContent);
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', modified);

    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Scroll to the very bottom so all changes are above viewport
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));

    await page.keyboard.press('n');

    // Should wrap to the first change
    const flashed = section.locator('.line-block.change-flash');
    await expect(flashed.first()).toBeVisible();
  });

  test('change-nav down arrow button navigates to next change', async ({ page, request }) => {
    await loadPage(page);

    const modified = makeModified(originalContent);
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', modified);

    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Click the down arrow button
    const downBtn = section.locator('.change-nav-btn[data-dir="1"]');
    await downBtn.click();

    const flashed = section.locator('.line-block.change-flash');
    await expect(flashed.first()).toBeVisible();
  });

  test('change-nav up arrow button navigates to previous change', async ({ page, request }) => {
    await loadPage(page);

    const modified = makeModified(originalContent);
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', modified);

    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Click the up arrow button (wraps to last)
    const upBtn = section.locator('.change-nav-btn[data-dir="-1"]');
    await upBtn.click();

    const flashed = section.locator('.line-block.change-flash');
    await expect(flashed.first()).toBeVisible();
  });

  test('n then n moves forward through two change groups', async ({ page, request }) => {
    await loadPage(page);

    // Use multi-area changes so there are 2 separated change groups
    const modified = makeModified(originalContent, 'multi');
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', modified);

    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Scroll to top so both changes are below
    await page.evaluate(() => window.scrollTo(0, 0));

    // Press n — should go to first change
    await page.keyboard.press('n');
    const firstFlashed = section.locator('.line-block.change-flash').first();
    await expect(firstFlashed).toBeVisible();
    // Record document-level Y of the first flashed element
    const firstAbsY = await firstFlashed.evaluate(el => el.getBoundingClientRect().top + window.scrollY);

    // Press n again — should go to second change (further down the document)
    await page.keyboard.press('n');
    const secondFlashed = section.locator('.line-block.change-flash').first();
    await expect(secondFlashed).toBeVisible();
    const secondAbsY = await secondFlashed.evaluate(el => el.getBoundingClientRect().top + window.scrollY);

    expect(secondAbsY).toBeGreaterThan(firstAbsY);
  });

  test('n does not go backwards when user scrolls past a change', async ({ page, request }) => {
    await loadPage(page);

    const modified = makeModified(originalContent, 'multi');
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', modified);

    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Navigate to first change from top
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.keyboard.press('n');
    await expect(section.locator('.line-block.change-flash').first()).toBeVisible();

    // Get document-level Y of the first change group
    const firstChangeAbsY = await section.locator('.line-block-added, .line-block-modified').first().evaluate(
      el => el.getBoundingClientRect().top + window.scrollY
    );

    // Manually scroll well past the first change
    await page.evaluate((y) => window.scrollTo(0, y + 300), firstChangeAbsY);

    // Press n — should go forward to next change, not back to first
    await page.keyboard.press('n');
    const flashed = section.locator('.line-block.change-flash').first();
    await expect(flashed).toBeVisible();

    // The flashed element should be below the first change in the document
    const flashedAbsY = await flashed.evaluate(el => el.getBoundingClientRect().top + window.scrollY);
    expect(flashedAbsY).toBeGreaterThan(firstChangeAbsY);
  });

  test('N goes backward after n navigated forward', async ({ page, request }) => {
    await loadPage(page);

    const modified = makeModified(originalContent, 'multi');
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', modified);

    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Navigate forward twice to reach the second change
    await page.evaluate(() => window.scrollTo(0, 0));
    await page.keyboard.press('n');
    await expect(section.locator('.line-block.change-flash').first()).toBeVisible();
    const firstAbsY = await section.locator('.line-block.change-flash').first().evaluate(
      el => el.getBoundingClientRect().top + window.scrollY
    );

    await page.keyboard.press('n');
    await expect(section.locator('.line-block.change-flash').first()).toBeVisible();
    const secondAbsY = await section.locator('.line-block.change-flash').first().evaluate(
      el => el.getBoundingClientRect().top + window.scrollY
    );
    // Verify we actually moved forward
    expect(secondAbsY).toBeGreaterThan(firstAbsY);

    // Now press N — should go back to the first change
    await page.keyboard.press('Shift+N');
    const backFlashed = section.locator('.line-block.change-flash').first();
    await expect(backFlashed).toBeVisible();
    const backAbsY = await backFlashed.evaluate(el => el.getBoundingClientRect().top + window.scrollY);
    expect(backAbsY).toBeLessThan(secondAbsY);
  });

  test('N wraps to last change when scrolled above all changes', async ({ page, request }) => {
    await loadPage(page);

    const modified = makeModified(originalContent, 'multi');
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', modified);

    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Scroll to the very top
    await page.evaluate(() => window.scrollTo(0, 0));

    // Press N — no change above viewport center, should wrap to last
    await page.keyboard.press('Shift+N');

    const flashed = section.locator('.line-block.change-flash').first();
    await expect(flashed).toBeVisible();
  });

  test('n/N shortcuts are listed in keyboard shortcuts overlay', async ({ page }) => {
    await loadPage(page);

    // Open shortcuts overlay
    await page.keyboard.press('?');
    const overlay = page.locator('#shortcutsOverlay');
    await expect(overlay).toHaveClass(/active/);

    // Should list n and N shortcuts
    await expect(overlay.locator('text=Next change')).toBeVisible();
    await expect(overlay.locator('text=Previous change')).toBeVisible();
  });

  test('changed blocks have colored left border (box-shadow)', async ({ page, request }) => {
    await loadPage(page);

    const modified = makeModified(originalContent);
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', modified);

    const section = mdSection(page);
    const changedBlock = section.locator('.line-block-added, .line-block-modified').first();
    await expect(changedBlock).toBeVisible();

    // Verify the block has a box-shadow (the colored indicator)
    const boxShadow = await changedBlock.evaluate(el => getComputedStyle(el).boxShadow);
    expect(boxShadow).not.toBe('none');
  });
});

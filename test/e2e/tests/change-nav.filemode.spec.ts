import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { execFileSync } from 'child_process';
import { randomUUID } from 'crypto';
import { clearAllComments, loadPage, mdSection, fileItem, sessionCwd } from './helpers';

// The rounds below arrive over SSE; rendering can finish before the event
// stream connects on a busy runner, so also wait for /api/events.
async function loadPageWithEvents(page: Page) {
  const connected = page.waitForResponse(response =>
    new URL(response.url()).pathname === '/api/events' && response.ok());
  await loadPage(page);
  await connected;
}

// Drop any change-flash so the next navigation has to add its own.
async function clearFlash(page: Page) {
  await page.evaluate(() => document.querySelectorAll('.change-flash').forEach(el => el.classList.remove('change-flash')));
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
  const finished = page.waitForResponse(response =>
    new URL(response.url()).pathname === '/api/finish' && response.request().method() === 'POST');
  await page.locator('#finishBtn').click();
  expect((await finished).ok()).toBe(true);
  await expect(page.locator('#waitingOverlay')).toHaveClass(/active/, { timeout: 15_000 });

  // Modify the file on disk
  fs.writeFileSync(path.join(fixtureDir, filePath), newContent);

  // Wait for the file watcher to detect the edit (polls every 1s).
  // The watcher snapshots PreviousContent on first detection, which is
  // required for inter-round diffs. The UI shows edit count when detected.
  await expect(page.locator('#waitingEdits')).toContainText('edit', { timeout: 5_000 });

  // Trigger round-complete
  await expect(await request.post('/api/round-complete')).toBeOK();

  // Wait for UI to refresh
  await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/, { timeout: 15_000 });
}

// The review list scrolls inside #filesContainer (not the window); the
// document-Y helpers below measure positions in that scroller.

// Generate a unique modification of the original content.
// Each call produces a different version to avoid stale-diff issues when
// the server's in-memory content matches a previous modification.
let modificationId = '';
function makeModified(original: string, areas: 'single' | 'multi' = 'single'): string {
  // A retried test runs in a new worker against the same daemon and disk.
  // A process-local counter can repeat the previous edit, producing no diff.
  const marker = randomUUID();
  modificationId = marker;
  let result = original.replace(
    "We're adding API key authentication to the server. This is phase 1 of the auth system.",
    `We're adding method-${marker} authentication to the server. This is variant ${marker} of the auth system.`,
  );
  if (areas === 'multi') {
    result = result.replace(
      '- **Week 1**: Middleware + key model',
      `- **Week 1**: Method-${marker} middleware + token model`,
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
    fixtureDir = await sessionCwd(request);
    // The working file may still contain the failed worker's edit. The
    // fixture's committed baseline survives retries and suite restarts.
    originalContent = execFileSync('git', ['show', 'HEAD:plan.md'], {
      cwd: fixtureDir, encoding: 'utf-8',
    });
  });

  test.afterAll(() => {
    // Restore original file content for other test suites
    if (fixtureDir && originalContent) {
      fs.writeFileSync(path.join(fixtureDir, 'plan.md'), originalContent);
    }
  });

  test.beforeEach(async ({ page, request }) => {
    // These tests exercise round diffs, not the OS clipboard. Finishing awaits
    // clipboard writes, which can stall when parallel browser projects take focus.
    await page.addInitScript(() => {
      Object.defineProperty(navigator.clipboard, 'writeText', { value: async () => {} });
    });
    await clearAllComments(request);
    // NOTE: We intentionally do NOT restore the file here. The server's
    // in-memory content must match the disk to avoid phantom edit detection
    // by the file watcher. Each test uses a unique modification instead.
  });

  test('no change indicators or change-nav widget in round 1 (before any edits)', async ({ page }) => {
    await loadPageWithEvents(page);
    const section = await mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();

    await expect(section.locator('.line-block-added, .line-block-modified, .deletion-marker')).toHaveCount(0);
    await expect(section.locator('.change-nav')).toHaveCount(0);
  });

  test('one changed area: indicators, change-nav widget, and n / N / arrow navigation incl. wrap from a virtualized position', async ({ page, request }) => {
    await loadPageWithEvents(page);
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', makeModified(originalContent));

    const section = await mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();

    // Changed blocks get a colour-coded indicator (modified = amber for
    // replacements), drawn as a coloured left border (box-shadow).
    const changedBlocks = section.locator('.line-block-added, .line-block-modified');
    await expect(changedBlocks.first()).toBeVisible();
    await expect(changedBlocks.first()).toContainText(`method-${modificationId} authentication`);
    expect(await changedBlocks.first().evaluate(el => getComputedStyle(el).boxShadow)).not.toBe('none');

    // The change-nav widget has up/down buttons and a label.
    const changeNav = section.locator('.change-nav');
    await expect(changeNav).toBeVisible();
    await expect(changeNav.locator('.change-nav-btn')).toHaveCount(2);
    await expect(changeNav.locator('.change-nav-label')).toBeVisible();

    // Each way of navigating flashes the changed block. Clear the previous
    // flash first so every step has to produce its own.
    const flashed = section.locator('.line-block.change-flash');
    const steps: Array<[string, () => Promise<void>]> = [
      ['n (next)', () => page.keyboard.press('n')],
      ['N (previous, wraps to last)', () => page.keyboard.press('Shift+N')],
      ['down arrow button', () => section.locator('.change-nav-btn[data-dir="1"]').click()],
      ['up arrow button', () => section.locator('.change-nav-btn[data-dir="-1"]').click()],
    ];
    for (const [name, step] of steps) {
      await clearFlash(page);
      await expect(flashed).toHaveCount(0);
      await step();
      await expect(flashed.first(), name).toBeVisible();
    }

    // n wraps to the first change when scrolled past all changes. Exercise the
    // virtualized path: a cached change's DOM cannot scroll itself back into
    // view after Pierre has unmounted its file.
    await clearFlash(page);
    await page.evaluate(() => document.getElementById('filesContainer')!.scrollTo(0, document.getElementById('filesContainer')!.scrollHeight));
    await expect(fileItem(page, 'plan.md')).toHaveCount(0);
    await page.keyboard.press('n');
    await expect(flashed.first()).toBeVisible();
  });

  test('two changed areas: label counts changes; n / N move forward and back, follow manual scrolls, and wrap', async ({ page, request }) => {
    await loadPageWithEvents(page);
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', makeModified(originalContent, 'multi'));

    const section = await mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
    const flashed = section.locator('.line-block.change-flash').first();
    const absY = (el: typeof flashed) => el.evaluate(node => node.getBoundingClientRect().top + document.getElementById('filesContainer')!.scrollTop);
    const scrollTop = (y: number) => page.evaluate(top => document.getElementById('filesContainer')!.scrollTo(0, top), y);

    // The label shows the change count.
    await expect(section.locator('.change-nav-label')).toHaveText(/\d+\s*change/);

    // N from the top (no change above the viewport centre) wraps to the last change.
    await scrollTop(0);
    await clearFlash(page);
    await page.keyboard.press('Shift+N');
    await expect(flashed).toBeVisible();

    // From the top, n then n moves forward through two change groups, and N
    // goes back to the first.
    await scrollTop(0);
    await clearFlash(page);
    await page.keyboard.press('n');
    await expect(flashed).toBeVisible();
    const firstAbsY = await absY(flashed);
    await clearFlash(page);
    await page.keyboard.press('n');
    await expect(flashed).toBeVisible();
    const secondAbsY = await absY(flashed);
    expect(secondAbsY).toBeGreaterThan(firstAbsY);
    await clearFlash(page);
    await page.keyboard.press('Shift+N');
    await expect(flashed).toBeVisible();
    expect(await absY(flashed)).toBeLessThan(secondAbsY);

    // After the user scrolls well past the first change, n goes forward to
    // the next change rather than back to the first.
    await scrollTop(0);
    await clearFlash(page);
    await page.keyboard.press('n');
    await expect(flashed).toBeVisible();
    const firstChangeAbsY = await absY(section.locator('.line-block-added, .line-block-modified').first());
    await scrollTop(firstChangeAbsY + 300);
    await clearFlash(page);
    await page.keyboard.press('n');
    await expect(flashed).toBeVisible();
    expect(await absY(flashed)).toBeGreaterThan(firstChangeAbsY);
  });

  test('a changed table stays one navigation group and keeps aligned round-diff columns', async ({ page, request }) => {
    await loadPageWithEvents(page);
    const current = fs.readFileSync(path.join(fixtureDir, 'plan.md'), 'utf-8');
    const marker = Date.now();
    const modified = current +
      `\n\n| Choice ${marker} | Result |\n` +
      '| --- | --- |\n' +
      '| short | available |\n' +
      '| a much longer visible choice | waiting for review |\n';
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', modified);

    const section = await mdSection(page);
    const table = section.locator('table.native-table').last();
    await expect(table).toBeVisible();
    await expect(table.locator('.line-block-added')).toHaveCount(4);
    const label = section.locator('.change-nav-label');
    await expect(label).toHaveText(/\/ \d+ changes?/);
    const totalGroups = Number((await label.textContent())?.match(/\/ (\d+) changes?/)?.[1]);
    expect(totalGroups).toBeGreaterThan(0);
    for (let index = 0; index < totalGroups; index++) {
      await page.keyboard.press('n');
      if (await table.locator('.line-block.change-flash').count()) break;
    }
    await expect(table.locator('.line-block.change-flash')).toHaveCount(4);
    const flashedCell = table.locator('.line-block.change-flash > .line-content').first();
    await expect(flashedCell).toBeVisible();
    expect(await flashedCell.evaluate(cell => getComputedStyle(cell).animationName)).toContain('change-flash');

    await page.locator('#diffToggle').click();
    const diff = section.locator('.diff-view');
    await expect(diff).toBeVisible();
    const fallbackTables = diff.locator('table.split-table');
    await expect(fallbackTables.first()).toBeVisible();
    await expect(fallbackTables.locator('colgroup').first()).toBeAttached();
    const changedTableId = await fallbackTables.filter({ hasText: `Choice ${marker}` }).first().getAttribute('data-table-id');
    expect(changedTableId).toBeTruthy();
    const sideWidthSets = await diff.evaluate((element, tableId) => {
      const cells = Array.from(element.querySelectorAll(':scope > .diff-view-cell'));
      return [0, 1].map(side => Array.from(new Set(cells
        .filter((_, index) => index % 2 === side)
        .map(cell => Array.from(cell.querySelectorAll(`table.split-table[data-table-id="${tableId}"] col`))
          .map(col => (col as HTMLElement).style.width).join(','))
        .filter(Boolean))));
    }, changedTableId);
    expect(sideWidthSets[0].length).toBeLessThanOrEqual(1);
    expect(sideWidthSets[1]).toHaveLength(1);
    expect(await fallbackTables.first().evaluate(element => getComputedStyle(element).marginTop)).toBe('0px');
  });

  test('navigating to a deleted table row flashes its deletion marker', async ({ page, request }) => {
    await loadPageWithEvents(page);
    const current = fs.readFileSync(path.join(fixtureDir, 'plan.md'), 'utf-8');
    const deletedRow = '| Key storage | Env var, database | Database | Supports rotation |\n';
    expect(current).toContain(deletedRow);
    await doRoundWithEdit(page, request, fixtureDir, 'plan.md', current.replace(deletedRow, ''));

    const section = await mdSection(page);
    const annotation = section.locator('.native-table-annotation', {
      has: page.locator('.deletion-marker'),
    });
    await expect(annotation).toBeVisible();
    const label = section.locator('.change-nav-label');
    await expect(label).toHaveText(/\/ \d+ changes?/);
    const totalGroups = Number((await label.textContent())?.match(/\/ (\d+) changes?/)?.[1]);
    expect(totalGroups).toBeGreaterThan(0);
    for (let index = 0; index < totalGroups; index++) {
      await page.keyboard.press('n');
      if (await annotation.evaluate(element => element.classList.contains('change-flash'))) break;
    }

    await expect(annotation).toHaveClass(/change-flash/);
    const marker = annotation.locator('.deletion-marker');
    expect(await marker.evaluate(element => getComputedStyle(element).animationName)).toContain('change-flash');
  });

  test('n/N shortcuts are listed in keyboard shortcuts overlay', async ({ page }) => {
    await loadPage(page);

    // Open shortcuts overlay
    await page.keyboard.press('?');
    const overlay = page.locator('.settings-overlay');
    await expect(overlay).toHaveClass(/active/);
    const pane = page.locator('.settings-pane[data-pane="shortcuts"]');

    // Should list n and N shortcuts
    await expect(pane.locator('text=Next change')).toBeVisible();
    await expect(pane.locator('text=Previous change')).toBeVisible();
  });
});

import { expect, type Page, type APIRequestContext, type Locator } from '@playwright/test';
import * as path from 'path';

// Return the on-disk review.json path for the running e2e daemon.
export async function getReviewFilePath(request: APIRequestContext): Promise<string> {
  const config = await request.get('/api/config').then(r => r.json());
  const reviewPath = config.review_path as string;
  expect(reviewPath).toBeTruthy();
  // review_path is the v4 identity folder; review.json lives inside it.
  const filePath = reviewPath.endsWith('review.json')
    ? reviewPath
    : path.join(reviewPath, 'review.json');
  expect(filePath).toMatch(/review\.json$/);
  return filePath;
}
export async function clearAllComments(request: APIRequestContext) {
  const response = await request.delete('/api/comments');
  await expect(response).toBeOK();
}

/** Commit picker rows excluding the virtual working-tree entry. */
export function realCommitItems(page: Page): Locator {
  return page.locator('#commitDropdownList .commit-picker-item:not(.is-virtual)');
}

// Navigate to the root page and wait for loading to complete.
// Ensures diffScope=all is set in the crit-settings cookie so tests see
// all files (branch+untracked), matching the pre-smart-default behavior.
// Merges with any existing cookie values to avoid clobbering other settings.
export async function loadPage(page: Page) {
  const cookies = await page.context().cookies();
  const existing = cookies.find(c => c.name === 'crit-settings');
  let settings: Record<string, unknown> = {};
  if (existing) {
    try { settings = JSON.parse(decodeURIComponent(existing.value)); } catch {}
  }
  if (!settings.diffScope) {
    settings.diffScope = 'all';
    await page.context().addCookies([{
      name: 'crit-settings',
      value: encodeURIComponent(JSON.stringify(settings)),
      domain: 'localhost',
      path: '/',
    }]);
  }
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

// ----- Pierre diff surface -----
//
// Git-mode files render through @pierre/diffs CodeView. Each file is a
// <diffs-container> item: Crit's header (.pierre-file-header) and annotations
// (comment cards, forms, the rendered markdown document) are light-DOM
// children; the code lines live in its open shadow root, which Playwright
// CSS locators pierce. CodeView virtualizes the list, so a file far from
// the viewport has no DOM until it is scrolled to — use the async section
// helpers, which bring the file into view first.

function cssAttr(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/"/g, '\\"');
}

/** A file's Pierre item. Only resolves while the file is mounted. */
export function fileItem(page: Page, filePath: string): Locator {
  return page.locator('diffs-container').filter({
    has: page.locator(`.pierre-file-header[data-file-path="${cssAttr(filePath)}"]`),
  });
}

/** A file's Crit header inside its Pierre item. */
export function fileHeader(page: Page, filePath: string): Locator {
  return page.locator(`.pierre-file-header[data-file-path="${cssAttr(filePath)}"]`);
}

/**
 * Bring a file into view (tree click, or the mobile file picker) and return
 * its item. Leaves the page alone when the file is already on screen.
 */
export async function revealFile(page: Page, filePath: string): Promise<Locator> {
  const item = fileItem(page, filePath);
  const header = fileHeader(page, filePath);
  const onScreen = async () => {
    if (await header.count() === 0) return false;
    return item.evaluate(el => {
      const r = el.getBoundingClientRect();
      return r.height > 0 && r.bottom > 0 && r.top < window.innerHeight;
    }).catch(() => false);
  };
  if (!(await onScreen())) {
    const picker = page.locator('#mobileFilePicker');
    if (await picker.isVisible()) {
      await picker.selectOption(filePath);
    } else {
      const tree = page.locator(`.tree-file[data-tree-path="${cssAttr(filePath)}"]`);
      await expect(tree).toBeVisible({ timeout: 10_000 });
      await tree.click();
    }
  }
  await expect(header).toBeVisible({ timeout: 15_000 });
  return item;
}

// The fixture's usual files, brought into view.
export function mdSection(page: Page) { return revealFile(page, 'plan.md'); }
export function goSection(page: Page) { return revealFile(page, 'server.go'); }
export function jsSection(page: Page) { return revealFile(page, 'handler.js'); }

export type DiffSide = 'new' | 'old';

// Content cell of one diff line. Split puts each side in its own
// code[data-additions|data-deletions]; unified uses one code[data-unified]
// where data-line is the line number on the row's own side.
export function diffLine(item: Locator, line: number, side: DiffSide = 'new'): Locator {
  const n = `[data-line="${line}"]`;
  return side === 'old'
    ? item.locator(`code[data-deletions] [data-content] > ${n}, code[data-unified] [data-content] > ${n}[data-line-type="change-deletion"]`)
    : item.locator(`code[data-additions] [data-content] > ${n}, code[data-unified] [data-content] > ${n}:not([data-line-type="change-deletion"])`);
}

// Line-number cell of one diff line (same side rules as diffLine).
export function diffLineNumber(item: Locator, line: number, side: DiffSide = 'new'): Locator {
  const n = `[data-column-number="${line}"]`;
  return side === 'old'
    ? item.locator(`code[data-deletions] [data-gutter] > ${n}, code[data-unified] [data-gutter] > ${n}[data-line-type="change-deletion"]`)
    : item.locator(`code[data-additions] [data-gutter] > ${n}, code[data-unified] [data-gutter] > ${n}:not([data-line-type="change-deletion"])`);
}

// Pierre pauses pointer events briefly after any scroll, and Playwright's
// actionability scroll counts. Settle, then press at coordinates.
async function pressAt(page: Page, target: Locator) {
  await target.scrollIntoViewIfNeeded();
  await expect.poll(async () => {
    const box = await target.boundingBox();
    if (!box) return false;
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    return true;
  }).toBe(true);
  await page.waitForFunction(() => new Promise(r => requestAnimationFrame(() => requestAnimationFrame(() => r(true)))));
}

/** The gutter "+" for a line: hover the line, then the utility button. */
export async function hoverLine(page: Page, item: Locator, line: number, side: DiffSide = 'new'): Promise<Locator> {
  const content = diffLine(item, line, side).first();
  await expect(content).toBeVisible();
  const button = item.locator('[data-utility-button]');
  await expect(async () => {
    await pressAt(page, content);
    await expect(button).toBeVisible({ timeout: 500 });
  }).toPass({ timeout: 10_000 });
  return button;
}

/** Open a line comment form through the gutter "+" and return the form. */
export async function openLineComment(page: Page, item: Locator, line: number, side: DiffSide = 'new'): Promise<Locator> {
  const button = await hoverLine(page, item, line, side);
  const box = await button.boundingBox();
  expect(box).toBeTruthy();
  await page.mouse.click(box!.x + box!.width / 2, box!.y + box!.height / 2);
  const form = page.locator('#filesContainer .comment-form').last();
  await expect(form.locator('textarea')).toBeVisible();
  return form;
}

/** Drag the gutter "+" from one line to another to open a range form. */
export async function dragLineRange(page: Page, item: Locator, from: number, to: number, side: DiffSide = 'new'): Promise<Locator> {
  const button = await hoverLine(page, item, from, side);
  const start = await button.boundingBox();
  const end = await diffLineNumber(item, to, side).first().boundingBox();
  expect(start && end).toBeTruthy();
  await page.mouse.move(start!.x + start!.width / 2, start!.y + start!.height / 2);
  await page.mouse.down();
  await page.mouse.move(start!.x + start!.width / 2, end!.y + end!.height / 2, { steps: 8 });
  await page.mouse.up();
  const form = page.locator('#filesContainer .comment-form').last();
  await expect(form.locator('textarea')).toBeVisible();
  return form;
}

/** The line Pierre marks selected (keyboard focus / visual range). */
export function selectedLines(page: Page): Locator {
  return page.locator('diffs-container [data-content] > [data-selected-line]');
}

// In git mode, markdown defaults to diff view. Switch plan.md to Document
// view and return the rendered document (.pierre-document).
export async function switchToDocumentView(page: Page): Promise<Locator> {
  await mdSection(page);
  const docBtn = fileHeader(page, 'plan.md').locator('.file-header-toggle .toggle-btn[data-mode="document"]');
  await expect(docBtn).toBeVisible();
  await docBtn.click();
  const doc = page.locator('[id="file-section-plan.md"].pierre-document');
  await expect(doc.locator('.document-wrapper')).toBeVisible();
  return doc;
}

// Perform a mouse drag between two elements (for gutter range selection).
export async function dragBetween(page: Page, startEl: ReturnType<Page['locator']>, endEl: ReturnType<Page['locator']>) {
  const startBox = await startEl.boundingBox();
  const endBox = await endEl.boundingBox();

  expect(startBox).toBeTruthy();
  expect(endBox).toBeTruthy();

  if (startBox && endBox) {
    await page.mouse.move(startBox.x + startBox.width / 2, startBox.y + startBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(endBox.x + endBox.width / 2, endBox.y + endBox.height / 2, { steps: 5 });
    await page.mouse.up();
  }
}

// Click body at (0,0) to clear any focused element.
export async function clearFocus(page: Page) {
  await page.locator('body').click({ position: { x: 0, y: 0 } });
}

export async function focusKbNavByJ(page: Page, presses: number) {
  for (let i = 0; i < presses; i++) {
    await page.keyboard.press('j');
  }
}

async function kbNavIndex(page: Page, locator: ReturnType<Page['locator']>) {
  return locator.evaluate(el => Array.from(document.querySelectorAll('.kb-nav')).indexOf(el));
}

export async function focusKbNavElement(page: Page, locator: ReturnType<Page['locator']>) {
  await clearFocus(page);
  const index = await kbNavIndex(page, locator);
  expect(index).toBeGreaterThanOrEqual(0);
  await focusKbNavByJ(page, index + 1);
}

// Add a comment via API and return the created comment object.
export async function addComment(request: APIRequestContext, path: string, line: number, body: string) {
  const resp = await request.post(`/api/file/comments?path=${encodeURIComponent(path)}`, {
    data: { start_line: line, end_line: line, body },
  });
  expect(resp.ok()).toBeTruthy();
  return resp.json();
}

// Get the markdown file path from the session.
export async function getMdPath(request: APIRequestContext): Promise<string> {
  const session = await (await request.get('/api/session')).json();
  const mdFile = session.files.find((f: { path: string }) => f.path.endsWith('.md'));
  expect(mdFile).toBeTruthy();
  return mdFile.path;
}

// Wait for document scroll height to stop changing (deferred bodies settled,
// SSE-triggered rebuilds complete, etc.). Polls via requestAnimationFrame and
// requires the height to be stable across consecutive frames.
export async function waitForScrollStable(page: Page, { timeout = 5000 } = {}) {
  await page.waitForFunction(() => {
    return new Promise<boolean>(resolve => {
      let lastH = -1;
      let stableCount = 0;
      const check = () => {
        const h = document.documentElement.scrollHeight;
        if (h === lastH && h > 0) {
          if (++stableCount >= 3) return resolve(true);
        } else {
          stableCount = 0;
          lastH = h;
        }
        requestAnimationFrame(check);
      };
      requestAnimationFrame(check);
    });
  }, { timeout });
}

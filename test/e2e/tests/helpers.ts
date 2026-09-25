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

// Content cell of one line. Split diffs put each side in its own
// code[data-additions|data-deletions]; unified uses one code[data-unified]
// where data-line is the line number on the row's own side; a whole-file
// item (files mode) has a single side-less code[data-code].
const FILE_CODE = 'code[data-code]:not([data-additions]):not([data-deletions]):not([data-unified])';

export function diffLine(item: Locator, line: number, side: DiffSide = 'new'): Locator {
  const n = `[data-line="${line}"]`;
  return side === 'old'
    ? item.locator(`code[data-deletions] [data-content] > ${n}, code[data-unified] [data-content] > ${n}[data-line-type="change-deletion"]`)
    : item.locator(`code[data-additions] [data-content] > ${n}, code[data-unified] [data-content] > ${n}:not([data-line-type="change-deletion"]), ${FILE_CODE} [data-content] > ${n}`);
}

// Line-number cell of one line (same side rules as diffLine).
export function diffLineNumber(item: Locator, line: number, side: DiffSide = 'new'): Locator {
  const n = `[data-column-number="${line}"]`;
  return side === 'old'
    ? item.locator(`code[data-deletions] [data-gutter] > ${n}, code[data-unified] [data-gutter] > ${n}[data-line-type="change-deletion"]`)
    : item.locator(`code[data-additions] [data-gutter] > ${n}, code[data-unified] [data-gutter] > ${n}:not([data-line-type="change-deletion"]), ${FILE_CODE} [data-gutter] > ${n}`);
}

// ----- Waiting out Pierre -----
//
// Pierre pauses pointer events on the list for a moment after any scroll
// (it sets an inline pointer-events style under #filesContainer), and
// virtualizes both files and the lines inside long files.

/** The review pane: git mode scrolls #filesContainer (CodeView's scroll root), not the window. */
export function reviewScroller(page: Page): Locator {
  return page.locator('#filesContainer');
}

/** Wait two animation frames (let the virtualizer mount what scrolled into view). */
export async function nextFrames(page: Page) {
  await page.evaluate(() => new Promise(r => requestAnimationFrame(() => requestAnimationFrame(() => r(true)))));
}

/** Wait until Pierre's post-scroll pointer-events pause lifts. */
export async function waitForPointerEvents(page: Page) {
  await expect.poll(() => page.evaluate(() =>
    !document.querySelector('#filesContainer [style*="pointer-events"]'),
  )).toBe(true);
}

// Whether a hit test at the element's centre lands on it (or inside it).
// Uses the element's own root so shadow-DOM lines hit-test correctly.
function hitsCentre(target: Locator, timeout?: number): Promise<boolean> {
  return target.evaluate(el => {
    const r = el.getBoundingClientRect();
    const root = el.getRootNode() as Document | ShadowRoot;
    const hit = root.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
    return !!hit && el.contains(hit);
  }, undefined, { timeout });
}

/** Wait until the element receives pointer hits at its centre (the list is interactive again). */
export async function waitUntilHittable(target: Locator) {
  await expect.poll(() => hitsCentre(target)).toBe(true);
}

/**
 * Click only once the element is actually the hit target at its centre, so
 * the click lands exactly once (Pierre ignores pointer events after a scroll).
 */
export async function clickWhenHittable(page: Page, target: Locator) {
  await expect(async () => {
    await target.scrollIntoViewIfNeeded({ timeout: 1000 });
    expect(await hitsCentre(target, 1000)).toBe(true);
  }).toPass({ timeout: 10_000 });
  const box = await target.boundingBox();
  await page.mouse.click(box!.x + box!.width / 2, box!.y + box!.height / 2);
}

/** Scroll `target` to the centre, wait for pointer events, and tap its centre. */
export async function tapCenter(page: Page, target: Locator) {
  // The row can re-render under us (hover/selection state), so re-resolve
  // until it is on screen and measurable.
  // Pierre can still be settling layout after the scroll (neighbours
  // hydrating), so tap only once the row holds its position for a frame and
  // is what the point hits.
  let box: { x: number; y: number; width: number; height: number } | null = null;
  await expect(async () => {
    await target.evaluate((el) => el.scrollIntoView({ block: 'center' }));
    await waitForPointerEvents(page);
    const first = await target.boundingBox();
    await nextFrames(page);
    box = await target.boundingBox();
    expect(first && box && Math.abs(first.y - box.y) < 1).toBe(true);
    const hits = await target.evaluate((el, p) => {
      const root = el.getRootNode() as Document | ShadowRoot;
      const hit = root.elementFromPoint(p.x, p.y);
      return !!hit && (hit === el || el.contains(hit));
    }, { x: box!.x + box!.width / 2, y: box!.y + box!.height / 2 });
    expect(hits).toBe(true);
  }).toPass({ timeout: 10_000 });
  await page.touchscreen.tap(box!.x + box!.width / 2, box!.y + box!.height / 2);
}

/**
 * Pierre virtualizes lines inside long files: scroll the pane down until the
 * line is rendered, then bring it on screen.
 */
export async function showLine(page: Page, line: Locator) {
  await expect.poll(async () => {
    if (await line.count() > 0) return true;
    await reviewScroller(page).evaluate(el => el.scrollBy(0, 200));
    return false;
  }, { timeout: 15_000 }).toBe(true);
  // The row can re-mount while the list settles; retry until it holds.
  await expect(async () => {
    await line.first().scrollIntoViewIfNeeded({ timeout: 1000 });
    await expect(line.first()).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10_000 });
}

/**
 * Wait until the review pane's scroll offset and height hold still for a few
 * frames (re-layout after an update, virtualized items mounting).
 */
export async function waitForScrollStable(page: Page) {
  await reviewScroller(page).evaluate((el) => new Promise<void>((resolve) => {
    let last = '';
    let stable = 0;
    const check = () => {
      const now = `${el.scrollTop}:${el.scrollHeight}`;
      if (now === last) {
        if (++stable >= 5) return resolve();
      } else {
        stable = 0;
        last = now;
      }
      requestAnimationFrame(check);
    };
    requestAnimationFrame(check);
  }));
}

/**
 * Switch the global Split/Unified toggle and wait until the mounted Pierre
 * items have re-rendered in that layout.
 */
export async function setDiffStyle(page: Page, mode: 'split' | 'unified') {
  const btn = page.locator(`#diffModeToggle .toggle-btn[data-mode="${mode}"]`);
  await expect(btn).toBeVisible();
  await btn.click();
  await expect(btn).toHaveClass(/active/);
  const unified = reviewScroller(page).locator('diffs-container code[data-unified]');
  if (mode === 'unified') await expect(unified.first()).toBeAttached();
  else await expect(unified).toHaveCount(0);
}

// Settle, then press at coordinates: Playwright's actionability scroll
// counts as a scroll for Pierre's pointer-events pause.
async function pressAt(page: Page, target: Locator) {
  await target.scrollIntoViewIfNeeded();
  await expect.poll(async () => {
    const box = await target.boundingBox();
    if (!box) return false;
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    return true;
  }).toBe(true);
  await nextFrames(page);
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

/** plan.md's rendered document (Document view). */
export function mdDocument(page: Page): Locator {
  return page.locator('[id="file-section-plan.md"].pierre-document');
}

// In git mode, markdown defaults to diff view. Switch plan.md to Document
// view and return the rendered document (.pierre-document).
export async function switchToDocumentView(page: Page): Promise<Locator> {
  await mdSection(page);
  const docBtn = fileHeader(page, 'plan.md').locator('.file-header-toggle .toggle-btn[data-mode="document"]');
  await expect(docBtn).toBeVisible();
  await docBtn.click();
  const doc = mdDocument(page);
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

async function kbNavIndex(page: Page, locator: ReturnType<Page['locator']>) {
  return locator.evaluate(el => Array.from(document.querySelectorAll('.kb-nav')).indexOf(el));
}

export async function focusKbNavElement(page: Page, locator: ReturnType<Page['locator']>) {
  await clearFocus(page);
  const index = await kbNavIndex(page, locator);
  expect(index).toBeGreaterThanOrEqual(0);
  for (let i = 0; i <= index; i++) await page.keyboard.press('j');
}

/**
 * Press `key` until `reached()` holds, at most `max` times. With `state`,
 * wait after each press until its value changes, so a slow re-render can't
 * make the loop overshoot the target.
 */
export async function pressUntil(
  page: Page,
  key: string,
  reached: () => Promise<boolean>,
  { max = 200, state }: { max?: number; state?: () => Promise<unknown> } = {},
): Promise<void> {
  for (let i = 0; i < max; i++) {
    if (await reached()) return;
    const before = state && JSON.stringify(await state());
    await page.keyboard.press(key);
    if (state) await expect.poll(async () => JSON.stringify(await state())).not.toBe(before);
  }
  throw new Error(`pressed ${key} ${max} times without reaching the target`);
}

/** File paths in the file tree, in tree order. */
export async function treePaths(page: Page): Promise<string[]> {
  const tree = page.locator('.tree-file[data-tree-path]');
  await expect(tree.first()).toBeVisible();
  return tree.evaluateAll(els => els.map(el => (el as HTMLElement).dataset.treePath!));
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

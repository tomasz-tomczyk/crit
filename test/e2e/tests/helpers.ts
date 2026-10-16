import { test, expect, type Page, type APIRequestContext, type Locator } from '@playwright/test';
import * as path from 'path';
import * as fs from 'fs';
import { stateFilePath } from './state-file';

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

// Rows tinted as an open form's range, as "line:type" in visual order.
// `column` narrows to one split side (or the unified column).
export function selectedRows(item: Locator, column = 'code') {
  return item.evaluate((host, column) => {
    const probe = document.createElement('div');
    probe.style.backgroundColor = 'var(--crit-brand-subtle)';
    document.body.appendChild(probe);
    const tint = getComputedStyle(probe).backgroundColor;
    probe.remove();
    return Array.from(host.shadowRoot!.querySelectorAll(`${column} [data-content] > [data-line]`))
      .filter(el => getComputedStyle(el).backgroundColor === tint)
      .map(el => `${(el as HTMLElement).dataset.line}:${(el as HTMLElement).dataset.lineType}`);
  }, column);
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
// Pierre virtualizes both files and the lines inside long files, so rows
// mount and move while the list settles. (Crit turns off its post-scroll
// pointer-events pause: pointerEventsOnScroll in crit-pierre-view.js.)

/** The review pane: git mode scrolls #filesContainer (CodeView's scroll root), not the window. */
export function reviewScroller(page: Page): Locator {
  return page.locator('#filesContainer');
}

/** Wait two animation frames (let the virtualizer mount what scrolled into view). */
export async function nextFrames(page: Page) {
  await page.evaluate(() => new Promise(r => requestAnimationFrame(() => requestAnimationFrame(() => r(true)))));
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
 * the click lands exactly once (sticky headers and re-mounting rows can cover it).
 * The element must also hold its position for a frame: a sticky header or a
 * neighbour that is still settling can move it between measuring and clicking.
 */
export async function clickWhenHittable(page: Page, target: Locator) {
  let box: { x: number; y: number; width: number; height: number } | null = null;
  await expect(async () => {
    await target.scrollIntoViewIfNeeded({ timeout: 1000 });
    const first = await target.boundingBox();
    await nextFrames(page);
    box = await target.boundingBox();
    expect(!!first && !!box && Math.abs(first.x - box.x) < 1 && Math.abs(first.y - box.y) < 1).toBe(true);
    expect(await hitsCentre(target, 1000)).toBe(true);
  }).toPass({ timeout: 10_000 });
  await page.mouse.click(box!.x + box!.width / 2, box!.y + box!.height / 2);
}

/**
 * Wait until Shiki has highlighted diff `line` (its token spans are in).
 * Shiki runs in a worker after the first plain render, then Pierre re-renders
 * the item: the line's text nodes and the item's separators are replaced, so a
 * selection made, or a click landing, in between is lost.
 */
export async function waitHighlighted(item: Locator, line: number, side: DiffSide = 'new') {
  await expect(diffLine(item, line, side).locator('span[style*="--diffs-token-light"]').first()).toBeAttached();
}

/** Scroll `target` to the centre and tap its centre. */
export async function tapCenter(page: Page, target: Locator) {
  // The row can re-render under us (hover/selection state), so re-resolve
  // until it is on screen and measurable.
  // Pierre can still be settling layout after the scroll (neighbours
  // hydrating), so tap only once the row holds its position for a frame and
  // is what the point hits.
  let box: { x: number; y: number; width: number; height: number } | null = null;
  await expect(async () => {
    await target.evaluate((el) => el.scrollIntoView({ block: 'center' }));
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
 * Wait until the review pane has settled: fonts loaded, every mounted
 * pluggable render target (`.crit-render`, e.g. a Mermaid diagram) has
 * finished, and the pane's scroll offset, height and first item position hold
 * still for a few frames (re-layout after an update, virtualized items
 * mounting).
 *
 * A diagram that mounts as the pane scrolls renders asynchronously: its file
 * keeps the source's height for ~150ms, then grows when the SVG lands, and
 * the virtualizer shifts the items for a frame while it re-measures. Height
 * alone looks stable during that wait, so pending targets count as unsettled.
 */
export async function waitForScrollStable(page: Page) {
  await reviewScroller(page).evaluate(async (el) => {
    await document.fonts.ready;
    await new Promise<void>((resolve, reject) => {
      const deadline = performance.now() + 10_000;
      let last = '';
      let stable = 0;
      const unsettled = () => [...el.querySelectorAll('.crit-render')].filter((t) => {
        const state = t.getAttribute('data-crit-state');
        return state !== 'rendered' && state !== 'error';
      }).length;
      const check = () => {
        const first = el.querySelector('diffs-container');
        const firstTop = first ? first.getBoundingClientRect().top : 0;
        const pending = unsettled();
        const now = `${el.scrollTop}:${el.scrollHeight}:${firstTop}:${pending}`;
        if (now === last && pending === 0) {
          if (++stable >= 5) return resolve();
        } else {
          stable = 0;
          last = now;
        }
        if (performance.now() > deadline) {
          return reject(new Error(`review pane did not settle in 10s (${now})`));
        }
        requestAnimationFrame(check);
      };
      requestAnimationFrame(check);
    });
  });
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

// Settle, then press at coordinates rather than via Playwright's
// actionability scroll.
async function pressAt(page: Page, target: Locator) {
  // A row can intersect the viewport while the sticky file header covers it.
  // Centre it rather than trusting native actionability's visible rectangle.
  // Native scrollIntoView doesn't know about CodeView's virtualization: rows
  // mount as the list scrolls, so one centring can land short. Repeat until
  // the row holds still and takes hits (Pierre also turns pointer events off
  // while scrolling, and hit tests skip it until they're back).
  await expect(async () => {
    await target.evaluate(el => el.scrollIntoView({ block: 'center', inline: 'nearest' }));
    await waitForScrollStable(page);
    const before = await target.boundingBox();
    await nextFrames(page);
    const after = await target.boundingBox();
    expect(before && after && Math.abs(before.y - after.y) < 1).toBe(true);
    expect(await hitsCentre(target, 1000)).toBe(true);
  }).toPass({ timeout: 10_000 });
  const box = await target.boundingBox();
  // Two moves, so a pointer already at this spot still fires a hover.
  await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2 + 2);
  await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2);
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
    // The sticky file header can still cover a row near the top.
    expect(await hitsCentre(button, 1000)).toBe(true);
  }).toPass({ timeout: 10_000 });
  return button;
}

/** Open a line comment form through the gutter "+" and return the form. */
export async function openLineComment(page: Page, item: Locator, line: number, side: DiffSide = 'new'): Promise<Locator> {
  // Layout/highlighting can remount the utility between the hover assertion
  // and measuring it. Retry preparation, then click exactly once.
  // A late re-layout (lazy hydration of a neighbouring file) can shift rows
  // between measuring the "+" and clicking it, so the click hovers — and
  // opens — a different line. A form's data-form-key names its line range
  // and side ("path:start:end:side"), so the opened form is found by key in
  // this file, never by DOM position: other forms (drafts on later lines or
  // files) may already be open. If the wrong line opened, cancel it and try
  // again.
  const forms = item.locator('.comment-form');
  const formKeys = () => forms.evaluateAll(els => els.map(el => (el as HTMLElement).dataset.formKey || ''));
  const isWanted = (key: string) => {
    const parts = key.split(':');
    const keySide = parts.pop();
    const end = Number(parts.pop());
    const start = Number(parts.pop());
    return start === line && end === line && (keySide === 'old') === (side === 'old');
  };
  for (let attempt = 1; ; attempt++) {
    const before = new Set(await formKeys());
    let box: { x: number; y: number; width: number; height: number } | null = null;
    await expect(async () => {
      const button = await hoverLine(page, item, line, side);
      // The "+" must hold its position for a frame, so the click lands where
      // it was measured.
      const first = await button.boundingBox();
      await nextFrames(page);
      box = await button.boundingBox();
      expect(!!first && !!box && Math.abs(first.y - box.y) < 1).toBe(true);
      expect(await hitsCentre(button)).toBe(true);
    }).toPass({ timeout: 10_000 });
    await page.mouse.click(box!.x + box!.width / 2, box!.y + box!.height / 2);
    // The requested line's form (new, or already open), else one that was
    // not open before the click (a wrong-line open).
    let opened = '';
    await expect.poll(async () => {
      const keys = await formKeys();
      opened = keys.find(isWanted) ?? keys.find(k => !before.has(k)) ?? '';
      return opened;
    }, { message: `no comment form opened for line ${line} (${side})` }).not.toBe('');
    const form = item.locator(`.comment-form[data-form-key="${cssAttr(opened)}"]`);
    await expect(form.locator('textarea')).toBeVisible();
    if (isWanted(opened)) {
      if (side === 'new') await expect(form.locator('.comment-form-header')).toHaveText(`Comment on Line ${line}`);
      return form;
    }
    expect(attempt, `openLineComment wanted line ${line} (${side}) but opened form ${opened}`).toBeLessThan(3);
    // Record the retry so a recurring wrong-line open shows in the report
    // instead of passing silently.
    test.info().annotations.push({ type: 'openLineComment retry', description: `wanted line ${line} (${side}), opened ${opened}` });
    await form.getByRole('button', { name: 'Cancel' }).click();
    await expect(form).toHaveCount(0);
  }
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

/**
 * Submit a file-level comment from a file header, then check the new card is
 * where the reader expects it: on screen and above the file's content (first
 * line or rendered document). Counting cards is not enough: a card placed
 * below a long document, or at a wrong scroll offset, still counts.
 *
 * `scope` holds the file-level threads and form (a Pierre item, or a story
 * file group); `content` is the file's first visible line or document.
 */
export async function submitFileLevelComment(
  page: Page,
  opts: { header: Locator; scope: Locator; content: Locator; body: string },
): Promise<Locator> {
  await opts.header.locator('.file-comment-btn').click();
  const textarea = opts.scope.locator('.comment-form textarea');
  await expect(textarea).toBeVisible();
  // A reopened composer must be empty, not the previous submitted form.
  await expect(textarea).toHaveValue('');
  // The composer lines up with the file's existing comments.
  const existing = opts.scope.locator('.comment-card').first();
  if (await existing.count()) {
    const form = await opts.scope.locator('.comment-form').boundingBox();
    const card = await existing.boundingBox();
    expect(Math.abs(form!.x - card!.x), 'form and comments share a left edge').toBeLessThan(2);
    expect(Math.abs(form!.width - card!.width), 'form and comments share a width').toBeLessThan(2);
  }
  await textarea.fill(opts.body);
  await opts.scope.locator('.comment-form .btn-primary').click();
  await expect(opts.scope.locator('.comment-form')).toHaveCount(0);

  const card = opts.scope.locator('.comment-card', { hasText: opts.body });
  await expect(card).toHaveCount(1);
  await expect(card).toBeInViewport();
  await expect.poll(async () => {
    const c = await card.boundingBox();
    const t = await opts.content.boundingBox();
    return !!c && !!t && c.y + c.height <= t.y + 1;
  }, { message: 'file-level card should sit above the file content' }).toBe(true);
  const gaps = await card.evaluate(el => {
    const item = el.closest('.pierre-file-level, .file-comments > *')!;
    // Files-mode source centers its whole panel; its cards fill that panel.
    // Measure the inset against the file, rather than the annotation cell.
    const area = item.closest('diffs-container[data-crit-file-view]') || item.parentElement!;
    const cs = getComputedStyle(area);
    const a = area.getBoundingClientRect();
    const r = el.getBoundingClientRect();
    return {
      // Pierre diffs (not documents, placeholders or files-mode source).
      diff: item.matches('.pierre-file-level') && !item.closest('[data-crit-document], [data-crit-file-view]'),
      // Story chapters: file-level threads sit above the diff, outside Pierre.
      story: item.matches('.file-comments > *'),
      left: r.left - (a.left + parseFloat(cs.paddingLeft)),
      right: (a.right - parseFloat(cs.paddingRight)) - r.right,
      outer: r.left - a.left,
    };
  });
  if (gaps.diff) {
    // Diffs: starts at the code column, like line comments.
    expect(Math.abs(gaps.outer), 'file-level card starts at the code column').toBeLessThan(1);
  } else if (gaps.story) {
    // Story diffs: left-aligned at the container's 16px inset.
    expect(Math.abs(gaps.left), 'story file-level card is left-aligned').toBeLessThan(1);
    expect(gaps.outer, 'story file-level card has a left margin').toBeGreaterThanOrEqual(12);
  } else {
    // Documents and files-mode source: centered, with margin on each side.
    expect(Math.abs(gaps.left - gaps.right), 'file-level card is centered').toBeLessThan(2);
    expect(gaps.outer, 'file-level card has a left margin').toBeGreaterThanOrEqual(12);
  }
  return card;
}

// A registered range can still be detached or have no visible styling. Check
// the mounted text and the underline that distinguishes it from the line tint.
export async function expectPaintedQuote(page: Page) {
  await expect.poll(() => page.evaluate(() => {
    const ranges = [...(CSS.highlights.get('crit-quote') || [])];
    return ranges.length > 0 && ranges.every(range => {
      if (!(range instanceof Range) || !range.startContainer.isConnected) return false;
      const element = range.startContainer.nodeType === Node.ELEMENT_NODE
        ? range.startContainer as Element : range.startContainer.parentElement;
      if (!element) return false;
      const style = getComputedStyle(element, '::highlight(crit-quote)');
      const box = range.getBoundingClientRect();
      return box.width > 0 && box.height > 0 && style.backgroundColor !== 'rgba(0, 0, 0, 0)' &&
        style.textDecorationLine === 'underline' && style.textDecorationThickness === '1.5px';
    });
  })).toBe(true);
}

// Mouse-select part of one diff line's text (inside Pierre's shadow root),
// then press c. Returns the selected text.
//
// Drag points come from the text itself (the gutter and hover "+" overlap the
// start of the line box). Chromium occasionally ends a drag inside a shadow
// root without a selection, so the gesture is retried from a slightly
// different start until the browser reports a non-empty composed selection.
export async function selectInLineAndPressC(page: Page, item: Locator, lineNo: number): Promise<string> {
  const line = diffLine(item, lineNo);
  let attempt = 0;
  let selected = '';
  await expect(async () => {
    attempt++;
    // Scroll via the (narrow) line number: scrolling the wide line element
    // also scrolls the code column sideways, under the sticky gutter.
    await diffLineNumber(item, lineNo).scrollIntoViewIfNeeded({ timeout: 1000 });
    const pts = await line.evaluate((el, offset) => {
      const nodes: Text[] = [];
      const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
      let n: Node | null;
      while ((n = walker.nextNode())) nodes.push(n as Text);
      const text = nodes.map(t => t.data).join('');
      const from = text.search(/\S/) + offset;
      const to = Math.min(text.length - 2, from + 20);
      const at = (idx: number) => {
        let i = idx;
        for (const t of nodes) {
          if (i < t.data.length) {
            const r = document.createRange();
            r.setStart(t, i);
            r.setEnd(t, i + 1);
            const b = r.getBoundingClientRect();
            return { x: b.x + b.width / 2, y: b.y + b.height / 2 };
          }
          i -= t.data.length;
        }
        return null;
      };
      const start = at(from);
      const end = at(to);
      if (!start || !end) return null;
      // Only drag once the document-level hit is the host (nothing covers
      // it) and the start point is this line's text.
      const root = el.getRootNode() as ShadowRoot;
      const hit = root.elementFromPoint(start.x, start.y);
      if (document.elementFromPoint(start.x, start.y) !== root.host || !hit || !el.contains(hit)) return null;
      return { start, end };
    }, 1 + (attempt % 8));
    expect(pts).toBeTruthy();
    await page.mouse.move(pts!.start.x, pts!.start.y);
    await page.mouse.down();
    await page.mouse.move(pts!.end.x, pts!.end.y, { steps: 5 });
    await page.mouse.up();
    selected = await page.evaluate(() => {
      const sel = window.getSelection()!;
      const roots = Array.from(document.querySelectorAll('diffs-container'))
        .map(h => h.shadowRoot).filter((r): r is ShadowRoot => !!r);
      const [r] = sel.getComposedRanges({ shadowRoots: roots });
      if (!r) return '';
      const live = document.createRange();
      live.setStart(r.startContainer, r.startOffset);
      live.setEnd(r.endContainer, r.endOffset);
      return live.toString();
    });
    expect(selected.trim()).not.toBe('');
  }).toPass({ timeout: 15_000 });
  await page.keyboard.press('c');
  return selected;
}

// ----- Shared server-state helpers -----
//
// Prefer these over per-spec copies. Seeding state through the API before
// loadPage() is far cheaper than loading, mutating, then reloading.

/** Current review round reported by the session API. */
export async function currentRound(request: APIRequestContext): Promise<number> {
  const session = await request.get('/api/session').then(r => r.json());
  return session.review_round as number;
}

/** Poll the session API until review_round advances past `previousRound`. */
export async function waitForRound(request: APIRequestContext, previousRound: number) {
  await expect(async () => {
    expect(await currentRound(request)).toBeGreaterThan(previousRound);
  }).toPass();
}

/**
 * POST /api/round-complete and wait for the watcher to finish carry-forward
 * (review_round advances). The server drops a second signal while one is
 * still pending, so never post round-complete twice without waiting — and
 * never load the page straight after posting, or it can render stale files.
 */
export async function roundComplete(request: APIRequestContext) {
  const before = await currentRound(request);
  const resp = await request.post('/api/round-complete');
  await expect(resp).toBeOK();
  await waitForRound(request, before);
}

/** Resolve (or unresolve) a file comment through the API. */
export async function resolveComment(
  request: APIRequestContext, filePath: string, commentId: string, resolved = true,
) {
  const resp = await request.fetch(
    `/api/comment/${commentId}/resolve?path=${encodeURIComponent(filePath)}`,
    { method: 'PUT', data: { resolved } },
  );
  await expect(resp).toBeOK();
}

/** Click a diff-scope toggle button and wait for the session refetch. */
export async function switchScope(page: Page, scope: string) {
  const responsePromise = page.waitForResponse(resp =>
    resp.url().includes('/api/session') && resp.status() === 200,
  );
  await page.click(`#scopeToggle .toggle-btn[data-scope="${scope}"]`);
  await responsePromise;
  await expect(page.locator(`#scopeToggle .toggle-btn[data-scope="${scope}"]`)).toHaveClass(/active/);
}

/**
 * KEY=VALUE pairs the git-mode fixture script wrote to its state file
 * (CRIT_FIXTURE_DIR, FAKE_HOME, ...). Port defaults to CRIT_TEST_PORT.
 */
export function readFixtureState(port: string | number = process.env.CRIT_TEST_PORT || '3123'): Record<string, string> {
  const raw = fs.readFileSync(stateFilePath(port), 'utf8');
  const env: Record<string, string> = {};
  for (const line of raw.trim().split('\n')) {
    const eq = line.indexOf('=');
    if (eq >= 0) env[line.slice(0, eq)] = line.slice(eq + 1);
  }
  return env;
}

/** The daemon's working directory (the fixture dir for files-mode fixtures). */
export async function sessionCwd(request: APIRequestContext): Promise<string> {
  const session = await request.get('/api/session').then(r => r.json());
  expect(session.cwd).toBeTruthy();
  return session.cwd as string;
}

// ----- Transient highlight recording -----
//
// Crit flashes a class on a comment card and removes it again after a short
// timer or animation (comment-nav-highlight: 1s, comment-card-highlight /
// comment-ref-flash: one animation). Polling for the class can miss that
// window on a slow runner. Record every time the class is added instead.

/** Start recording which comment cards gain `className`. Call after each page load. */
export async function recordClassAdditions(page: Page, className: string) {
  await page.evaluate((cls) => {
    const w = window as unknown as { __critClassAdds?: Record<string, string[]> };
    w.__critClassAdds = w.__critClassAdds || {};
    if (w.__critClassAdds[cls]) return;
    const hits: string[] = [];
    w.__critClassAdds[cls] = hits;
    const has = (value: string | null) => (value || '').split(/\s+/).includes(cls);
    new MutationObserver((records) => {
      // A target's value after record i is the old value of its next record,
      // or its current class for the last one.
      records.forEach((r, i) => {
        const el = r.target as HTMLElement;
        const next = records.slice(i + 1).find(n => n.target === el);
        const after = next ? next.oldValue : el.getAttribute('class');
        if (!has(r.oldValue) && has(after)) {
          const card = el.closest('[data-comment-id]') as HTMLElement | null;
          hits.push(el.dataset.commentId || card?.dataset.commentId || '');
        }
      });
    }).observe(document.body, { subtree: true, attributes: true, attributeFilter: ['class'], attributeOldValue: true });
  }, className);
}

/** Comment ids that gained `className` since recordClassAdditions, in order. */
export function classAdditions(page: Page, className: string): Promise<string[]> {
  return page.evaluate((cls) => {
    const w = window as unknown as { __critClassAdds?: Record<string, string[]> };
    return [...(w.__critClassAdds?.[cls] || [])];
  }, className);
}

/** Run `action`, then wait until exactly one more card gained `className` and it is `commentId`. */
export async function expectClassAddedTo(page: Page, className: string, action: () => Promise<unknown>, commentId: string) {
  const before = (await classAdditions(page, className)).length;
  await action();
  await expect.poll(async () => (await classAdditions(page, className)).slice(before)).toEqual([commentId]);
}

// ----- Diff row scanning -----

/**
 * Scroll through a file's diff and record, in visual order, every new-side
 * line number and every separator ("sep") Pierre renders, until line `last`
 * has been seen. Pierre virtualizes lines, so this is the reliable way to see
 * a whole file's row order.
 */
export async function scanDiffRows(page: Page, item: Locator, last: number): Promise<string[]> {
  const seen = new Map<string, number>();
  const rowsOf = () => item.locator('code[data-additions] [data-content], code[data-unified] [data-content]').evaluateAll(codes => {
    const out: { key: string; top: number }[] = [];
    const scroller = document.getElementById('filesContainer')!;
    for (const content of codes) {
      for (const el of Array.from(content.children)) {
        const top = Math.round(el.getBoundingClientRect().top + scroller.scrollTop);
        if (el.hasAttribute('data-separator')) {
          if (el.querySelector('[data-unmodified-lines]')) out.push({ key: `sep@${top}`, top });
        } else if (el.hasAttribute('data-line') && el.getAttribute('data-line-type') !== 'change-deletion') {
          out.push({ key: el.getAttribute('data-line')!, top });
        }
      }
    }
    return out;
  });
  // Start from the file's top (the header re-mounts while the file hydrates).
  await expect(async () => {
    await item.locator('.pierre-file-header').scrollIntoViewIfNeeded({ timeout: 1000 });
  }).toPass({ timeout: 10_000 });
  await waitForScrollStable(page);
  await expect.poll(async () => {
    for (const r of await rowsOf()) seen.set(r.key, r.top);
    if (seen.has(String(last))) return true;
    await reviewScroller(page).evaluate(el => el.scrollBy(0, 200));
    return false;
  }, { timeout: 15_000 }).toBe(true);
  return [...seen.entries()].sort((a, b) => a[1] - b[1]).map(e => e[0].startsWith('sep@') ? 'sep' : e[0]);
}

/** Consecutive line numbers as strings, `from`..`to` inclusive. */
export function lineRange(from: number, to: number): string[] {
  return Array.from({ length: to - from + 1 }, (_, i) => String(from + i));
}

// ----- Markdown document comments -----

/**
 * Open a comment form from the gutter of the n-th line block of a rendered
 * document (hover the block, click its gutter). Returns the form's textarea.
 */
export async function openDocLineComment(page: Page, doc: Locator, index = 0): Promise<Locator> {
  const block = doc.locator('.line-block').nth(index);
  await block.scrollIntoViewIfNeeded();
  await block.hover();
  const gutter = doc.locator('.line-comment-gutter').nth(index);
  await expect(gutter).toBeVisible();
  await gutter.click();
  const textarea = doc.locator('.comment-form textarea').last();
  await expect(textarea).toBeVisible();
  return textarea;
}

// ----- Jump settle -----

/**
 * After a jump to `filePath`, require its header to hold still while Pierre
 * hydrates and re-measures the neighbours: 10 samples ~100ms apart within
 * 2px. Pierre can remount the item as it measures. Each sample first waits
 * for the header to be attached, so a remount is not recorded as NaN; if the
 * header comes back at another position, the 2px check still fails. A header
 * that detaches between that wait and the read restarts the window (at most
 * 5 times). Returns the samples.
 */
export async function expectSettledAt(page: Page, filePath: string): Promise<number[]> {
  const header = fileHeader(page, filePath);
  await expect(header).toBeInViewport();
  const tops: number[] = [];
  let restarts = 0;
  while (tops.length < 10) {
    await expect.poll(async () => (await header.boundingBox()) !== null).toBe(true);
    const box = await header.boundingBox();
    if (!box) {
      restarts++;
      expect(restarts, `${filePath} header kept remounting while settling`).toBeLessThanOrEqual(5);
      tops.length = 0;
      continue;
    }
    tops.push(Math.round(box.y));
    await page.waitForFunction(() => new Promise(r => setTimeout(() => r(true), 100)));
  }
  expect(Math.max(...tops) - Math.min(...tops), `header top samples ${tops.join(',')}`).toBeLessThanOrEqual(2);
  return tops;
}

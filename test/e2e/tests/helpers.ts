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

/** Sidebar file rows — always fully present under file-list virtualization. */
export function treeFiles(page: Page): Locator {
  return page.locator('.tree-file');
}

/** True when #filesContainer has an active FileListVirtualizer controller. */
export async function fileListVirtActive(page: Page): Promise<boolean> {
  return page.evaluate(() => {
    const surface = document.getElementById('filesContainer') as
      (HTMLElement & { _critFileListVirtualizer?: unknown }) | null;
    return !!(surface && surface._critFileListVirtualizer);
  });
}

export async function expectFileListVirt(page: Page) {
  await expect.poll(() => fileListVirtActive(page)).toBe(true);
}

/**
 * Read-only snapshot of file-list virtualizer internals. Keep every e2e read
 * of private virt state behind this one helper so a rename breaks one place;
 * pair it with a DOM assertion rather than using it instead of one.
 */
export async function fileListState(page: Page): Promise<{
  pinned: string[]; mounted: string[]; stick: string | null;
}> {
  return page.evaluate(() => {
    const surface = document.getElementById('filesContainer') as (HTMLElement & {
      _critFileListVirtualizer?: {
        pinnedKeys: Set<string>; mountedKeys: Set<string>; stickKey: () => string | null;
      };
    }) | null;
    const virt = surface && surface._critFileListVirtualizer;
    if (!virt) throw new Error('file-list virtualizer not active');
    return { pinned: [...virt.pinnedKeys], mounted: [...virt.mountedKeys], stick: virt.stickKey() };
  });
}

/**
 * Logical file order from the file-list virtualizer.
 * When requireVirt is true (default), fails if the controller is missing so
 * tests cannot silently fall back to a partial windowed DOM.
 */
export async function reviewFileOrder(
  page: Page,
  { requireVirt = true }: { requireVirt?: boolean } = {},
): Promise<string[]> {
  if (requireVirt) await expectFileListVirt(page);
  return page.evaluate(() => {
    const surface = document.getElementById('filesContainer') as
      (HTMLElement & { _critFileListVirtualizer?: { items: { key: string }[] } }) | null;
    const virt = surface && surface._critFileListVirtualizer;
    if (virt && Array.isArray(virt.items) && virt.items.length > 0) {
      return virt.items.map(item => item.key);
    }
    return Array.from(document.querySelectorAll('.file-section[id]')).map(el =>
      el.id.replace(/^file-section-/, '')
    );
  });
}

/**
 * Mount (via tree click / scrollToFile) and return a file section that is
 * visible in the viewport. File-list virt only keeps a window of nodes — a
 * keep-alive section may exist off-screen; always re-stick when needed.
 */
export async function fileSection(page: Page, filePath: string): Promise<Locator> {
  const section = page.locator(`[id="file-section-${filePath}"]`);
  const tree = page.locator(`.tree-file[data-tree-path="${cssAttr(filePath)}"]`);

  const inViewport = async () => {
    if (await section.count() === 0) return false;
    return section.evaluate(el => {
      const r = el.getBoundingClientRect();
      return r.height > 0 && r.bottom > 0 && r.top < window.innerHeight;
    }).catch(() => false);
  };

  if (!(await inViewport())) {
    // Mobile hides the tree in a drawer; the file picker is its stand-in.
    const picker = page.locator('#mobileFilePicker');
    if (await picker.isVisible()) {
      await picker.selectOption(filePath);
    } else {
      await expect(tree).toBeVisible({ timeout: 10_000 });
      await tree.click();
    }
  }
  await expect(section).toBeVisible({ timeout: 15_000 });
  return section;
}

/** Mount by sidebar basename (e.g. plan.md). */
export async function fileSectionByName(page: Page, fileName: string): Promise<Locator> {
  const tree = page.locator('.tree-file').filter({
    has: page.locator('.tree-file-name', { hasText: new RegExp(`^${escapeRegExp(fileName)}$`) }),
  });
  await expect(tree.first()).toBeVisible({ timeout: 10_000 });
  const filePath = await tree.first().getAttribute('data-tree-path');
  expect(filePath).toBeTruthy();
  return fileSection(page, filePath!);
}

// Scope selectors to the plan.md file section (mounts if off-window).
export async function mdSection(page: Page): Promise<Locator> {
  return fileSectionByName(page, 'plan.md');
}

// Scope selectors to the server.go file section.
export async function goSection(page: Page): Promise<Locator> {
  return fileSection(page, 'server.go');
}

// Scope selectors to the handler.js file section.
export async function jsSection(page: Page): Promise<Locator> {
  return fileSection(page, 'handler.js');
}

// In git mode, markdown defaults to diff view. Click the Document toggle to switch.
export async function switchToDocumentView(page: Page) {
  const section = await mdSection(page);
  await expect(section).toBeVisible();
  const docBtn = section.locator('.file-header-toggle .toggle-btn[data-mode="document"]');
  await expect(docBtn).toBeVisible();
  await docBtn.click();
  await expect(section.locator('.document-wrapper')).toBeVisible();
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
  // Escape is the product clear path; body click alone can leave .focused.
  if (await page.locator('.kb-nav.focused').count() > 0) {
    await page.keyboard.press('Escape');
  }
  await expect.poll(async () => page.locator('.kb-nav.focused').count()).toBe(0);
}

/**
 * Stable identity for the currently focused kb-nav. Row keys and start lines
 * are only unique within a file, so prefix the owning section.
 */
async function focusedKbNavId(page: Page): Promise<string | null> {
  return page.evaluate(() => {
    const el = document.querySelector('.kb-nav.focused') as HTMLElement | null;
    if (!el) return null;
    const section = el.closest('.file-section') as HTMLElement | null;
    const local = el.getAttribute('data-virtual-key')
      || el.getAttribute('data-start-line')
      || el.id
      || `idx:${[...document.querySelectorAll('.kb-nav')].indexOf(el)}`;
    return `${section ? section.id : ''}|${local}`;
  });
}

/** Focused diff row in the row-virtualizer model: owning file + logical index. */
export async function focusedDiffRow(page: Page): Promise<{ file: string; index: number } | null> {
  return page.evaluate(() => {
    const el = document.querySelector('.kb-nav.focused');
    const row = el && el.closest('[data-virtual-row-index]') as HTMLElement | null;
    const section = el && el.closest('.file-section') as HTMLElement | null;
    if (!row || !section) return null;
    return {
      file: section.id.replace(/^file-section-/, ''),
      index: parseInt(row.dataset.virtualRowIndex || '', 10),
    };
  });
}

/**
 * Index of the next (dir=1) or previous (dir=-1) navigable line row after
 * `from` in the file's row model — what j/k must land on. -1 at the boundary.
 */
export async function adjacentLineRowIndex(
  page: Page, filePath: string, from: number, dir: 1 | -1,
): Promise<number> {
  return page.evaluate(({ filePath, from, dir }) => {
    const section = document.getElementById('file-section-' + filePath);
    const surface = section && section.querySelector('.diff-virtual-surface') as
      (HTMLElement & { _critVirtualWindow?: { rows: { kind: string; lineNum?: number }[] } }) | null;
    const rows = surface && surface._critVirtualWindow ? surface._critVirtualWindow.rows : [];
    for (let i = from + dir; i >= 0 && i < rows.length; i += dir) {
      if (rows[i].kind === 'line' && rows[i].lineNum) return i;
    }
    return -1;
  }, { filePath, from, dir });
}

/** Press j/k and require focus to land on exactly the adjacent line row. */
export async function pressAndExpectAdjacentRow(page: Page, key: 'j' | 'k') {
  const before = await focusedDiffRow(page);
  expect(before).toBeTruthy();
  const expected = await adjacentLineRowIndex(page, before!.file, before!.index, key === 'j' ? 1 : -1);
  expect(expected, `no ${key === 'j' ? 'next' : 'previous'} line row in ${before!.file}`).toBeGreaterThanOrEqual(0);
  await page.keyboard.press(key);
  await expect.poll(() => focusedDiffRow(page)).toEqual({ file: before!.file, index: expected });
}

export async function focusKbNavByJ(page: Page, presses: number) {
  for (let i = 0; i < presses; i++) {
    const prev = await focusedKbNavId(page);
    await page.keyboard.press('j');
    // Wait for identity change (not merely .focused count) — scrollToRow is async.
    await expect.poll(async () => {
      const cur = await focusedKbNavId(page);
      if (prev == null) return cur != null;
      return cur != null && cur !== prev;
    }).toBe(true);
  }
}

/**
 * Land keyboard focus on a specific .kb-nav node. Under file-list virt, DOM
 * index is unstable — walk with j until this locator is focused.
 */
export async function focusKbNavElement(page: Page, locator: ReturnType<Page['locator']>) {
  const filePath = await locator.evaluate(el => {
    const section = el.closest('.file-section');
    const id = section && (section as HTMLElement).id;
    return id ? id.replace(/^file-section-/, '') : null;
  }).catch(() => null);
  if (filePath) {
    await fileSection(page, filePath);
  } else {
    await locator.scrollIntoViewIfNeeded();
  }
  await clearFocus(page);
  for (let i = 0; i < 200; i++) {
    const prev = await focusedKbNavId(page);
    await page.keyboard.press('j');
    await expect.poll(async () => {
      const cur = await focusedKbNavId(page);
      if (prev == null) return cur != null;
      return cur != null && cur !== prev;
    }).toBe(true);
    if (await locator.evaluate(el => el.classList.contains('focused')).catch(() => false)) {
      return;
    }
  }
  throw new Error('focusKbNavElement: could not land focus on target');
}

/**
 * Stick a file into view, then press j until keyboard focus lands inside it.
 * Prefer this over raw focusKbNavElement when file-list virt may leave
 * keep-alive neighbors earlier in DOM order.
 */
export async function focusInsideFile(page: Page, filePath: string, { maxPresses = 250 } = {}) {
  await fileSection(page, filePath);
  await clearFocus(page);
  for (let i = 0; i < maxPresses; i++) {
    const prev = await focusedKbNavId(page);
    await page.keyboard.press('j');
    await expect.poll(async () => {
      const cur = await focusedKbNavId(page);
      if (prev == null) return cur != null;
      return cur != null && cur !== prev;
    }).toBe(true);
    const inside = await page.evaluate((path) => {
      const focused = document.querySelector('.kb-nav.focused');
      const section = document.getElementById('file-section-' + path);
      return !!(focused && section && section.contains(focused));
    }, filePath);
    if (inside) return;
  }
  throw new Error('focusInsideFile: could not land focus in ' + filePath);
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

function cssAttr(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/"/g, '\\"');
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

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

/** Logical file order from the file-list virtualizer (or DOM fallback). */
export async function reviewFileOrder(page: Page): Promise<string[]> {
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
 * Mount (via tree click / scrollToFile) and return a file section.
 * File-list virt only keeps a window of .file-section nodes in the DOM.
 */
export async function fileSection(page: Page, filePath: string): Promise<Locator> {
  const section = page.locator(`[id="file-section-${filePath}"]`);
  if (await section.count() === 0) {
    const tree = page.locator(`.tree-file[data-tree-path="${cssAttr(filePath)}"]`);
    await expect(tree).toBeVisible({ timeout: 10_000 });
    await tree.click();
    await expect(section).toBeVisible({ timeout: 15_000 });
  }
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

/** Press j until focus lands inside a mounted file section (file-list virt safe). */
export async function focusInsideFile(page: Page, filePath: string, { maxPresses = 80 } = {}) {
  await clearFocus(page);
  for (let i = 0; i < maxPresses; i++) {
    await page.keyboard.press('j');
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

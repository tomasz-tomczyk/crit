import { test, expect } from '@playwright/test';
import { loadPage, waitForScrollStable, setDiffStyle, reviewScroller } from './helpers';

// Switching diff mode rebuilds every file's diff. It used to collapse the
// document so the browser clamped the scroll to the top of the review instead
// of leaving the reader where they were. Under Pierre, CodeView scrolls
// #filesContainer and must keep the reading position across the re-layout.

type Pinned = { filePath: string; line: string; top: number; scrollTop: number };

// New-side (or context) rows of every mounted file, keyed by file + line.
function newSideRows(): { filePath: string; line: string; el: Element }[] {
  const out: { filePath: string; line: string; el: Element }[] = [];
  for (const host of document.querySelectorAll('#filesContainer diffs-container')) {
    const header = host.querySelector('.pierre-file-header') as HTMLElement | null;
    const root = host.shadowRoot;
    if (!header || !root) continue;
    const rows = root.querySelectorAll(
      'code[data-additions] [data-content] > [data-line], ' +
      'code[data-unified] [data-content] > [data-line]:not([data-line-type="change-deletion"])',
    );
    for (const el of rows) {
      out.push({ filePath: header.dataset.filePath || '', line: el.getAttribute('data-line') || '', el });
    }
  }
  return out;
}

test.describe('Scroll position across view toggles', () => {
  test('switching split/unified keeps the mid-viewport line in place', async ({ page }) => {
    await page.setViewportSize({ width: 1200, height: 400 });
    await loadPage(page);

    const inactive = page.locator('#diffModeToggle .toggle-btn:not(.active)').first();
    await expect(inactive).toBeVisible();
    const mode = await inactive.getAttribute('data-mode') as 'split' | 'unified';
    const pane = reviewScroller(page);
    await expect(page.locator('diffs-container [data-line]').first()).toBeVisible();
    await page.evaluate((src) => {
      (window as unknown as { __newSideRows: unknown }).__newSideRows = (0, eval)(`(${src})`);
    }, newSideRows.toString());

    // Park deep enough that a collapsed rebuild would clamp the offset.
    await pane.evaluate((el) => { el.scrollTop = el.scrollHeight; });
    await waitForScrollStable(page);
    await pane.evaluate((el) => { el.scrollTop = el.scrollHeight * 0.6; });
    await waitForScrollStable(page);

    // Pin the line closest to the vertical center — that's what the rebuild
    // should put back, not just the file header.
    const before = await page.evaluate(() => {
      const rows = (window as unknown as { __newSideRows: typeof newSideRows }).__newSideRows();
      const pane = document.getElementById('filesContainer')!;
      const midY = window.innerHeight / 2;
      let best: Pinned | null = null;
      let bestDist = Infinity;
      for (const r of rows) {
        const rect = r.el.getBoundingClientRect();
        if (rect.height === 0 || rect.bottom <= 0 || rect.top >= window.innerHeight) continue;
        const dist = Math.abs((rect.top + rect.bottom) / 2 - midY);
        if (dist < bestDist) {
          bestDist = dist;
          best = { filePath: r.filePath, line: r.line, top: rect.top, scrollTop: pane.scrollTop };
        }
      }
      return best;
    });
    expect(before).toBeTruthy();
    expect(before!.scrollTop).toBeGreaterThan(100);

    await setDiffStyle(page, mode);

    // The same line must be mounted again, near where it was.
    await expect.poll(async () => {
      await waitForScrollStable(page);
      return page.evaluate((a: Pinned) => {
        const rows = (window as unknown as { __newSideRows: typeof newSideRows }).__newSideRows();
        const pane = document.getElementById('filesContainer')!;
        const hit = rows.find((r) => r.filePath === a.filePath && r.line === a.line);
        if (!hit || pane.scrollTop <= 100) return null;
        return Math.round(Math.abs(hit.el.getBoundingClientRect().top - a.top));
      }, before!);
    }, { timeout: 10_000 }).toBeLessThan(50);
  });
});

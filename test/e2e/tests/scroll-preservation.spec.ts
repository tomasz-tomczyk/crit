import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

// Switching diff mode rebuilds every file section. Deferred bodies come back
// empty, so the document used to collapse and the browser clamped the scroll to
// the top of the review instead of leaving the reader where they were.
test.describe('Scroll position across view toggles', () => {
  test('switching split/unified keeps the mid-viewport line in place', async ({ page }) => {
    await page.setViewportSize({ width: 1200, height: 400 });
    await loadPage(page);

    const toggle = page.locator('#diffModeToggle .toggle-btn:not(.active)').first();
    await expect(toggle).toBeVisible();

    // Walk to the bottom so every body mounts and the height is real, then park
    // deep enough that a collapsed rebuild would be shorter than the offset.
    await page.evaluate(() => window.scrollTo({ top: document.documentElement.scrollHeight, behavior: 'instant' }));
    await page.waitForTimeout(500);
    await page.evaluate(() => window.scrollTo({ top: document.documentElement.scrollHeight * 0.6, behavior: 'instant' }));
    await page.waitForTimeout(500);

    // Pin the line closest to the vertical center — that's what the rebuild
    // should put back, not just the file-section header.
    const before = await page.evaluate(() => {
      const midY = window.innerHeight / 2;
      const nodes = document.querySelectorAll(
        '#filesContainer .diff-line[data-diff-line-num], #filesContainer .diff-split-side[data-diff-line-num]'
      );
      let best: { filePath: string; lineNum: string; side: string; top: number; scrollY: number } | null = null;
      let bestDist = Infinity;
      for (const el of nodes) {
        const rect = el.getBoundingClientRect();
        if (rect.bottom <= 0 || rect.top >= window.innerHeight) continue;
        const dist = Math.abs((rect.top + rect.bottom) / 2 - midY);
        const html = el as HTMLElement;
        const side = html.dataset.diffSide || '';
        const preferNew = side === '';
        const bestPreferNew = !!(best && best.side === '');
        if (dist > bestDist + 0.5) continue;
        if (Math.abs(dist - bestDist) <= 0.5 && best && !(preferNew && !bestPreferNew)) continue;
        bestDist = dist;
        best = {
          filePath: html.dataset.diffFilePath || '',
          lineNum: html.dataset.diffLineNum || '',
          side,
          top: rect.top,
          scrollY: window.scrollY,
        };
      }
      return best;
    });
    expect(before).toBeTruthy();
    expect(before!.scrollY).toBeGreaterThan(100);

    await toggle.click();
    await page.waitForTimeout(1000);

    const after = await page.evaluate((a: { filePath: string; lineNum: string; side: string }) => {
      const base =
        `#filesContainer [data-diff-file-path="${CSS.escape(a.filePath)}"]` +
        `[data-diff-line-num="${a.lineNum}"]`;
      const el = (document.querySelector(base + `[data-diff-side="${CSS.escape(a.side)}"]`) ||
        document.querySelector(base)) as HTMLElement | null;
      return { top: el ? el.getBoundingClientRect().top : null, scrollY: window.scrollY };
    }, before!);
    expect(after.scrollY).toBeGreaterThan(100);
    expect(after.top).not.toBeNull();
    expect(Math.abs((after.top ?? 0) - before!.top)).toBeLessThan(50);
  });
});

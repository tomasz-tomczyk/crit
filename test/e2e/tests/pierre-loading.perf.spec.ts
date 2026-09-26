import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

test.beforeEach(async ({ request }) => { await clearAllComments(request); });

test('a stationary newly mounted lazy viewport shows loading annotations and then hydrates', async ({ page }) => {
  await loadPage(page);
  let releaseLoads: () => void = () => {};
  const loadsAllowed = new Promise<void>(resolve => { releaseLoads = resolve; });
  await page.route(/\/api\/file(?:\/diff|\/comments)?\?/, async route => {
    await loadsAllowed;
    await route.continue();
  });
  try {
    await page.locator('#filesContainer').evaluate(el => { el.scrollTop = 24000; });
    const loading = page.locator('#filesContainer diffs-container[data-crit-stub] .pierre-loading');
    await expect(loading.first()).toBeVisible();
    await expect(loading.first()).toHaveText('Loading diff…');
    const geometry = await loading.first().evaluate(el => {
      const host = el.closest('diffs-container');
      const row = host?.shadowRoot?.querySelector('[data-line]');
      return { rowHeight: row?.getBoundingClientRect().height, rowVisibility: row && getComputedStyle(row).visibility };
    });
    expect(geometry.rowHeight).toBeGreaterThan(0);
    expect(geometry.rowVisibility).toBe('hidden');
    releaseLoads();
    // No second scroll event: post-render mounting must schedule hydration.
    await expect.poll(() => page.locator('#filesContainer diffs-container:not([data-crit-stub]) [data-line]').evaluateAll(lines => {
      const viewport = document.getElementById('filesContainer')!.getBoundingClientRect();
      return lines.some(line => {
        const rect = line.getBoundingClientRect();
        return rect.bottom > viewport.top && rect.top < viewport.bottom && getComputedStyle(line).visibility === 'visible';
      });
    })).toBe(true);
  } finally { releaseLoads(); }
});

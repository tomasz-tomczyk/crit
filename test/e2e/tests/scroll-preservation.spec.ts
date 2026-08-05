import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

// Switching diff mode rebuilds every file section. Deferred bodies come back
// empty, so the document used to collapse and the browser clamped the scroll to
// the top of the review instead of leaving the reader where they were.
test.describe('Scroll position across view toggles', () => {
  test('switching split/unified keeps the topmost visible file in place', async ({ page }) => {
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

    // The rebuild pins the topmost file section still touching the viewport.
    const before = await page.evaluate(() => {
      const sections = document.querySelectorAll('#filesContainer .file-section[id]');
      for (const section of sections) {
        const rect = section.getBoundingClientRect();
        if (rect.bottom > 0) return { id: section.id, top: rect.top, scrollY: window.scrollY };
      }
      return null;
    });
    expect(before).toBeTruthy();
    expect(before!.scrollY).toBeGreaterThan(100);

    await toggle.click();
    await page.waitForTimeout(1000);

    const after = await page.evaluate((id: string) => {
      const section = document.getElementById(id);
      return { top: section ? section.getBoundingClientRect().top : null, scrollY: window.scrollY };
    }, before!.id);
    expect(after.scrollY).toBeGreaterThan(100);
    expect(Math.abs((after.top ?? 0) - before!.top)).toBeLessThan(50);
  });
});

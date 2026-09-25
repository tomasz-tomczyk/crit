import { test, expect } from '@playwright/test';
import { loadPage, clearAllComments, treeFiles, reviewFileOrder, fileSection, expectFileListVirt } from './helpers';

test.describe('Lazy loading', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('session API does not mark files as lazy when under threshold', async ({ request }) => {
    const res = await request.get('/api/session');
    const session = await res.json();

    // Git-mode fixture has <25 files — none should be lazy
    expect(session.files.length).toBeGreaterThan(0);
    expect(session.files.length).toBeLessThan(25);

    for (const file of session.files) {
      expect(file.lazy).toBeFalsy();
    }
  });

  test('every review file can mount without a loading spinner', async ({ page }) => {
    await loadPage(page);
    await expectFileListVirt(page);

    const order = await reviewFileOrder(page);
    expect(order.length).toBe(await treeFiles(page).count());
    expect(order.length).toBeGreaterThan(0);
    const session = await (await page.request.get('/api/session')).json();
    const sessionPaths = new Set(session.files.map((f: { path: string }) => f.path));
    // Virt order is the review list; every key must be a session file (session may
    // also list paths filtered out of the current tree/scope).
    expect(order.every(p => sessionPaths.has(p))).toBe(true);

    for (const path of order) {
      const section = await fileSection(page, path);
      await expect(section).toBeVisible();
      // .file-section-loading is a class on the section, not a descendant.
      await expect(section).not.toHaveClass(/file-section-loading/);
      // Open sections must mount a real body; collapsed ones (deleted files
      // start collapsed) legitimately keep it deferred.
      await expect.poll(() => section.evaluate(el =>
        !(el as HTMLDetailsElement).open ||
        !!el.querySelector(':scope > .file-body:not([data-body-deferred])')
      )).toBe(true);
    }
    await expect(page.locator('.file-section-loading')).toHaveCount(0);
  });
});

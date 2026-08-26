import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, addComment } from './helpers';

// Rebuilding every file section hands back deferred (empty) bodies, so the
// document collapses shorter than the current offset and the browser clamps the
// scroll to the top. Replying and deleting used to trigger that via the
// comments-changed SSE event.
test.describe('Scroll position across comment updates', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('submitting a reply keeps the thread where it was on screen', async ({ page, request }) => {
    // Small viewport so most file bodies stay deferred while we read one thread.
    await page.setViewportSize({ width: 1200, height: 400 });

    const session = await (await request.get('/api/session')).json();
    const lastFile = session.files[session.files.length - 1].path as string;
    await addComment(request, lastFile, 5, 'Reply to me');

    await loadPage(page);

    const card = page.locator('.comment-card').first();
    await expect(card).toBeVisible();

    // Park the thread mid-viewport and let deferred bodies settle so the
    // measurement below isn't racing the mount observer.
    await card.evaluate(el => el.scrollIntoView({ block: 'center', behavior: 'instant' }));
    await page.waitForTimeout(500);
    const before = await card.evaluate(el => ({
      scrollY: window.scrollY,
      top: el.getBoundingClientRect().top,
    }));
    expect(before.scrollY).toBeGreaterThan(100);

    await card.locator('.reply-input').fill('replying without losing my place');
    await card.locator('.reply-form-buttons .btn-primary').click();

    await expect(card.locator('.comment-reply')).toHaveCount(1);
    // The jump happened when the SSE event landed, after the local re-render.
    await page.waitForTimeout(1500);

    const after = await card.evaluate(el => ({
      scrollY: window.scrollY,
      top: el.getBoundingClientRect().top,
    }));
    expect(after.scrollY).toBeGreaterThan(100);
    expect(Math.abs(after.top - before.top)).toBeLessThan(50);
  });

  // Deleting broadcasts the same event, so it lost the reader's place too.
  test('deleting a comment keeps the surrounding file where it was on screen', async ({ page, request }) => {
    await page.setViewportSize({ width: 1200, height: 400 });

    const session = await (await request.get('/api/session')).json();
    const lastFile = session.files[session.files.length - 1].path as string;
    await addComment(request, lastFile, 5, 'Delete me');

    await loadPage(page);

    const card = page.locator('.comment-card').first();
    const section = page.locator('.file-section').last();
    await expect(card).toBeVisible();

    await card.evaluate(el => el.scrollIntoView({ block: 'center', behavior: 'instant' }));
    await page.waitForTimeout(500);
    // The card itself disappears, so anchor the measurement to its file section.
    const before = await section.evaluate(el => ({
      scrollY: window.scrollY,
      top: el.getBoundingClientRect().top,
    }));
    expect(before.scrollY).toBeGreaterThan(100);

    await card.locator('.comment-actions .delete-btn').click();
    await expect(page.locator('.comment-card')).toHaveCount(0);
    await page.waitForTimeout(1500);

    const after = await section.evaluate(el => ({
      scrollY: window.scrollY,
      top: el.getBoundingClientRect().top,
    }));
    expect(after.scrollY).toBeGreaterThan(100);
    expect(Math.abs(after.top - before.top)).toBeLessThan(50);
  });

  // Hide-resolved is CSS for cards plus a highlight sync — it must not wipe
  // #filesContainer. A full rebuild was both unnecessary and the scroll bug.
  test('toggling hide-resolved preserves file section DOM nodes', async ({ page }) => {
    await page.setViewportSize({ width: 1200, height: 400 });
    await loadPage(page);

    await page.evaluate(() => window.scrollTo({ top: 2000, behavior: 'instant' }));
    await page.waitForTimeout(500);

    const before = await page.evaluate(() => {
      const sections = [...document.querySelectorAll('#filesContainer .file-section[id]')];
      const top = sections.find(s => s.getBoundingClientRect().bottom > 0);
      if (!top) return null;
      // Stamp the live node so we can tell a rebuild from an in-place update.
      (top as HTMLElement).dataset.critPreserveProbe = '1';
      return { id: top.id, top: top.getBoundingClientRect().top, scrollY: window.scrollY };
    });
    expect(before).toBeTruthy();
    expect(before!.scrollY).toBeGreaterThan(100);

    await page.keyboard.press('h');
    await page.waitForTimeout(300);

    const after = await page.evaluate((id: string) => {
      const section = document.getElementById(id);
      return {
        sameNode: !!(section && section.dataset.critPreserveProbe === '1'),
        top: section ? section.getBoundingClientRect().top : null,
        scrollY: window.scrollY,
      };
    }, before!.id);
    expect(after.sameNode).toBe(true);
    expect(after.scrollY).toBeGreaterThan(100);
    expect(Math.abs((after.top ?? 0) - before!.top)).toBeLessThan(5);
  });
});

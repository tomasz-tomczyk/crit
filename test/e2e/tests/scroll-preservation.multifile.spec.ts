import { test, expect } from '@playwright/test';
import { readFileSync, writeFileSync } from 'node:fs';
import { clearAllComments, loadPage, addComment, getReviewFilePath, waitForScrollStable, fileSection } from './helpers';

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

    await fileSection(page, lastFile);
    const card = page.locator('.comment-card').first();
    await expect(card).toBeVisible();

    // Park the thread mid-viewport and let deferred bodies settle so the
    // measurement below isn't racing the mount observer.
    await card.evaluate(el => el.scrollIntoView({ block: 'center', behavior: 'instant' }));
    await waitForScrollStable(page);
    const before = await card.evaluate(el => ({
      scrollY: window.scrollY,
      top: el.getBoundingClientRect().top,
    }));
    expect(before.scrollY).toBeGreaterThan(100);

    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('replying without losing my place');
    await card.locator('.reply-form-buttons .btn-primary').click();

    await expect(card.locator('.comment-reply')).toHaveCount(1);
    // The jump happened when the SSE event landed, after the local re-render.
    await waitForScrollStable(page);

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

    // Pin the review last file — under file-list virt `.file-section).last()` is only the window edge.
    const section = await fileSection(page, lastFile);
    const card = section.locator('.comment-card').first();
    await expect(card).toBeVisible();

    await card.evaluate(el => el.scrollIntoView({ block: 'center', behavior: 'instant' }));
    await waitForScrollStable(page);
    // The card itself disappears, so anchor the measurement to its file section.
    const before = await section.evaluate(el => ({
      scrollY: window.scrollY,
      top: el.getBoundingClientRect().top,
    }));
    expect(before.scrollY).toBeGreaterThan(100);

    await card.locator('.comment-actions .delete-btn').click();
    await expect(page.locator('.comment-card')).toHaveCount(0);
    await waitForScrollStable(page);

    const after = await section.evaluate(el => ({
      scrollY: window.scrollY,
      top: el.getBoundingClientRect().top,
    }));
    expect(after.scrollY).toBeGreaterThan(100);
    expect(Math.abs(after.top - before.top)).toBeLessThan(50);
  });

  // Round-complete (the agent re-arming after Finish Review) re-fetches the
  // session and rebuilds every section, so it threw the reader back to the top
  // once per round — the same deferred-body collapse as above.
  test('round-complete keeps the file being read where it was on screen', async ({ page, request }) => {
    await page.setViewportSize({ width: 1200, height: 400 });

    const session = await (await request.get('/api/session')).json();
    const lastFile = session.files[session.files.length - 1].path as string;
    await addComment(request, lastFile, 5, 'Round me');

    await loadPage(page);

    const section = await fileSection(page, lastFile);
    await section.evaluate(el => el.scrollIntoView({ block: 'start', behavior: 'instant' }));
    await waitForScrollStable(page);
    const before = await section.evaluate(el => ({
      scrollY: window.scrollY,
      top: el.getBoundingClientRect().top,
    }));
    expect(before.scrollY).toBeGreaterThan(100);

    await page.locator('#finishBtn').click();
    const overlay = page.locator('#waitingOverlay');
    await expect(overlay).toHaveClass(/active/);

    await request.post('/api/round-complete');
    await expect(overlay).not.toHaveClass(/active/, { timeout: 5_000 });
    await waitForScrollStable(page);

    // The rebuild replaces the section node; the locator re-resolves to the new one.
    const after = await section.evaluate(el => ({
      scrollY: window.scrollY,
      top: el.getBoundingClientRect().top,
    }));
    expect(after.scrollY).toBeGreaterThan(100);
    expect(Math.abs(after.top - before.top)).toBeLessThan(50);
  });

  test('round-complete keeps a conversation reader above offscreen files', async ({ page, request }) => {
    await page.setViewportSize({ width: 1200, height: 400 });
    const response = await request.post('/api/comments', {
      data: { body: 'Conversation\n\n' + 'A paragraph\n\n'.repeat(30) },
    });
    expect(response.ok()).toBeTruthy();
    const reviewPath = await getReviewFilePath(request);
    await loadPage(page);
    await page.evaluate(() => window.scrollTo({ top: 0, behavior: 'instant' }));
    await waitForScrollStable(page);
    // The whole file list must sit below the conversation — independent of
    // which file sections the list window happens to mount.
    expect(await page.evaluate(() =>
      document.getElementById('filesContainer')!.getBoundingClientRect().top >= window.innerHeight
    )).toBe(true);

    await page.locator('#finishBtn').click();
    const overlay = page.locator('#waitingOverlay');
    await expect(overlay).toHaveClass(/active/);

    // An agent can add a review-level comment on disk immediately before
    // re-arming. Round-complete imports it even before the file watcher polls.
    const review = JSON.parse(readFileSync(reviewPath, 'utf8'));
    review.review_comments.push({
      ...review.review_comments[0],
      id: 'round-complete-conversation-reply',
      body: 'New paragraph\n\n'.repeat(20),
    });
    writeFileSync(reviewPath, JSON.stringify(review));
    expect((await request.post('/api/round-complete')).ok()).toBeTruthy();
    await expect(overlay).not.toHaveClass(/active/);
    await expect(page.locator('#reviewConversation .comment-card')).toHaveCount(2);
    await waitForScrollStable(page);

    // A file below the viewport must not become the scroll anchor when the
    // conversation grows: the reader is still at the top of the conversation.
    expect(await page.evaluate(() => window.scrollY)).toBe(0);
  });

  // Hide-resolved is CSS for cards plus a highlight sync — it must not wipe
  // #filesContainer. A full rebuild was both unnecessary and the scroll bug.
  test('toggling hide-resolved preserves file section DOM nodes', async ({ page }) => {
    await page.setViewportSize({ width: 1200, height: 400 });
    await loadPage(page);

    await page.evaluate(() => window.scrollTo({ top: 2000, behavior: 'instant' }));
    await waitForScrollStable(page);

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

    const wasHidden = await page.evaluate(() => document.body.classList.contains('hide-resolved'));
    await page.keyboard.press('h');
    // Hide-resolved is a CSS class toggle on <body> — wait for it to flip.
    await expect.poll(
      () => page.evaluate(() => document.body.classList.contains('hide-resolved')),
    ).toBe(!wasHidden);

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

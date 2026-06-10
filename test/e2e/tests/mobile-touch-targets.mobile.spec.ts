import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, getMdPath } from './helpers';

// F2: touch target sizing + iOS textarea zoom prevention.
// Under @media (pointer: coarse) all interactive icon buttons reach the
// WCAG 2.5.5 / Apple HIG minimum 44x44 px, reply actions are always
// visible (no hover), and comment textareas use 16px font-size to
// suppress iOS Safari focus-zoom.
test.describe('Mobile touch targets (F2)', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('header icon buttons meet 44x44 target', async ({ page }) => {
    // .theme-toggle is the class applied to the System/Light/Dark pill
    // buttons in the header. They're icon-only and the most-tapped header
    // controls on mobile.
    const themeToggle = page.locator('.theme-toggle').first();
    await expect(themeToggle).toBeVisible();
    const box = await themeToggle.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.width).toBeGreaterThanOrEqual(44);
    expect(box!.height).toBeGreaterThanOrEqual(44);
  });

  test('comment-count badge button meets 44x44 target', async ({ page }) => {
    const btn = page.locator('.comment-count-btn').first();
    await expect(btn).toBeVisible();
    const box = await btn.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.width).toBeGreaterThanOrEqual(44);
    expect(box!.height).toBeGreaterThanOrEqual(44);
  });

  test('comment-nav buttons meet 44x44 target when comments exist', async ({ page, request }) => {
    // comment-nav-btn only renders when at least one comment exists.
    // Post one via API so the buttons appear.
    const mdPath = await getMdPath(request);
    const resp = await request.post(`/api/file/comments?path=${encodeURIComponent(mdPath)}`, {
      data: { start_line: 1, end_line: 1, body: 'nav target test' },
    });
    expect(resp.ok()).toBeTruthy();
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    const nav = page.locator('.comment-nav-btn').first();
    await expect(nav).toBeVisible();
    const box = await nav.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.width).toBeGreaterThanOrEqual(44);
    expect(box!.height).toBeGreaterThanOrEqual(44);
  });

  test('comment textarea uses font-size >= 16px (iOS zoom prevention)', async ({ page }) => {
    // Open a comment form to expose its textarea. The mobile file picker
    // gives us a known file; we tap a line gutter to open a form.
    // Actually simpler: post a comment so a reply input renders, OR open
    // the review-conversation form. Easiest is to tap a markdown line.
    const fileSec = page.locator('.file-section').filter({ hasText: '.md' }).first();
    await expect(fileSec).toBeVisible();
    // Switch to document view so .line-comment-gutter is the affordance.
    const docBtn = fileSec.locator('.file-header-toggle .toggle-btn[data-mode="document"]');
    if (await docBtn.isVisible()) await docBtn.click();
    const gutter = fileSec.locator('.line-comment-gutter').first();
    await expect(gutter).toBeVisible();
    await gutter.tap();

    const textarea = page.locator('.comment-form textarea').first();
    await expect(textarea).toBeVisible();
    const fontSize = await textarea.evaluate((el: Element) =>
      parseFloat(getComputedStyle(el).fontSize)
    );
    expect(fontSize).toBeGreaterThanOrEqual(16);
  });

  test('reply-actions are visible on touch without hover', async ({ page, request }) => {
    // Reply actions are hover-revealed on desktop. On touch they must be
    // always visible. Post a comment and a reply so .reply-actions renders.
    const mdPath = await getMdPath(request);
    const commentResp = await request.post(`/api/file/comments?path=${encodeURIComponent(mdPath)}`, {
      data: { start_line: 1, end_line: 1, body: 'test comment' },
    });
    expect(commentResp.ok()).toBeTruthy();
    const comment = await commentResp.json();
    const replyResp = await request.post(
      `/api/comment/${comment.id}/replies?path=${encodeURIComponent(mdPath)}`,
      { data: { body: 'test reply' } },
    );
    expect(replyResp.ok()).toBeTruthy();

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    const replyActions = page.locator('.reply-actions').first();
    await expect(replyActions).toBeVisible();
  });

  test('file-header copy-path button meets 44x44 target', async ({ page }) => {
    const btn = page.locator('.file-header-copy-path').first();
    await expect(btn).toBeVisible();
    const box = await btn.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.width).toBeGreaterThanOrEqual(44);
    expect(box!.height).toBeGreaterThanOrEqual(44);
  });

  test('reply-actions buttons meet 44x44 target', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const commentResp = await request.post(`/api/file/comments?path=${encodeURIComponent(mdPath)}`, {
      data: { start_line: 1, end_line: 1, body: 'reply-actions size test' },
    });
    expect(commentResp.ok()).toBeTruthy();
    const comment = await commentResp.json();
    const replyResp = await request.post(
      `/api/comment/${comment.id}/replies?path=${encodeURIComponent(mdPath)}`,
      { data: { body: 'reply' } },
    );
    expect(replyResp.ok()).toBeTruthy();

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    const btn = page.locator('.reply-actions button').first();
    await expect(btn).toBeVisible();
    const box = await btn.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.height).toBeGreaterThanOrEqual(44);
  });
});

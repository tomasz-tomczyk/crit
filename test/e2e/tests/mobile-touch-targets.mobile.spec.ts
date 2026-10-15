import { test, expect, type Locator } from '@playwright/test';
import {
  clearAllComments, loadPage, getMdPath, mdSection, switchToDocumentView, tapCenter,
} from './helpers';

async function expectTouchTargets(targets: Locator, expectedCount?: number) {
  if (expectedCount !== undefined) {
    await expect(targets).toHaveCount(expectedCount);
  }
  await expect(targets.first()).toBeVisible();
  const boxes = await targets.evaluateAll(elements => elements.map((element) => {
    const rect = element.getBoundingClientRect();
    return { width: rect.width, height: rect.height };
  }));
  expect(boxes.length).toBeGreaterThan(0);
  for (const box of boxes) {
    expect(box.width).toBeGreaterThanOrEqual(44);
    expect(box.height).toBeGreaterThanOrEqual(44);
  }
}

// F2: touch target sizing + iOS textarea zoom prevention.
// Under @media (pointer: coarse) all interactive icon buttons reach the
// WCAG 2.5.5 / Apple HIG minimum 44x44 px, reply actions are always
// visible (no hover), and comment textareas use 16px font-size to
// suppress iOS Safari focus-zoom.
test.describe('Mobile touch targets (F2)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('header and file icon buttons meet 44x44 targets; comment textarea is >= 16px', async ({ page }) => {
    await loadPage(page);
    // The TOC toggle is intentionally hidden below 600px; settings remains
    // the visible header icon control at this project's 375px viewport.
    await expectTouchTargets(page.locator('#settingsToggle'), 1);
    await expectTouchTargets(page.locator('.comment-count-btn'), 1);
    await expectTouchTargets(page.locator('.file-header-copy-path'));

    // Open a comment form to expose its textarea: tap a markdown line's
    // comment gutter in Document view (a line below the sticky file header /
    // picker bar, centred on screen).
    const doc = await switchToDocumentView(page);
    const gutter = doc.locator('.line-comment-gutter').nth(2);
    await expect(gutter).toBeVisible();
    await tapCenter(page, gutter);
    const textarea = page.locator('.comment-form textarea').first();
    await expect(textarea).toBeVisible();
    // iOS zoom prevention.
    await expect.poll(() => textarea.evaluate(el => parseFloat(getComputedStyle(el).fontSize)))
      .toBeGreaterThanOrEqual(16);
  });

  test('with a comment: nav buttons and always-visible reply actions meet 44x44 targets', async ({ page, request }) => {
    // comment-nav-btn only renders when at least one comment exists; reply
    // actions need a reply.
    const mdPath = await getMdPath(request);
    const commentResp = await request.post(`/api/file/comments?path=${encodeURIComponent(mdPath)}`, {
      data: { start_line: 1, end_line: 1, body: 'touch target test' },
    });
    expect(commentResp.ok()).toBeTruthy();
    const comment = await commentResp.json();
    const replyResp = await request.post(
      `/api/comment/${comment.id}/replies?path=${encodeURIComponent(mdPath)}`,
      { data: { body: 'reply' } },
    );
    expect(replyResp.ok()).toBeTruthy();
    await loadPage(page);

    await expectTouchTargets(page.locator('.comment-nav-btn'), 2);

    await mdSection(page);
    const replyActions = page.locator('.reply-actions');
    await expect(replyActions).toBeVisible();
    await expect(replyActions).toHaveCSS('opacity', '1');
    await expectTouchTargets(replyActions.locator('button'), 2);
  });
});

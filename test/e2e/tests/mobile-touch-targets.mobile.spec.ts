import { test, expect, type Locator } from '@playwright/test';
import { clearAllComments, loadPage, getMdPath, fileSectionByName } from './helpers';

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
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('header and file icon buttons meet 44x44 targets', async ({ page }) => {
    // The TOC toggle is intentionally hidden below 600px; settings remains
    // the visible header icon control at this project's 375px viewport.
    await expectTouchTargets(page.locator('#settingsToggle'), 1);
    await expectTouchTargets(page.locator('.comment-count-btn'), 1);
    await expectTouchTargets(page.locator('.file-header-copy-path'));
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

    await expectTouchTargets(page.locator('.comment-nav-btn'), 2);
  });

  test('comment textarea uses font-size >= 16px (iOS zoom prevention)', async ({ page }) => {
    // Open a comment form to expose its textarea. The mobile file picker
    // gives us a known file; we tap a line gutter to open a form.
    // Actually simpler: post a comment so a reply input renders, OR open
    // the review-conversation form. Easiest is to tap a markdown line.
    const fileSec = (await fileSectionByName(page, '.md')).first();
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

  test('reply actions stay visible and meet 44x44 targets without hover', async ({ page, request }) => {
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

    const replyActions = page.locator('.reply-actions');
    await expect(replyActions).toBeVisible();
    await expect(replyActions).toHaveCSS('opacity', '1');
    await expectTouchTargets(replyActions.locator('button'), 2);
  });
});

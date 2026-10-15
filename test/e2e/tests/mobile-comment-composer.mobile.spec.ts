import { test, expect, type Locator, type Page } from '@playwright/test';
import { clearAllComments, goSection, loadPage, tapCenter, waitUntilHittable } from './helpers';

// F4 + composer: on touch, a single tap on a commentable diff line number
// opens the comment form (no hover step, no second tap on a hover "+"), and
// the composer fits the phone viewport.
//
// IMPORTANT LIMITATION: Playwright's page.touchscreen.tap() synthesizes
// pointerdown → pointerup deterministically. It does NOT produce the
// pointercancel that real touch hardware emits when the browser's
// scroll/zoom gesture recognizer wins the touch sequence. These tests
// verify the JS event routing path; the real-hardware fix is
// touch-action:none on the gutter tap target, asserted as CSS below.

// Line-number cell of the first added line (mobile renders unified diffs).
function additionGutter(item: Locator): Locator {
  return item.locator(
    'code[data-unified] [data-gutter] > [data-column-number][data-line-type="change-addition"]',
  ).first();
}

// One tap on the first added line's number must open its form. tapCenter
// re-centres the row until it holds still and takes hits, then taps once.
async function touchOpenFirstAddition(page: Page): Promise<number> {
  const gutter = additionGutter(await goSection(page));
  await expect(gutter).toBeVisible();
  await tapCenter(page, gutter);
  await expect(page.locator('#filesContainer .comment-form textarea')).toBeVisible();
  return Number(await gutter.getAttribute('data-column-number'));
}

// Tap a button the way a finger would: only once it is the hit target at its
// centre (the sticky picker bar or a re-mounting row can cover it).
async function tapButton(page: Page, button: Locator) {
  await button.scrollIntoViewIfNeeded();
  await waitUntilHittable(button);
  const box = await button.boundingBox();
  expect(box).not.toBeNull();
  await page.touchscreen.tap(box!.x + box!.width / 2, box!.y + box!.height / 2);
}

test.describe('Mobile comment composer', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('a single tap opens a composer that fits the mobile viewport; the gutter is touch-action:none', async ({ page }) => {
    // The real-hardware reliability fix: if this regresses, hardware drops taps.
    await expect(additionGutter(await goSection(page))).toHaveCSS('touch-action', 'none');

    await touchOpenFirstAddition(page);

    const wrapper = page.locator('.comment-form-wrapper');
    const textarea = wrapper.locator('textarea');
    await expect(wrapper).toBeVisible();
    await expect(textarea).toBeFocused();

    const layout = await wrapper.evaluate((element) => {
      const wrapperRect = element.getBoundingClientRect();
      const textareaRect = element.querySelector('textarea')!.getBoundingClientRect();
      return {
        viewportWidth: document.documentElement.clientWidth,
        pageScrollWidth: document.documentElement.scrollWidth,
        wrapperLeft: wrapperRect.left,
        wrapperRight: wrapperRect.right,
        textareaWidth: textareaRect.width,
      };
    });

    expect(layout.wrapperLeft).toBeGreaterThanOrEqual(0);
    expect(layout.wrapperRight).toBeLessThanOrEqual(layout.viewportWidth);
    expect(layout.textareaWidth).toBeGreaterThan(layout.viewportWidth * 0.6);
    expect(layout.pageScrollWidth).toBe(layout.viewportWidth);
  });

  test('a touch-opened comment can be submitted and persists through the API', async ({ page, request }) => {
    const line = await touchOpenFirstAddition(page);
    expect(line).toBeGreaterThan(0);

    const form = page.locator('#filesContainer .comment-form');
    await form.locator('textarea').fill('Submitted from mobile');
    await tapButton(page, form.locator('.btn-primary'));

    await expect(form).toHaveCount(0);
    await expect(
      (await goSection(page)).locator('.comment-card .comment-body', { hasText: 'Submitted from mobile' }),
    ).toBeVisible();
    await expect(page.locator('#commentCount .comment-count-number')).toHaveText('1');

    await expect.poll(async () => {
      const response = await request.get('/api/file/comments?path=server.go');
      expect(response.ok()).toBeTruthy();
      const comments = await response.json() as Array<{ end_line: number; body: string }>;
      return comments.some(comment => comment.end_line === line && comment.body === 'Submitted from mobile');
    }).toBe(true);
  });

  test('tapping Cancel closes a touch-opened form', async ({ page }) => {
    await touchOpenFirstAddition(page);
    const form = page.locator('#filesContainer .comment-form');
    await tapButton(page, form.locator('button', { hasText: 'Cancel' }).first());
    await expect(form).toHaveCount(0);
  });
});

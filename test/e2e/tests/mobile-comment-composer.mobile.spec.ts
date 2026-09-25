import { test, expect, type Locator, type Page } from '@playwright/test';
import { clearAllComments, goSection, loadPage, tapCenter } from './helpers';

// Open a line comment on the first added line of server.go with a single
// tap on its line number (mobile-tap-to-comment covers the tap itself) and
// return that line's number. Pierre can re-render the row while the page
// settles, so the row is re-resolved until the tap lands.
async function touchOpenFirstAddition(page: Page): Promise<number> {
  const item = await goSection(page);
  const gutter: Locator = item.locator(
    'code[data-unified] [data-gutter] > [data-column-number][data-line-type="change-addition"]',
  ).first();
  const textarea = page.locator('#filesContainer .comment-form textarea');
  await expect(async () => {
    await tapCenter(page, gutter);
    await expect(textarea).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10_000 });
  return Number(await gutter.getAttribute('data-column-number'));
}

test.describe('Mobile comment composer', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('composer uses the mobile viewport instead of the desktop gutter indent', async ({ page }) => {
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

    const form = page.locator('.comment-form');
    await form.locator('textarea').fill('Submitted from mobile');
    await form.locator('.btn-primary').click();

    await expect(form).toBeHidden();
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
    const cancel = form.locator('button', { hasText: 'Cancel' }).first();
    // Tap where the button is once Pierre accepts pointer input again.
    await expect.poll(() => page.evaluate(() =>
      !document.querySelector('#filesContainer [style*="pointer-events"]'),
    )).toBe(true);
    const box = await cancel.boundingBox();
    expect(box).not.toBeNull();
    await page.touchscreen.tap(box!.x + box!.width / 2, box!.y + box!.height / 2);
    await expect(form).toHaveCount(0);
  });
});

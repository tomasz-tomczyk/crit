import { test, expect, type Locator, type Page } from '@playwright/test';
import { clearAllComments, goSection, loadPage } from './helpers';

async function tapFirstAdditionGutter(page: Page): Promise<Locator> {
  const addition = goSection(page).locator('.diff-container.unified .diff-line.addition').first();
  await addition.scrollIntoViewIfNeeded();
  const gutter = addition.locator('.diff-gutter-num').last();
  await expect(gutter).toBeVisible();
  const box = await gutter.boundingBox();
  expect(box).not.toBeNull();
  await page.touchscreen.tap(box!.x + box!.width / 2, box!.y + box!.height / 2);
  return addition;
}

test.describe('Mobile comment composer', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('composer uses the mobile viewport instead of the desktop gutter indent', async ({ page }) => {
    await tapFirstAdditionGutter(page);

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
    const addition = await tapFirstAdditionGutter(page);
    const line = Number(await addition.getAttribute('data-diff-line-num'));
    expect(line).toBeGreaterThan(0);

    const form = page.locator('.comment-form');
    await form.locator('textarea').fill('Submitted from mobile');
    await form.locator('.btn-primary').click();

    await expect(form).toBeHidden();
    await expect(
      goSection(page).locator('.comment-card .comment-body', { hasText: 'Submitted from mobile' }),
    ).toBeVisible();
    await expect(page.locator('#commentCount .comment-count-number')).toHaveText('1');

    await expect.poll(async () => {
      const response = await request.get('/api/file/comments?path=server.go');
      expect(response.ok()).toBeTruthy();
      const comments = await response.json() as Array<{ end_line: number; body: string }>;
      return comments.some(comment => comment.end_line === line && comment.body === 'Submitted from mobile');
    }).toBe(true);
  });
});

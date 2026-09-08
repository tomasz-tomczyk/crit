import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, dragBetween } from './helpers';

test.describe('Drag Selection — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
  });

  test('markdown gutter drag opens multi-line comment form in file mode', async ({ page }) => {
    const section = mdSection(page);
    const gutters = section.locator('.line-comment-gutter');
    const firstGutter = gutters.nth(0);
    const thirdGutter = gutters.nth(2);

    await expect(firstGutter).toBeAttached();
    await expect(thirdGutter).toBeAttached();
    await firstGutter.scrollIntoViewIfNeeded();

    await dragBetween(page, firstGutter, thirdGutter);

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();
    await expect(page.locator('.comment-form-header')).toContainText('Lines');

    await page.locator('.comment-form textarea').fill('Multi-line drag comment in file mode');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toBeVisible();
    await expect(section.locator('.comment-card .comment-body')).toContainText('Multi-line drag comment in file mode');
  });
});

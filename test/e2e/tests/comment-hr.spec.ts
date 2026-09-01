import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, switchToDocumentView } from './helpers';

test.describe('Comment horizontal rules', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);
  });

  test('styles markdown horizontal rules in comment bodies', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Above\n\n---\n\nBelow');
    await page.locator('.comment-form .btn-primary').click();

    const hr = section.locator('.comment-card .comment-body hr');
    await expect(hr).toHaveCount(1);
    await expect(hr).toHaveCSS('height', '1px');
    await expect(hr).toHaveCSS('border-top-width', '0px');
  });
});

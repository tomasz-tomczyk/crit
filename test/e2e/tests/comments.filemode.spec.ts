import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection } from './helpers';

test.describe('Comments — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    const section = await mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
  });

  test('can add and delete a markdown comment in file mode; @ opens the file picker', async ({ page }) => {
    const section = await mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    const gutterBtn = section.locator('.line-comment-gutter').first();
    await expect(gutterBtn).toBeVisible();
    await gutterBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await expect(textarea).toBeFocused();

    // Typing @ opens the file picker autocomplete with the session's files.
    await textarea.pressSequentially('@');
    const dropdown = page.locator('.file-picker-dropdown');
    await expect(dropdown).toBeVisible();
    await expect(dropdown.locator('.file-picker-item').first()).toBeVisible();

    await textarea.fill('File mode comment');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('File mode comment');

    await card.locator('.comment-actions .delete-btn').click();
    await expect(section.locator('.comment-card')).toHaveCount(0);
  });

  test('renders safe HTML while stripping unsafe markup from comments', async ({ page }) => {
    const section = await mdSection(page);
    await section.locator('.line-block').first().hover();
    await section.locator('.line-comment-gutter').first().click();

    await page.locator('.comment-form textarea').fill(
      '<details open><summary>More context</summary>Safe content</details><!-- agent metadata --><img src=x onerror="window.__commentXss = true"><a href="javascript:alert(1)">unsafe</a>'
    );
    await page.locator('.comment-form .btn-primary').click();

    const body = section.locator('.comment-card .comment-body');
    await expect(body.locator('details[open] > summary')).toHaveText('More context');
    await expect(body).toContainText('Safe content');
    await expect(body.locator('script, [onerror], [onclick]')).toHaveCount(0);
    await expect(body.locator('a[href^="javascript:"]')).toHaveCount(0);
    await expect(await body.evaluate(el => {
      const walker = document.createTreeWalker(el, NodeFilter.SHOW_COMMENT);
      return walker.nextNode();
    })).toBeNull();
  });
});

import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection } from './helpers';

test.describe('Select-to-comment (file mode)', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('selecting text opens comment form', async ({ page }) => {
    const section = mdSection(page);
    const firstBlock = section.locator('.line-block').first();
    await expect(firstBlock).toBeVisible();

    const blockBox = await firstBlock.boundingBox();
    expect(blockBox).toBeTruthy();
    if (!blockBox) return;

    await page.mouse.move(blockBox.x + 60, blockBox.y + blockBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(blockBox.x + blockBox.width - 10, blockBox.y + blockBox.height / 2, { steps: 5 });
    await page.mouse.up();

    const textarea = section.locator('.comment-form textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toBeFocused();
  });

  test('full comment lifecycle via text selection', async ({ page }) => {
    const section = mdSection(page);
    const firstBlock = section.locator('.line-block').first();
    const blockBox = await firstBlock.boundingBox();
    expect(blockBox).toBeTruthy();
    if (!blockBox) return;

    await page.mouse.move(blockBox.x + 60, blockBox.y + blockBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(blockBox.x + blockBox.width - 10, blockBox.y + blockBox.height / 2, { steps: 5 });
    await page.mouse.up();

    const textarea = section.locator('.comment-form textarea');
    await textarea.fill('Nice improvement here');
    await textarea.press('Control+Enter');

    const comment = section.locator('.comment-card');
    await expect(comment).toBeVisible();
    await expect(comment).toContainText('Nice improvement here');
  });

  test('selecting text in non-commentable area does not trigger', async ({ page }) => {
    const header = page.locator('.header');
    const headerBox = await header.boundingBox();
    expect(headerBox).toBeTruthy();
    if (!headerBox) return;

    await page.mouse.move(headerBox.x + 10, headerBox.y + headerBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(headerBox.x + headerBox.width / 2, headerBox.y + headerBox.height / 2, { steps: 5 });
    await page.mouse.up();

    await expect(page.locator('.comment-form')).not.toBeVisible();
  });
});

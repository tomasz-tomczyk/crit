import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, goSection, switchToDocumentView } from './helpers';

test.describe('Select-to-comment (git mode)', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test.describe('document view', () => {
    test.beforeEach(async ({ page }) => {
      await switchToDocumentView(page);
    });

    test('selecting text opens comment form immediately', async ({ page }) => {
      const section = mdSection(page);
      const firstBlock = section.locator('.line-block').first();
      await expect(firstBlock).toBeVisible();

      const blockBox = await firstBlock.boundingBox();
      expect(blockBox).toBeTruthy();
      if (!blockBox) return;

      // Start past the 44px line-gutter (which has user-select: none)
      await page.mouse.move(blockBox.x + 60, blockBox.y + blockBox.height / 2);
      await page.mouse.down();
      await page.mouse.move(blockBox.x + blockBox.width - 10, blockBox.y + blockBox.height / 2, { steps: 5 });
      await page.mouse.up();

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await expect(textarea).toBeFocused();
    });

    test('Escape cancels the comment form', async ({ page }) => {
      const section = mdSection(page);
      const firstBlock = section.locator('.line-block').first();
      const blockBox = await firstBlock.boundingBox();
      expect(blockBox).toBeTruthy();
      if (!blockBox) return;

      await page.mouse.move(blockBox.x + 60, blockBox.y + blockBox.height / 2);
      await page.mouse.down();
      await page.mouse.move(blockBox.x + blockBox.width - 10, blockBox.y + blockBox.height / 2, { steps: 5 });
      await page.mouse.up();

      await expect(section.locator('.comment-form textarea')).toBeVisible();

      await page.keyboard.press('Escape');
      await expect(section.locator('.comment-form')).not.toBeVisible();
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
      await expect(textarea).toBeFocused();
      await textarea.fill('Hello from text selection');
      await textarea.press('Control+Enter');

      const comment = section.locator('.comment-card');
      await expect(comment).toBeVisible();
      await expect(comment).toContainText('Hello from text selection');
    });

    test('coexists with already-open comment form (multi-form)', async ({ page }) => {
      const section = mdSection(page);

      // Open a comment form via gutter click first
      const firstBlock = section.locator('.line-block').first();
      await firstBlock.hover();
      const gutterBtn = section.locator('.line-comment-gutter').first();
      await expect(gutterBtn).toBeVisible();
      await gutterBtn.click();

      await expect(section.locator('.comment-form')).toHaveCount(1);

      // Now select text in a different block — should open a second form
      const thirdBlock = section.locator('.line-block').nth(2);
      await thirdBlock.scrollIntoViewIfNeeded();
      const blockBox = await thirdBlock.boundingBox();
      if (!blockBox) return;

      await page.mouse.move(blockBox.x + 60, blockBox.y + blockBox.height / 2);
      await page.mouse.down();
      await page.mouse.move(blockBox.x + blockBox.width - 10, blockBox.y + blockBox.height / 2, { steps: 5 });
      await page.mouse.up();

      await expect(section.locator('.comment-form')).toHaveCount(2);
    });

    test('multi-block selection spans correct line range', async ({ page }) => {
      const section = mdSection(page);
      const blocks = section.locator('.line-block');
      const firstBlock = blocks.first();
      const thirdBlock = blocks.nth(2);

      await firstBlock.scrollIntoViewIfNeeded();
      const startBox = await firstBlock.boundingBox();
      await thirdBlock.scrollIntoViewIfNeeded();
      const endBox = await thirdBlock.boundingBox();
      expect(startBox).toBeTruthy();
      expect(endBox).toBeTruthy();
      if (!startBox || !endBox) return;

      await page.mouse.move(startBox.x + 60, startBox.y + startBox.height / 2);
      await page.mouse.down();
      await page.mouse.move(endBox.x + endBox.width - 10, endBox.y + endBox.height / 2, { steps: 10 });
      await page.mouse.up();

      const formHeader = section.locator('.comment-form-header');
      await expect(formHeader).toBeVisible();
      await expect(formHeader).toContainText('Comment on');
    });

    test('single click (no drag) does not open a form', async ({ page }) => {
      const section = mdSection(page);
      const firstBlock = section.locator('.line-block').first();
      await expect(firstBlock).toBeVisible();

      const blockBox = await firstBlock.boundingBox();
      expect(blockBox).toBeTruthy();
      if (!blockBox) return;

      await page.mouse.click(blockBox.x + 10, blockBox.y + blockBox.height / 2);

      await expect(section.locator('.comment-form')).not.toBeVisible();
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

  test.describe('diff view', () => {
    test('selecting diff text opens comment form', async ({ page }) => {
      // Use server.go (modified file) which has proper split diff sides
      const section = goSection(page);
      const additionLine = section.locator('.diff-split-side.addition').first();
      await additionLine.scrollIntoViewIfNeeded();
      await expect(additionLine).toBeVisible();

      // Target the .diff-content child directly to avoid gutter areas
      const diffContent = additionLine.locator('.diff-content');
      await expect(diffContent).toBeVisible();
      const box = await diffContent.boundingBox();
      expect(box).toBeTruthy();
      if (!box) return;

      await page.mouse.move(box.x + 10, box.y + box.height / 2);
      await page.mouse.down();
      await page.mouse.move(box.x + box.width - 10, box.y + box.height / 2, { steps: 5 });
      await page.mouse.up();

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await expect(textarea).toBeFocused();
    });
  });
});

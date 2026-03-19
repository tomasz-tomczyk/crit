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

    test('text selection does not open a second form when one is already open', async ({ page }) => {
      const section = mdSection(page);

      // Open a comment form via gutter click first
      const firstBlock = section.locator('.line-block').first();
      await firstBlock.hover();
      const gutterBtn = section.locator('.line-comment-gutter').first();
      await expect(gutterBtn).toBeVisible();
      await gutterBtn.click();

      await expect(section.locator('.comment-form')).toHaveCount(1);

      // Now select text in a different block — should NOT open a second form,
      // the selection should persist so the user can copy text
      const thirdBlock = section.locator('.line-block').nth(2);
      await thirdBlock.scrollIntoViewIfNeeded();
      const blockBox = await thirdBlock.boundingBox();
      if (!blockBox) return;

      await page.mouse.move(blockBox.x + 60, blockBox.y + blockBox.height / 2);
      await page.mouse.down();
      await page.mouse.move(blockBox.x + blockBox.width - 10, blockBox.y + blockBox.height / 2, { steps: 5 });
      await page.mouse.up();

      // Still only one form — text selection is for copying, not commenting
      await expect(section.locator('.comment-form')).toHaveCount(1);

      // Browser selection should persist
      const selectedText = await page.evaluate(() => window.getSelection()?.toString().trim());
      expect(selectedText).toBeTruthy();
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

  test.describe('quote highlight', () => {
    test.beforeEach(async ({ page }) => {
      await switchToDocumentView(page);
    });

    test('quote highlight appears while comment form is still open', async ({ page }) => {
      const section = mdSection(page);
      const block = section.locator('.line-block', { hasText: 'API key authentication' });
      await expect(block).toBeVisible();
      const content = block.locator('.line-content');
      const box = await content.boundingBox();
      expect(box).toBeTruthy();
      if (!box) return;

      // Select just a portion of the text
      await page.mouse.move(box.x + 80, box.y + box.height / 2);
      await page.mouse.down();
      await page.mouse.move(box.x + 250, box.y + box.height / 2, { steps: 5 });
      await page.mouse.up();

      // Form is open but NOT yet submitted — highlight should already be visible
      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await expect(section.locator('mark.quote-highlight')).toBeVisible();
    });

    test('partial text selection saves quote and shows highlight mark', async ({ page }) => {
      const section = mdSection(page);
      // Line 5: "We're adding API key authentication to the server..."
      // Find the block containing this text
      const block = section.locator('.line-block', { hasText: 'API key authentication' });
      await expect(block).toBeVisible();
      const content = block.locator('.line-content');
      const box = await content.boundingBox();
      expect(box).toBeTruthy();
      if (!box) return;

      // Select just a portion of the text (middle area, not full width)
      await page.mouse.move(box.x + 80, box.y + box.height / 2);
      await page.mouse.down();
      await page.mouse.move(box.x + 250, box.y + box.height / 2, { steps: 5 });
      await page.mouse.up();

      // Submit the comment
      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await textarea.fill('Check this part');
      await textarea.press('Control+Enter');

      // After submit, the quote-highlight mark should appear in the document
      await expect(section.locator('mark.quote-highlight')).toBeVisible();
    });

    test('cross-line partial selection saves quote and shows highlight', async ({ page, request }) => {
      const section = mdSection(page);
      // Lines 3-5 in plan.md: "## Overview" (line 3), blank (line 4),
      // "We're adding API key authentication..." (line 5)
      // Select from middle of "Overview" heading down into the paragraph
      const overviewBlock = section.locator('.line-block', { hasText: 'Overview' }).first();
      const authBlock = section.locator('.line-block', { hasText: 'API key authentication' });
      await expect(overviewBlock).toBeVisible();
      await expect(authBlock).toBeVisible();

      const startContent = overviewBlock.locator('.line-content');
      const endContent = authBlock.locator('.line-content');
      const startBox = await startContent.boundingBox();
      const endBox = await endContent.boundingBox();
      expect(startBox).toBeTruthy();
      expect(endBox).toBeTruthy();
      if (!startBox || !endBox) return;

      // Drag from middle of "Overview" to middle of auth line
      await page.mouse.move(startBox.x + 60, startBox.y + startBox.height / 2);
      await page.mouse.down();
      await page.mouse.move(endBox.x + 150, endBox.y + endBox.height / 2, { steps: 10 });
      await page.mouse.up();

      // Comment form should open
      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await textarea.fill('Cross-line comment');
      await textarea.press('Control+Enter');
      await expect(section.locator('.comment-card')).toBeVisible();

      // Verify quote was saved via API
      const mdPath = await page.evaluate(() => {
        const el = document.querySelector('.file-section[id*="plan"] .line-block[data-file-path]');
        return el ? (el as HTMLElement).dataset.filePath : null;
      });
      const res = await request.get(`/api/file/comments?path=${mdPath}`);
      const comments = await res.json();
      const crossLine = comments.find((c: any) => c.body === 'Cross-line comment');
      expect(crossLine).toBeTruthy();
      expect(crossLine.quote).toBeTruthy();
      expect(crossLine.quote.length).toBeGreaterThan(0);

      // Quote highlight marks should appear in the document
      await expect(section.locator('mark.quote-highlight').first()).toBeVisible();
    });

    test('quote highlight inherits text color (not black)', async ({ page }) => {
      const section = mdSection(page);
      const block = section.locator('.line-block', { hasText: 'API key authentication' });
      await expect(block).toBeVisible();
      const content = block.locator('.line-content');
      const box = await content.boundingBox();
      if (!box) return;

      await page.mouse.move(box.x + 80, box.y + box.height / 2);
      await page.mouse.down();
      await page.mouse.move(box.x + 250, box.y + box.height / 2, { steps: 5 });
      await page.mouse.up();

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await textarea.fill('Color check');
      await textarea.press('Control+Enter');

      const mark = section.locator('mark.quote-highlight');
      await expect(mark).toBeVisible();

      // Verify the mark has color: inherit (not browser default black)
      const color = await mark.evaluate(el => getComputedStyle(el).color);
      expect(color).not.toBe('rgb(0, 0, 0)');
    });

    test('full-line selection does NOT produce a quote highlight', async ({ page }) => {
      const section = mdSection(page);
      const block = section.locator('.line-block', { hasText: 'API key authentication' });
      await expect(block).toBeVisible();
      const content = block.locator('.line-content');
      const box = await content.boundingBox();
      if (!box) return;

      // Select the FULL width of the content (start to end)
      await page.mouse.move(box.x, box.y + box.height / 2);
      await page.mouse.down();
      await page.mouse.move(box.x + box.width, box.y + box.height / 2, { steps: 5 });
      await page.mouse.up();

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await textarea.fill('Full line comment');
      await textarea.press('Control+Enter');

      // No quote highlight should appear (full line = redundant)
      await expect(section.locator('mark.quote-highlight')).not.toBeVisible();
    });

    test('quote is stored in API response', async ({ page, request }) => {
      const section = mdSection(page);
      const block = section.locator('.line-block', { hasText: 'API key authentication' });
      await expect(block).toBeVisible();
      const content = block.locator('.line-content');
      const box = await content.boundingBox();
      if (!box) return;

      // Partial selection
      await page.mouse.move(box.x + 80, box.y + box.height / 2);
      await page.mouse.down();
      await page.mouse.move(box.x + 250, box.y + box.height / 2, { steps: 5 });
      await page.mouse.up();

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await textarea.fill('API check');
      await textarea.press('Control+Enter');
      await expect(section.locator('.comment-card')).toBeVisible();

      // Verify the quote field exists in the API response
      const mdPath = await page.evaluate(() => {
        const el = document.querySelector('.file-section[id*="plan"] .line-block[data-file-path]');
        return el ? (el as HTMLElement).dataset.filePath : null;
      });
      expect(mdPath).toBeTruthy();
      const res = await request.get(`/api/file/comments?path=${mdPath}`);
      const comments = await res.json();
      const withQuote = comments.filter((c: any) => c.quote);
      expect(withQuote.length).toBeGreaterThan(0);
      expect(withQuote[0].quote.length).toBeGreaterThan(0);
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

    test('quote highlight appears in split diff view while form is open', async ({ page }) => {
      const section = goSection(page);
      // Find an addition line with enough text for a partial selection
      const additionLine = section.locator('.diff-split-side.addition').first();
      await additionLine.scrollIntoViewIfNeeded();
      await expect(additionLine).toBeVisible();

      const diffContent = additionLine.locator('.diff-content');
      await expect(diffContent).toBeVisible();
      const box = await diffContent.boundingBox();
      expect(box).toBeTruthy();
      if (!box) return;

      // Partial selection (not full width) to ensure a quote is captured
      await page.mouse.move(box.x + 10, box.y + box.height / 2);
      await page.mouse.down();
      await page.mouse.move(box.x + Math.min(box.width / 2, 150), box.y + box.height / 2, { steps: 5 });
      await page.mouse.up();

      // Form should be open but NOT submitted — highlight should already show
      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await expect(section.locator('mark.quote-highlight')).toBeVisible();
    });

    test('quote highlight appears in unified diff view while form is open', async ({ page }) => {
      // Switch to unified mode via the header toggle
      const unifiedBtn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
      await expect(unifiedBtn).toBeVisible();
      await unifiedBtn.click();

      const section = goSection(page);
      // Find an addition line with enough text for a meaningful partial selection
      // Skip very short lines (like just `"log"`) — find one with substantial content
      const additionLines = section.locator('.diff-line.addition');
      let targetBox: any = null;
      const count = await additionLines.count();
      for (let i = 0; i < count; i++) {
        const line = additionLines.nth(i);
        const content = line.locator('.diff-content');
        const text = await content.textContent();
        if (text && text.trim().length > 20) {
          await line.scrollIntoViewIfNeeded();
          targetBox = await content.boundingBox();
          break;
        }
      }
      expect(targetBox).toBeTruthy();
      if (!targetBox) return;

      // Partial selection
      await page.mouse.move(targetBox.x + 10, targetBox.y + targetBox.height / 2);
      await page.mouse.down();
      await page.mouse.move(targetBox.x + Math.min(targetBox.width / 2, 150), targetBox.y + targetBox.height / 2, { steps: 5 });
      await page.mouse.up();

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await expect(section.locator('mark.quote-highlight')).toBeVisible();
    });

    test('quote highlight appears in markdown diff view while form is open', async ({ page }) => {
      // Markdown file in git mode defaults to split diff view
      const section = mdSection(page);
      const additionLine = section.locator('.diff-split-side.addition').first();
      await additionLine.scrollIntoViewIfNeeded();
      await expect(additionLine).toBeVisible();

      const diffContent = additionLine.locator('.diff-content');
      await expect(diffContent).toBeVisible();
      const box = await diffContent.boundingBox();
      expect(box).toBeTruthy();
      if (!box) return;

      // Partial selection
      await page.mouse.move(box.x + 10, box.y + box.height / 2);
      await page.mouse.down();
      await page.mouse.move(box.x + Math.min(box.width / 2, 150), box.y + box.height / 2, { steps: 5 });
      await page.mouse.up();

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await expect(section.locator('mark.quote-highlight')).toBeVisible();
    });
  });
});

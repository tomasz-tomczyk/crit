import { test, expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, goSection, switchToDocumentView } from './helpers';

// Helper: drag-select between two coordinates, then press `c` to comment.
// Selection alone never opens the form — `c` is the explicit commit.
async function selectAndPressC(
  page: Page,
  x1: number, y1: number, x2: number, y2: number,
  steps = 5,
) {
  await page.mouse.move(x1, y1);
  await page.mouse.down();
  await page.mouse.move(x2, y2, { steps });
  await page.mouse.up();
  await page.keyboard.press('c');
}

test.describe('Select-to-comment (git mode)', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test.describe('document view', () => {
    test.beforeEach(async ({ page }) => {
      await switchToDocumentView(page);
    });

    test('selecting text alone does not open form; selection preserved for copying', async ({ page }) => {
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

      // Form not visible — selection alone is for copying
      await expect(section.locator('.comment-form')).not.toBeVisible();

      // Selection still present
      const selectedText = await page.evaluate(() => window.getSelection()?.toString().trim());
      expect(selectedText).toBeTruthy();
    });

    test('selecting text then pressing c opens the comment form', async ({ page }) => {
      const section = mdSection(page);
      const firstBlock = section.locator('.line-block').first();
      await expect(firstBlock).toBeVisible();

      const blockBox = await firstBlock.boundingBox();
      expect(blockBox).toBeTruthy();
      if (!blockBox) return;

      await selectAndPressC(
        page,
        blockBox.x + 60, blockBox.y + blockBox.height / 2,
        blockBox.x + blockBox.width - 10, blockBox.y + blockBox.height / 2,
      );

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await expect(textarea).toBeFocused();
    });

    test('Escape cancels the comment form once opened', async ({ page }) => {
      const section = mdSection(page);
      const firstBlock = section.locator('.line-block').first();
      const blockBox = await firstBlock.boundingBox();
      expect(blockBox).toBeTruthy();
      if (!blockBox) return;

      await selectAndPressC(
        page,
        blockBox.x + 60, blockBox.y + blockBox.height / 2,
        blockBox.x + blockBox.width - 10, blockBox.y + blockBox.height / 2,
      );

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

      await selectAndPressC(
        page,
        blockBox.x + 60, blockBox.y + blockBox.height / 2,
        blockBox.x + blockBox.width - 10, blockBox.y + blockBox.height / 2,
      );

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeFocused();
      await textarea.fill('Hello from text selection');
      await textarea.press('Control+Enter');

      const comment = section.locator('.comment-card');
      await expect(comment).toBeVisible();
      await expect(comment).toContainText('Hello from text selection');
    });

    test('selecting alone does not open a second form when one is already open', async ({ page }) => {
      const section = mdSection(page);

      // Open a comment form via gutter click first
      const firstBlock = section.locator('.line-block').first();
      await firstBlock.hover();
      const gutterBtn = section.locator('.line-comment-gutter').first();
      await expect(gutterBtn).toBeVisible();
      await gutterBtn.click();

      await expect(section.locator('.comment-form')).toHaveCount(1);

      // Select text in a different block without pressing c — should NOT open a second form
      const thirdBlock = section.locator('.line-block').nth(2);
      await thirdBlock.scrollIntoViewIfNeeded();
      const blockBox = await thirdBlock.boundingBox();
      if (!blockBox) return;

      await page.mouse.move(blockBox.x + 60, blockBox.y + blockBox.height / 2);
      await page.mouse.down();
      await page.mouse.move(blockBox.x + blockBox.width - 10, blockBox.y + blockBox.height / 2, { steps: 5 });
      await page.mouse.up();

      // Still only one form; selection alone is for copying
      await expect(section.locator('.comment-form')).toHaveCount(1);
    });

    test('pressing c with a new selection opens a second form (multi-form workflow)', async ({ page }) => {
      const section = mdSection(page);
      const blocks = section.locator('.line-block');

      // First form: select-and-c on first block
      const firstBlock = blocks.first();
      const firstBox = await firstBlock.boundingBox();
      if (!firstBox) return;
      await selectAndPressC(
        page,
        firstBox.x + 60, firstBox.y + firstBox.height / 2,
        firstBox.x + firstBox.width - 10, firstBox.y + firstBox.height / 2,
      );
      const firstTextarea = section.locator('.comment-form textarea').first();
      await expect(firstTextarea).toBeVisible();
      // Type something so the first form isn't auto-closed when the second opens
      await firstTextarea.fill('In progress');

      // Second form: programmatically select content in a DIFFERENT block and press c.
      // (Drag-after-form-opens is fragile in headless tests because the open form
      // textarea retains focus; in a real session the mouse-drag shifts focus to body.)
      await page.evaluate(() => {
        (document.activeElement as HTMLElement)?.blur();
        const lineBlocks = Array.from(document.querySelectorAll('.line-block[data-file-path]'));
        // Skip the first commentable block (form 1 lives on it) and find a later one
        const target = lineBlocks
          .map(b => b.querySelector('.line-content'))
          .filter((c): c is Element => !!c && (c.textContent || '').trim().length > 10
            && !c.closest('.comment-form-wrapper'))
          .at(2);
        if (!target) throw new Error('no second commentable content found');
        const range = document.createRange();
        range.selectNodeContents(target);
        const sel = window.getSelection();
        sel?.removeAllRanges();
        sel?.addRange(range);
      });
      await page.keyboard.press('c');

      // Two forms open simultaneously — pressing c with a new selection adds another
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

      await selectAndPressC(
        page,
        startBox.x + 60, startBox.y + startBox.height / 2,
        endBox.x + endBox.width - 10, endBox.y + endBox.height / 2,
        10,
      );

      const formHeader = section.locator('.comment-form-header');
      await expect(formHeader).toBeVisible();
      await expect(formHeader).toContainText('Comment on');
    });

    test('selection endpoint on a gap container still spans full range (regression)', async ({ page, request }) => {
      const section = mdSection(page);
      const overviewBlock = section.locator('.line-block', { hasText: 'Overview' }).first();
      const paraBlock = section.locator('.line-block', { hasText: 'API key authentication' });
      await expect(overviewBlock).toBeVisible();
      await expect(paraBlock).toBeVisible();

      // Reproduces the bug class: selection crosses a blank-line boundary and one
      // endpoint resolves to the parent container (a "gap" between line-blocks)
      // rather than to a text node. This happens in real browsers with backward
      // drags across paragraph boundaries — anchor/focus can snap to the parent
      // element at the child-index between two line-blocks.
      //
      // Pre-fix behavior: findLineInfo() walks up via closest('.line-block') from
      // the parent container and returns null → entire selection-to-comment fails
      // OR collapses to whichever endpoint did resolve, depending on direction.
      await page.evaluate(() => {
        const blocks = Array.from(document.querySelectorAll('.line-block[data-file-path]')) as HTMLElement[];
        const heading = blocks.find(b => (b.textContent || '').includes('Overview'));
        const para = blocks.find(b => (b.textContent || '').includes('API key authentication'));
        if (!heading || !para) throw new Error('blocks not found');
        const wrapper = heading.parentElement!;
        const headingText = heading.querySelector('.line-content')!.firstChild as Text;
        const paraIdx = Array.from(wrapper.childNodes).indexOf(para);

        const range = document.createRange();
        range.setStart(headingText, 0);
        // End point lands ON the wrapper container at the index just after the
        // paragraph block. This is the "gap container" position — exercises the
        // bug where the endpoint has no .line-block ancestor.
        range.setEnd(wrapper, paraIdx + 1);

        const sel = window.getSelection()!;
        sel.removeAllRanges();
        sel.addRange(range);
      });

      await page.keyboard.press('c');

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await textarea.fill('Gap-endpoint comment');
      await textarea.press('Control+Enter');
      await expect(section.locator('.comment-card', { hasText: 'Gap-endpoint comment' })).toBeVisible();

      const mdPath = await page.evaluate(() => {
        const el = document.querySelector('.file-section[id*="plan"] .line-block[data-file-path]');
        return el ? (el as HTMLElement).dataset.filePath : null;
      });
      const res = await request.get(`/api/file/comments?path=${mdPath}`);
      const comments = await res.json();
      const c = comments.find((x: { body: string }) => x.body === 'Gap-endpoint comment');
      expect(c).toBeTruthy();
      // Range must span from heading (line 3) through paragraph (line 5).
      expect(c.start_line).toBe(3);
      expect(c.end_line).toBe(5);
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

      await selectAndPressC(
        page,
        box.x + 80, box.y + box.height / 2,
        box.x + 250, box.y + box.height / 2,
      );

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await expect(section.locator('mark.quote-highlight')).toBeVisible();
    });

    test('partial text selection saves quote and shows highlight mark', async ({ page }) => {
      const section = mdSection(page);
      const block = section.locator('.line-block', { hasText: 'API key authentication' });
      await expect(block).toBeVisible();
      const content = block.locator('.line-content');
      const box = await content.boundingBox();
      expect(box).toBeTruthy();
      if (!box) return;

      await selectAndPressC(
        page,
        box.x + 80, box.y + box.height / 2,
        box.x + 250, box.y + box.height / 2,
      );

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await textarea.fill('Check this part');
      await textarea.press('Control+Enter');

      await expect(section.locator('mark.quote-highlight')).toBeVisible();
    });

    test('cross-line partial selection saves quote and shows highlight', async ({ page, request }) => {
      const section = mdSection(page);
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

      await selectAndPressC(
        page,
        startBox.x + 60, startBox.y + startBox.height / 2,
        endBox.x + 150, endBox.y + endBox.height / 2,
        10,
      );

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await textarea.fill('Cross-line comment');
      await textarea.press('Control+Enter');
      await expect(section.locator('.comment-card')).toBeVisible();

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

      await expect(section.locator('mark.quote-highlight').first()).toBeVisible();
    });

    test('quote highlight inherits text color (not black)', async ({ page }) => {
      const section = mdSection(page);
      const block = section.locator('.line-block', { hasText: 'API key authentication' });
      await expect(block).toBeVisible();
      const content = block.locator('.line-content');
      const box = await content.boundingBox();
      if (!box) return;

      await selectAndPressC(
        page,
        box.x + 80, box.y + box.height / 2,
        box.x + 250, box.y + box.height / 2,
      );

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await textarea.fill('Color check');
      await textarea.press('Control+Enter');

      const mark = section.locator('mark.quote-highlight');
      await expect(mark).toBeVisible();

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

      // Programmatically select the entire .line-content text, then press `c`.
      await page.evaluate(() => {
        const blk = Array.from(document.querySelectorAll('.line-block'))
          .find(b => (b.textContent || '').includes('API key authentication'));
        const c = blk?.querySelector('.line-content');
        if (!c) throw new Error('no line-content found');
        const range = document.createRange();
        range.selectNodeContents(c);
        const sel = window.getSelection();
        sel?.removeAllRanges();
        sel?.addRange(range);
      });
      await page.keyboard.press('c');

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await textarea.fill('Full line comment');
      await textarea.press('Control+Enter');

      await expect(section.locator('mark.quote-highlight')).not.toBeVisible();
    });

    test('quote is stored in API response', async ({ page, request }) => {
      const section = mdSection(page);
      const block = section.locator('.line-block', { hasText: 'API key authentication' });
      await expect(block).toBeVisible();
      const content = block.locator('.line-content');
      const box = await content.boundingBox();
      if (!box) return;

      await selectAndPressC(
        page,
        box.x + 80, box.y + box.height / 2,
        box.x + 250, box.y + box.height / 2,
      );

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await textarea.fill('API check');
      await textarea.press('Control+Enter');
      await expect(section.locator('.comment-card')).toBeVisible();

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
    test('selecting diff text and pressing c opens comment form', async ({ page }) => {
      const section = goSection(page);
      const additionLine = section.locator('.diff-split-side.addition').first();
      await additionLine.scrollIntoViewIfNeeded();
      await expect(additionLine).toBeVisible();

      const diffContent = additionLine.locator('.diff-content');
      await expect(diffContent).toBeVisible();
      const box = await diffContent.boundingBox();
      expect(box).toBeTruthy();
      if (!box) return;

      await selectAndPressC(
        page,
        box.x + 10, box.y + box.height / 2,
        box.x + box.width - 10, box.y + box.height / 2,
      );

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await expect(textarea).toBeFocused();
    });

    test('quote highlight appears in split diff view while form is open', async ({ page }) => {
      const section = goSection(page);
      const additionLines = section.locator('.diff-split-side.addition');
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

      await selectAndPressC(
        page,
        targetBox.x + 10, targetBox.y + targetBox.height / 2,
        targetBox.x + Math.min(targetBox.width / 2, 150), targetBox.y + targetBox.height / 2,
      );

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await expect(section.locator('mark.quote-highlight')).toBeVisible();
    });

    test('quote highlight appears in unified diff view while form is open', async ({ page }) => {
      const unifiedBtn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
      await expect(unifiedBtn).toBeVisible();
      await unifiedBtn.click();

      const section = goSection(page);
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

      await selectAndPressC(
        page,
        targetBox.x + 10, targetBox.y + targetBox.height / 2,
        targetBox.x + Math.min(targetBox.width / 2, 150), targetBox.y + targetBox.height / 2,
      );

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await expect(section.locator('mark.quote-highlight')).toBeVisible();
    });

    test('quote highlight appears on addition line, not deletion line in unified diff (issue #133)', async ({ page }) => {
      const unifiedBtn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
      await expect(unifiedBtn).toBeVisible();
      await unifiedBtn.click();

      const section = goSection(page);
      const additionLines = section.locator('.diff-line.addition');
      let targetLine: any = null;
      let targetBox: any = null;
      const count = await additionLines.count();
      for (let i = 0; i < count; i++) {
        const line = additionLines.nth(i);
        const content = line.locator('.diff-content');
        const text = await content.textContent();
        if (text && text.trim().length > 20) {
          await line.scrollIntoViewIfNeeded();
          targetLine = line;
          targetBox = await content.boundingBox();
          break;
        }
      }
      expect(targetBox).toBeTruthy();
      if (!targetBox || !targetLine) return;

      await selectAndPressC(
        page,
        targetBox.x + 10, targetBox.y + targetBox.height / 2,
        targetBox.x + Math.min(targetBox.width / 2, 150), targetBox.y + targetBox.height / 2,
      );

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();

      const highlightMark = section.locator('mark.quote-highlight').first();
      await expect(highlightMark).toBeVisible();
      const parentLine = highlightMark.locator('xpath=ancestor::div[contains(@class, "diff-line")]');
      await expect(parentLine).toHaveClass(/addition/);
      await expect(parentLine).not.toHaveClass(/deletion/);
    });

    test('quote highlight appears in markdown diff view while form is open', async ({ page }) => {
      const section = mdSection(page);
      const additionLine = section.locator('.diff-split-side.addition').first();
      await additionLine.scrollIntoViewIfNeeded();
      await expect(additionLine).toBeVisible();

      const diffContent = additionLine.locator('.diff-content');
      await expect(diffContent).toBeVisible();
      const box = await diffContent.boundingBox();
      expect(box).toBeTruthy();
      if (!box) return;

      await selectAndPressC(
        page,
        box.x + 10, box.y + box.height / 2,
        box.x + Math.min(box.width / 2, 150), box.y + box.height / 2,
      );

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeVisible();
      await expect(section.locator('mark.quote-highlight')).toBeVisible();
    });
  });
});

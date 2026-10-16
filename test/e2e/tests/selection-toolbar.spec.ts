import { test, expect } from '@playwright/test';
import type { Page, Locator } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import {
  clearAllComments, getMdPath, loadPage, goSection, switchToDocumentView,
  selectInBlock, selectInLine, nextFrames, waitHighlighted, expectPaintedQuote,
} from './helpers';

// The selection toolbar shows a Comment button above a mouse selection that
// `c` could comment on. It opens nothing by itself.

function toolbar(page: Page) {
  return page.locator('.crit-selection-toolbar');
}

function commentButton(page: Page) {
  return toolbar(page).getByRole('button', { name: 'Comment on selection' });
}

// A paragraph whose line is long enough for a partial selection (a quote).
function partialBlock(section: Locator) {
  return section.locator('.line-block', { hasText: 'API key authentication' });
}

function selectionRect(page: Page) {
  return page.evaluate(() => {
    const r = window.getSelection()!.getRangeAt(0).getClientRects()[0];
    return { top: r.top, bottom: r.bottom };
  });
}

test.describe('Selection toolbar (git mode)', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test.describe('document view', () => {
    let section: Locator;

    test.beforeEach(async ({ page }) => {
      section = await switchToDocumentView(page);
    });

    test('a mouse selection shows the toolbar next to it and opens no form', async ({ page }) => {
      await selectInBlock(page, partialBlock(section));

      const bar = toolbar(page);
      await expect(bar).toBeVisible();
      await expect(bar).toHaveAttribute('role', 'toolbar');
      await expect(commentButton(page)).toHaveAttribute('aria-keyshortcuts', 'c');
      await expect(bar.locator('kbd')).toHaveText('c');

      // Above the selection, or below it when the sticky chrome leaves no room.
      const barBox = (await bar.boundingBox())!;
      const sel = await selectionRect(page);
      if (await bar.getAttribute('data-placement') === 'above') {
        expect(barBox.y + barBox.height).toBeLessThanOrEqual(sel.top);
      } else {
        expect(barBox.y).toBeGreaterThanOrEqual(sel.bottom);
      }

      // Selecting stays non-intrusive: no form, the selection is still there.
      await expect(section.locator('.comment-form')).toHaveCount(0);
      expect(await page.evaluate(() => window.getSelection()?.toString().trim())).toBeTruthy();
    });

    test('Comment opens the same form as c, with the selected text as quote', async ({ page, request }) => {
      const selected = await selectInBlock(page, partialBlock(section));
      await commentButton(page).click();

      const textarea = section.locator('.comment-form textarea');
      await expect(textarea).toBeFocused();
      await expect(toolbar(page)).toBeHidden();
      await expect(section.locator('mark.quote-highlight')).toBeVisible();

      await textarea.fill('From the selection toolbar');
      await textarea.press('Control+Enter');
      await expect(section.locator('.comment-card', { hasText: 'From the selection toolbar' })).toBeVisible();

      const mdPath = await getMdPath(request);
      const comments = await (await request.get('/api/file/comments?path=' + encodeURIComponent(mdPath))).json() as Array<{ body: string; quote?: string }>;
      const saved = comments.find(c => c.body === 'From the selection toolbar');
      expect(saved?.quote?.replace(/\s+/g, ' ')).toBe(selected.replace(/\s+/g, ' '));
    });

    test('c with the toolbar open opens the form and hides the toolbar', async ({ page }) => {
      await selectInBlock(page, partialBlock(section));
      await expect(toolbar(page)).toBeVisible();

      await page.keyboard.press('c');
      await expect(section.locator('.comment-form textarea')).toBeVisible();
      await expect(toolbar(page)).toBeHidden();
    });

    test('Ctrl+C keeps the toolbar and the selection', async ({ page }) => {
      await selectInBlock(page, partialBlock(section));
      await expect(toolbar(page)).toBeVisible();

      await page.keyboard.press('Control+c');
      await nextFrames(page);
      await expect(toolbar(page)).toBeVisible();
      expect(await page.evaluate(() => window.getSelection()?.toString().trim())).toBeTruthy();
    });

    test('Escape hides only the toolbar: the selection and an open form stay', async ({ page }) => {
      // An empty form opened from the gutter, before the selection.
      const first = section.locator('.line-block').first();
      await first.hover();
      await section.locator('.line-comment-gutter').first().click();
      await expect(section.locator('.comment-form')).toHaveCount(1);

      await selectInBlock(page, partialBlock(section));
      await expect(toolbar(page)).toBeVisible();

      await page.keyboard.press('Escape');
      await expect(toolbar(page)).toBeHidden();
      await expect(section.locator('.comment-form')).toHaveCount(1);
      expect(await page.evaluate(() => window.getSelection()?.toString().trim())).toBeTruthy();
    });

    test('a click that keeps an older selection does not bring the toolbar back', async ({ page }) => {
      await selectInBlock(page, partialBlock(section));
      await expect(toolbar(page)).toBeVisible();
      await page.keyboard.press('Escape');
      await expect(toolbar(page)).toBeHidden();

      // The gutter keeps the selection (its mousedown is prevented) and opens
      // a form on another line: no toolbar for the older selection.
      const first = section.locator('.line-block').first();
      await first.hover();
      await section.locator('.line-comment-gutter').first().click();
      await expect(section.locator('.comment-form textarea')).toBeVisible();
      await nextFrames(page);
      await expect(toolbar(page)).toBeHidden();
    });

    test('a click in the text hides the toolbar', async ({ page }) => {
      await selectInBlock(page, partialBlock(section));
      await expect(toolbar(page)).toBeVisible();

      // A plain click in another paragraph collapses the selection.
      const other = section.locator('.line-block .line-content').first();
      const box = (await other.boundingBox())!;
      await page.mouse.click(box.x + Math.min(40, box.width / 2), box.y + box.height / 2);
      await nextFrames(page);
      await expect(toolbar(page)).toBeHidden();
      await expect(section.locator('.comment-form')).toHaveCount(0);
    });

    test('a keyboard selection shows no toolbar', async ({ page }) => {
      const content = section.locator('.line-block .line-content').first();
      await expect(content).toBeVisible();
      await content.evaluate((el) => {
        const text = document.createTreeWalker(el, NodeFilter.SHOW_TEXT).nextNode()!;
        const range = document.createRange();
        range.setStart(text, 0);
        range.setEnd(text, Math.min(5, text.textContent!.length));
        const sel = window.getSelection()!;
        sel.removeAllRanges();
        sel.addRange(range);
      });
      await page.keyboard.press('Shift+ArrowRight');
      await nextFrames(page);
      await expect(toolbar(page)).toBeHidden();

      await page.keyboard.press('c');
      await expect(section.locator('.comment-form textarea')).toBeVisible();
    });

    test('the toolbar has no axe violations', async ({ page }) => {
      await selectInBlock(page, partialBlock(section));
      await expect(toolbar(page)).toBeVisible();

      const results = await new AxeBuilder({ page })
        .include('.crit-selection-toolbar')
        .withTags(['wcag2a', 'wcag2aa'])
        .analyze();
      expect(results.violations.map(({ id, impact }) => ({ id, impact }))).toEqual([]);
    });
  });

  test.describe('diff view', () => {
    test('Comment on a code selection opens the line form with the quote', async ({ page, request }) => {
      // server.go new 24: `func authMiddleware(next http.HandlerFunc) ...`
      const item = await goSection(page);
      await waitHighlighted(item, 24);
      const selected = await selectInLine(page, item, 24);

      await expect(toolbar(page)).toBeVisible();
      await commentButton(page).click();

      const form = item.locator('.comment-form');
      await expect(form.locator('textarea')).toBeFocused();
      await expect(form.locator('.comment-form-header')).toHaveText('Comment on Line 24');
      await expect(toolbar(page)).toBeHidden();
      await expectPaintedQuote(page);

      await form.locator('textarea').fill('toolbar code quote');
      await form.locator('textarea').press('Control+Enter');
      await expect(item.locator('.comment-card', { hasText: 'toolbar code quote' })).toBeVisible();
      const comments = await (await request.get('/api/file/comments?path=server.go')).json() as Array<{ body: string; quote?: string; start_line: number; end_line: number }>;
      const saved = comments.find(c => c.body === 'toolbar code quote');
      expect(saved?.quote).toBe(selected.trim());
      expect([saved?.start_line, saved?.end_line]).toEqual([24, 24]);
    });
  });
});

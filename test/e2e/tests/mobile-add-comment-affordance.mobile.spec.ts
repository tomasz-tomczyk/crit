import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, goSection, switchToDocumentView } from './helpers';

// F3: visible touch-only `+` affordance.
// On touch (pointer:coarse) there is no hover, so the hover "+" button can't
// be the cue. Instead every commentable line number shows a `+` prefix (a
// ::before pseudo-element) without any interaction.
test.describe('Mobile add-comment affordance (F3)', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('diff gutter line number shows a "+" prefix on touch', async ({ page }) => {
    // A computed `content: none`/`""` means no visible pseudo-element,
    // regardless of opacity.
    const item = await goSection(page);
    // The cue sits on the number itself; the cell's own ::before is Pierre's
    // change bar.
    const lineNum = item.locator(
      'code[data-unified] [data-gutter] > [data-column-number][data-line-type="change-addition"] [data-line-number-content]',
    ).first();
    await expect(lineNum).toBeVisible();
    const beforeStyle = await lineNum.evaluate((el) => {
      const cs = getComputedStyle(el, '::before');
      return { content: cs.content, opacity: parseFloat(cs.opacity) };
    });
    expect(beforeStyle.content).toContain('+');
    expect(beforeStyle.opacity).toBeGreaterThan(0);
  });

  test('document view line-num shows a "+" prefix on touch', async ({ page }) => {
    const doc = await switchToDocumentView(page);
    const lineNum = doc.locator('.line-block:not(:has(.line-comment-gutter.diff-no-comment)) .line-num').first();
    await expect(lineNum).toBeVisible();
    const beforeStyle = await lineNum.evaluate((el) => {
      const cs = getComputedStyle(el, '::before');
      return { content: cs.content, opacity: parseFloat(cs.opacity) };
    });
    expect(beforeStyle.content).toContain('+');
    expect(beforeStyle.opacity).toBeGreaterThan(0);
  });
});

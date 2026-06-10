import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, goSection } from './helpers';

// F3: visible touch-only `+` affordance.
// On touch (pointer:coarse), the user sees a `+` prefix next to each
// commentable line number (a CSS ::before pseudo-element on .line-num
// / .diff-gutter-num). The desktop blue `+` button (.line-add /
// .diff-comment-btn) is hidden on touch because it depends on hover.
test.describe('Mobile add-comment affordance (F3)', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('line-num ::before "+" prefix is rendered on touch', async ({ page }) => {
    // The ::before pseudo-element on .diff-gutter-num renders a `+` prefix
    // on touch. We assert content is set (not the default 'none') AND that
    // it contains a "+". A computed `content: none` means no pseudo-element
    // exists at all, regardless of opacity.
    const lineNum = goSection(page).locator('.diff-gutter-num').first();
    await expect(lineNum).toBeAttached();
    const beforeStyle = await lineNum.evaluate((el) => {
      const cs = getComputedStyle(el, '::before');
      return { content: cs.content, opacity: parseFloat(cs.opacity) };
    });
    expect(beforeStyle.content).not.toBe('none');
    expect(beforeStyle.content).toContain('+');
    expect(beforeStyle.opacity).toBeGreaterThan(0);
  });

  test('desktop blue .diff-comment-btn is display:none on touch', async ({ page }) => {
    // The button stays in the DOM (its mousedown handler is the desktop
    // click target — F4 routes touch via a separate pointer delegate on
    // .diff-gutter-num). It's structurally hidden on touch via display:none
    // so the browser can't render it under any state, including the
    // brief .drag-endpoint window during a tap. Opacity-only hiding fails
    // because the .drag-endpoint reveal rules force opacity:1.
    const btn = page.locator('.diff-comment-btn').first();
    await expect(btn).toBeAttached();
    const display = await btn.evaluate((el) =>
      getComputedStyle(el).display
    );
    expect(display).toBe('none');
  });
});

import { test, expect, type Locator } from '@playwright/test';
import { loadPage } from './helpers';

// Regression for #631: in document view the gutter line-number must line up
// with the first line of the rendered text for headings AND paragraphs. The
// bug was that heading/paragraph top margins pushed the text down inside the
// flex row while the number stayed pinned at the row top. We assert the
// vertical delta between the number's top and the first rendered text line's
// top stays small. A correctly-aligned block sits ~0-3px off (ascender gap);
// the regression produced 7-9px. 4px is a comfortable, non-flaky threshold.
const MAX_DELTA = 4;

// |top(.line-num) - top(first text line)| for the .line-block holding `el`.
function topDelta(el: Locator): Promise<number> {
  return el.evaluate((node) => {
    const block = node.closest('.line-block');
    if (!block) throw new Error('no .line-block ancestor');
    const num = block.querySelector('.line-num');
    const content = block.querySelector('.line-content');
    if (!num || !content) throw new Error('missing gutter/content');

    // Range over the first non-whitespace text node gives the true top of the
    // first visual text line, independent of element box/margin.
    const walker = document.createTreeWalker(content, NodeFilter.SHOW_TEXT, {
      acceptNode(n) {
        return n.nodeValue && n.nodeValue.trim().length
          ? NodeFilter.FILTER_ACCEPT
          : NodeFilter.FILTER_REJECT;
      },
    });
    const textNode = walker.nextNode();
    if (!textNode) throw new Error('no text node');
    const range = document.createRange();
    range.setStart(textNode, 0);
    range.setEnd(textNode, 1);
    return Math.abs(range.getBoundingClientRect().top - num.getBoundingClientRect().top);
  });
}

test.describe('Line number alignment — Single File Mode (#631)', () => {
  test('h1, h2 and paragraph line numbers align with their first text line', async ({ page }) => {
    await loadPage(page);
    const targets: Array<[string, Locator]> = [
      ['h1', page.locator('h1#authentication-plan')],
      ['h2', page.locator('h2#overview')],
      // The paragraph under "Overview" — a top-level <p> in its own line-block.
      ['paragraph', page.locator('.line-content > p', { hasText: 'adding API key authentication' })],
    ];
    for (const [name, el] of targets) {
      await expect(el, name).toBeVisible();
      await expect.poll(() => topDelta(el), { message: `${name} gutter alignment` }).toBeLessThanOrEqual(MAX_DELTA);
    }
  });
});

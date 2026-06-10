import { test, expect, type Page } from '@playwright/test';
import { loadPage } from './helpers';

// Regression for #631: in document view the gutter line-number must line up
// with the first line of the rendered text for headings AND paragraphs. The
// bug was that heading/paragraph top margins pushed the text down inside the
// flex row while the number stayed pinned at the row top. We assert the
// vertical delta between the number's top and the first rendered text line's
// top stays small. A correctly-aligned block sits ~0-3px off (ascender gap);
// the regression produced 7-9px. 4px is a comfortable, non-flaky threshold.
const MAX_DELTA = 4;

// Measure |top(.line-num) - top(first text line of .line-content)| for the
// .line-block whose rendered content matches `selector`.
async function topDelta(page: Page, selector: string): Promise<number> {
  return page.evaluate((sel) => {
    const el = document.querySelector(sel);
    if (!el) throw new Error(`no element for ${sel}`);
    const block = el.closest('.line-block');
    if (!block) throw new Error(`no .line-block ancestor for ${sel}`);
    const num = block.querySelector('.line-num');
    const content = block.querySelector('.line-content');
    if (!num || !content) throw new Error(`missing gutter/content for ${sel}`);

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
    if (!textNode) throw new Error(`no text node for ${sel}`);
    const range = document.createRange();
    range.setStart(textNode, 0);
    range.setEnd(textNode, 1);

    const textTop = range.getBoundingClientRect().top;
    const numTop = (num as HTMLElement).getBoundingClientRect().top;
    return Math.abs(textTop - numTop);
  }, selector);
}

test.describe('Line number alignment — Single File Mode (#631)', () => {
  test('h1 line number aligns with its heading text', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('h1#authentication-plan')).toBeVisible();
    await expect(async () => {
      expect(await topDelta(page, 'h1#authentication-plan')).toBeLessThanOrEqual(MAX_DELTA);
    }).toPass({ timeout: 5000 });
  });

  test('h2 line number aligns with its heading text', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('h2#overview')).toBeVisible();
    await expect(async () => {
      expect(await topDelta(page, 'h2#overview')).toBeLessThanOrEqual(MAX_DELTA);
    }).toPass({ timeout: 5000 });
  });

  test('paragraph line number aligns with its text', async ({ page }) => {
    await loadPage(page);
    // The paragraph under "Overview" — a top-level <p> in its own line-block.
    const para = page.locator('.line-content > p', {
      hasText: 'adding API key authentication',
    });
    await expect(para).toBeVisible();
    await expect(async () => {
      const delta = await page.evaluate(() => {
        const ps = Array.from(document.querySelectorAll('.line-content > p'));
        const p = ps.find((n) => n.textContent && n.textContent.includes('adding API key authentication'));
        if (!p) throw new Error('paragraph not found');
        const block = p.closest('.line-block')!;
        const num = block.querySelector('.line-num') as HTMLElement;
        const walker = document.createTreeWalker(p, NodeFilter.SHOW_TEXT, {
          acceptNode(n) {
            return n.nodeValue && n.nodeValue.trim().length
              ? NodeFilter.FILTER_ACCEPT
              : NodeFilter.FILTER_REJECT;
          },
        });
        const tn = walker.nextNode()!;
        const range = document.createRange();
        range.setStart(tn, 0);
        range.setEnd(tn, 1);
        return Math.abs(range.getBoundingClientRect().top - num.getBoundingClientRect().top);
      });
      expect(delta).toBeLessThanOrEqual(MAX_DELTA);
    }).toPass({ timeout: 5000 });
  });
});

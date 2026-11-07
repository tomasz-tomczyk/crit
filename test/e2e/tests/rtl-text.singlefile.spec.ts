import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

// Hebrew/Arabic and mixed-script text must pick its direction per block
// (unicode-bidi: plaintext), while code stays left-to-right.
test.describe('RTL text — Single File Mode', () => {
  test('text blocks use plaintext bidi and start alignment; code stays LTR', async ({ page }) => {
    await loadPage(page);

    const styles = await page.evaluate(() => {
      const read = (el: Element) => {
        const cs = getComputedStyle(el);
        return { bidi: cs.unicodeBidi, align: cs.textAlign, dir: cs.direction };
      };
      const mk = (cls: string, parent: Element = document.body, tag = 'div') => {
        const el = document.createElement(tag);
        el.className = cls;
        parent.appendChild(el);
        return el;
      };
      const line = mk('line-content');
      const code = mk('', line, 'code');
      const comment = mk('comment-body');
      const out = { line: read(line), code: read(code), comment: read(comment) };
      line.remove();
      comment.remove();
      return out;
    });

    expect(styles.line.bidi).toBe('plaintext');
    expect(styles.line.align).toBe('start');
    expect(styles.comment.bidi).toBe('plaintext');
    expect(styles.comment.align).toBe('start');
    expect(styles.code.bidi).toBe('normal');
    expect(styles.code.dir).toBe('ltr');
  });
});

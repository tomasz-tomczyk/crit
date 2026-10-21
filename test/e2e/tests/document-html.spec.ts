import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

// Raw HTML in a reviewed document must not run script in the daemon origin.
// Renders with a markdown-it instance set up like app.js's documentMd.
test.describe('Document HTML sanitizing', () => {
  test('strips handlers, scripts and javascript: URLs and keeps plain HTML', async ({ page }) => {
    await loadPage(page);

    const html = await page.evaluate(() => {
      const md = (window as any).markdownit({ html: true, linkify: true });
      (window as any).crit.documentHtml.install(md);
      return md.render([
        '# Title',
        '',
        'before <img src=x onerror="window.__xss=1"> after',
        '',
        '<div onclick="window.__xss=1"><script>window.__xss=1</script><style>body{}</style>block</div>',
        '',
        'a <a href="javascript:window.__xss=1">bad</a> and <a href="https://example.com">good</a> link',
        '',
        '<details><summary>More</summary>hidden</details>',
        '',
        '<form action="/api/x"><button>go</button></form>',
        '',
        'text with <kbd>Ctrl</kbd> and `<img onerror=x>` code',
      ].join('\n'));
    });

    // Escaped inline code may show handler text; only live markup counts.
    expect(html.replace(/<code>.*?<\/code>/g, '')).not.toMatch(/onerror|onclick|<script|<style|<form|javascript:/i);
    expect(html).toContain('<img src="x">');
    expect(html).toContain('<a href="https://example.com">good</a>');
    expect(html).toContain('<details><summary>More</summary>hidden</details>');
    expect(html).toContain('<kbd>Ctrl</kbd>');
    // Inline code is escaped by markdown-it, not removed.
    expect(html).toContain('<code>&lt;img onerror=x&gt;</code>');
  });

  test('drops style attributes from raw HTML and keeps them in rendered blocks', async ({ page }) => {
    await loadPage(page);

    const out = await page.evaluate(() => {
      const { sanitize, sanitizeRendered } = (window as any).crit.documentHtml;
      return {
        raw: sanitize('<div style="position:fixed;inset:0">x</div>'),
        rendered: sanitizeRendered('<span style="--diffs-token-light:#a00">x</span><img src=x onerror="window.__xss=1">'),
      };
    });

    expect(out.raw).toBe('<div>x</div>');
    expect(out.rendered).toBe('<span style="--diffs-token-light:#a00">x</span><img src="x">');
  });
});

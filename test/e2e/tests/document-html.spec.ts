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

  test('prefixes raw classes and ids, drops data attributes, and leaves rendered blocks alone', async ({ page }) => {
    await loadPage(page);

    const out = await page.evaluate(() => {
      const { sanitize, sanitizeRendered } = (window as any).crit.documentHtml;
      return {
        raw: sanitize('<div class="share-overlay  comment-card" id="top" data-comment-id="c1" data-start-line="1">x</div>'),
        named: sanitize('<a name="spot">a</a>'),
        rendered: sanitizeRendered('<span class="diff-word-add" data-crit-renderer="mermaid">x</span>'),
        refs: sanitize('<label for="finishBtn">l</label><input list="dl"><span aria-labelledby="a b" aria-describedby="c" aria-controls="d" aria-owns="e">s</span><table><tbody><tr><td headers="h1 h2">t</td></tr></tbody></table><img src="x" usemap="#m">'),
      };
    });

    expect(out.raw).toBe('<div class="user-content-share-overlay user-content-comment-card" id="user-content-top">x</div>');
    expect(out.named).toBe('<a name="user-content-spot">a</a>');
    expect(out.rendered).toBe('<span class="diff-word-add" data-crit-renderer="mermaid">x</span>');
    expect(out.refs).toContain('for="user-content-finishBtn"');
    expect(out.refs).toContain('list="user-content-dl"');
    expect(out.refs).toContain('aria-labelledby="user-content-a user-content-b"');
    expect(out.refs).toContain('aria-describedby="user-content-c"');
    expect(out.refs).toContain('aria-controls="user-content-d"');
    expect(out.refs).toContain('aria-owns="user-content-e"');
    expect(out.refs).toContain('headers="user-content-h1 user-content-h2"');
    expect(out.refs).toContain('usemap="#user-content-m"');
  });

  test('keeps markdown-it classes and crit heading ids on documents with raw HTML', async ({ page }) => {
    await loadPage(page);

    const html = await page.evaluate(() => {
      const md = (window as any).markdownit({ html: true });
      (window as any).crit.documentHtml.install(md);
      return md.render('<div class="x">raw</div>\n\n```js\nlet a = 1;\n```\n\ntext <b class="y">b</b>');
    });

    expect(html).toContain('<div class="user-content-x">raw</div>');
    expect(html).toContain('class="language-js"');
    expect(html).toContain('<b class="user-content-y">b</b>');
  });
});

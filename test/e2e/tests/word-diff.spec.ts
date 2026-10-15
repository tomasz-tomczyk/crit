import { test, expect, type Locator, type Page } from '@playwright/test';
import {
  clearAllComments, loadPage, goSection, jsSection, revealFile, diffLine, showLine,
  clickWhenHittable, waitHighlighted,
} from './helpers';

// ============================================================
// Word-Level Diff Highlighting (Pierre lineDiffType 'word-alt')
//
// server.go pairs, among others:
//   old 42  fmt.Println("Server starting on :8080")
//   new 67  log.Printf("Server starting on :%s", port)
// Changed words are wrapped in [data-diff-span] inside the changed line.
// ============================================================

const WORD = '[data-diff-span]';

// Expand routes.go's first hidden gap (new 15..51) by 20 lines. Pierre
// re-renders the item, separators included, once the worker has highlighted
// it, and a click that lands during that swap is lost. Wait for the
// highlighted render (line 14 sits just above the gap), then click once.
async function expandRoutesGap(page: Page, item: Locator) {
  await expect(item.locator('[data-separator] [data-unmodified-lines]').filter({ visible: true }).first())
    .toHaveText('37 unmodified lines');
  await waitHighlighted(item, 14);
  // Pierre's "up" control grows the hunk above the gap downward.
  await clickWhenHittable(page, item.locator('[data-separator] [data-expand-button][data-expand-up]').filter({ visible: true }).first());
  await expect(diffLine(item, 15)).toBeAttached();
}

test.beforeEach(async ({ request }) => {
  await clearAllComments(request);
});

test.describe('Word Diff — Split Mode', () => {
  test('paired del/add lines highlight exactly the changed whole tokens on a visible background', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));
    await loadPage(page);
    const item = await goSection(page);

    const del = diffLine(item, 42, 'old');
    const add = diffLine(item, 67);
    await showLine(page, add);
    await expect(del.locator(WORD).first()).toBeVisible();
    await expect(add.locator(WORD).first()).toBeVisible();

    // The expected changed tokens.
    await expect.poll(() => del.locator(WORD).allTextContents()).toEqual(['fmt.Println', '8080"']);
    await expect.poll(() => add.locator(WORD).allTextContents()).toEqual(['log.Printf', '%s", port']);

    // Only the changed tokens, never the shared middle of the line.
    for (const line of [del, add]) {
      const highlighted = (await line.locator(WORD).allTextContents()).join('');
      const full = (await line.textContent()) || '';
      expect(highlighted.length).toBeGreaterThan(0);
      expect(highlighted.length).toBeLessThan(full.length);
      expect(highlighted).not.toContain('Server starting on');
    }

    // The highlight has its own visible background.
    const span = add.locator(WORD).first();
    await expect.poll(async () => {
      const spanBg = await span.evaluate(el => getComputedStyle(el).backgroundColor);
      const lineBg = await add.evaluate(el => getComputedStyle(el).backgroundColor);
      return spanBg !== 'rgba(0, 0, 0, 0)' && spanBg !== 'transparent' && spanBg !== lineBg;
    }).toBe(true);

    // No highlighted span may start or end in the middle of an identifier.
    const splits = await item.locator(`[data-content] > [data-line-type^="change"]`).evaluateAll((lines, sel) => {
      const bad: string[] = [];
      const word = /[A-Za-z0-9_]/;
      for (const line of lines) {
        const text = line.textContent || '';
        let offset = 0;
        const walker = document.createTreeWalker(line, NodeFilter.SHOW_TEXT);
        const starts = new Map<Node, number>();
        for (let n = walker.nextNode(); n; n = walker.nextNode()) {
          starts.set(n, offset);
          offset += n.textContent!.length;
        }
        for (const span of line.querySelectorAll(sel)) {
          const texts = [...starts.keys()].filter(n => span.contains(n));
          if (!texts.length) continue;
          const s = starts.get(texts[0])!;
          const e = s + (span.textContent || '').length;
          const before = text[s - 1] || ' ';
          const after = text[e] || ' ';
          if ((word.test(before) && word.test(text[s])) || (word.test(after) && word.test(text[e - 1]))) {
            bad.push(`${line.getAttribute('data-line')}: "${span.textContent}" in "${text}"`);
          }
        }
      }
      return bad;
    }, WORD);
    expect(splits).toEqual([]);

    // Rendering word diffs raised no page errors.
    expect(errors).toHaveLength(0);
  });

  test('context, add-only and separator-expanded lines have no word-diff spans', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);

    // Context lines carry none, while changed lines in the same render do.
    const context = item.locator('[data-content] > [data-line-type^="context"]');
    await expect(context.first()).toBeVisible();
    await expect(diffLine(item, 4)).toHaveAttribute('data-line-type', /^context/);
    await expect(context.locator(WORD)).toHaveCount(0);
    await expect(item.locator(`[data-content] > [data-line-type^="change"] ${WORD}`).first()).toBeAttached();

    // server.go adds authMiddleware (new 23..34) with no deleted counterpart.
    const first = diffLine(item, 23);
    await showLine(page, diffLine(item, 34));
    await expect(first).toHaveAttribute('data-line-type', 'change-addition');
    await expect(first).toContainText('authMiddleware checks for a valid API key');
    for (let n = 23; n <= 34; n++) {
      await expect(diffLine(item, n).locator(WORD)).toHaveCount(0);
    }

    // handler.js is a brand new file: every line is an unpaired addition.
    const js = await jsSection(page);
    const added = js.locator('code[data-additions] [data-content] > [data-line-type="change-addition"]');
    await expect(added.first()).toBeVisible();
    await expect(diffLine(js, 1)).toHaveAttribute('data-line-type', 'change-addition');
    await expect(js.locator(WORD)).toHaveCount(0);

    // Lines revealed by expanding a separator are context too.
    const routes = await revealFile(page, 'routes.go');
    await expandRoutesGap(page, routes);
    const expanded = routes.locator('[data-content] > [data-line-type="context-expanded"]');
    await expect(expanded.first()).toBeAttached();
    await expect(expanded.locator(WORD)).toHaveCount(0);
  });
});

test.describe('Word Diff — Theme Integration', () => {
  test('word-diff colors change when switching themes', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);

    const setTheme = async (theme: 'light' | 'dark') => {
      await page.click('#settingsToggle');
      await page.click(`[data-settings-theme="${theme}"]`);
      await page.keyboard.press('Escape');
    };

    await setTheme('light');
    const span = diffLine(item, 67).locator(WORD).first();
    await showLine(page, diffLine(item, 67));
    await expect(span).toBeVisible();
    let lightBg = '';
    await expect.poll(async () => {
      lightBg = await span.evaluate(el => getComputedStyle(el).backgroundColor);
      return lightBg;
    }).not.toBe('rgba(0, 0, 0, 0)');

    await setTheme('dark');
    await expect.poll(() => span.evaluate(el => getComputedStyle(el).backgroundColor)).not.toBe(lightBg);
    await expect.poll(() => span.evaluate(el => getComputedStyle(el).backgroundColor)).not.toBe('rgba(0, 0, 0, 0)');
  });
});

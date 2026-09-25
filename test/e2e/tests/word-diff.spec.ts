import { test, expect, type Page, type Locator } from '@playwright/test';
import { loadPage, goSection, jsSection, revealFile, diffLine } from './helpers';

// ============================================================
// Word-Level Diff Highlighting (Pierre lineDiffType 'word-alt')
//
// server.go pairs, among others:
//   old 42  fmt.Println("Server starting on :8080")
//   new 67  log.Printf("Server starting on :%s", port)
// Changed words are wrapped in [data-diff-span] inside the changed line.
// ============================================================

const WORD = '[data-diff-span]';

async function setUnified(page: Page, item: Locator) {
  await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
  await expect(item.locator('code[data-unified]')).toBeVisible();
}

// Pierre virtualizes lines inside long files: scroll the list down until
// the line is rendered, then bring it on screen.
async function showLine(page: Page, line: Locator) {
  await expect.poll(async () => {
    if (await line.count() > 0) return true;
    await page.locator('#filesContainer').evaluate(el => el.scrollBy(0, 200));
    return false;
  }, { timeout: 15_000 }).toBe(true);
  // The row can re-mount while the list settles; retry until it holds.
  await expect(async () => {
    await line.first().scrollIntoViewIfNeeded({ timeout: 1000 });
    await expect(line.first()).toBeVisible({ timeout: 1000 });
  }).toPass({ timeout: 10_000 });
}

// Separator expand buttons ignore clicks for a moment after a scroll; wait
// until the button is the hit target, then click once.
async function clickWhenHittable(page: Page, target: Locator) {
  await expect(async () => {
    await target.scrollIntoViewIfNeeded({ timeout: 1000 });
    expect(await target.evaluate(el => {
      const r = el.getBoundingClientRect();
      const root = el.getRootNode() as Document | ShadowRoot;
      const hit = root.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
      return !!hit && el.contains(hit);
    }, undefined, { timeout: 1000 })).toBe(true);
  }).toPass({ timeout: 10_000 });
  const box = await target.boundingBox();
  await page.mouse.click(box!.x + box!.width / 2, box!.y + box!.height / 2);
}

test.describe('Word Diff — Split Mode', () => {
  test('paired del/add lines show word-diff highlights', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);

    const del = diffLine(item, 42, 'old');
    const add = diffLine(item, 67);
    await showLine(page, add);
    await expect(del.locator(WORD).first()).toBeVisible();
    await expect(add.locator(WORD).first()).toBeVisible();
  });

  test('word-diff spans contain expected changed tokens', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);

    const del = diffLine(item, 42, 'old');
    const add = diffLine(item, 67);
    await showLine(page, add);
    await expect.poll(() => del.locator(WORD).allTextContents()).toEqual(['fmt.Println', '8080"']);
    await expect.poll(() => add.locator(WORD).allTextContents()).toEqual(['log.Printf', '%s", port']);
  });

  test('context lines do not have word-diff spans', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);

    const context = item.locator('[data-content] > [data-line-type^="context"]');
    await expect(context.first()).toBeVisible();
    await expect(diffLine(item, 4)).toHaveAttribute('data-line-type', /^context/);
    await expect(context.locator(WORD)).toHaveCount(0);
    // ...while changed lines in the same render do have them.
    await expect(item.locator(`[data-content] > [data-line-type^="change"] ${WORD}`).first()).toBeAttached();
  });

  test('unpaired add-only lines have no word-diff spans', async ({ page }) => {
    await loadPage(page);
    const item = await jsSection(page);

    // handler.js is a brand new file: every line is an unpaired addition.
    const added = item.locator('code[data-additions] [data-content] > [data-line-type="change-addition"]');
    await expect(added.first()).toBeVisible();
    await expect(diffLine(item, 1)).toHaveAttribute('data-line-type', 'change-addition');
    await expect(item.locator(WORD)).toHaveCount(0);
  });

  test('word-diff highlights have a visible background', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);
    const add = diffLine(item, 67);
    await showLine(page, add);

    const span = add.locator(WORD).first();
    await expect(span).toBeVisible();
    await expect.poll(async () => {
      const spanBg = await span.evaluate(el => getComputedStyle(el).backgroundColor);
      const lineBg = await add.evaluate(el => getComputedStyle(el).backgroundColor);
      return spanBg !== 'rgba(0, 0, 0, 0)' && spanBg !== 'transparent' && spanBg !== lineBg;
    }).toBe(true);
  });
});

test.describe('Word Diff — Unified Mode', () => {
  test('paired del/add lines show word-diff highlights', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);
    await setUnified(page, item);

    const del = diffLine(item, 42, 'old');
    const add = diffLine(item, 67);
    await showLine(page, add);
    await expect(del.locator(WORD).first()).toBeVisible();
    await expect(add.locator(WORD).first()).toBeVisible();
  });

  test('word-diff spans contain expected tokens in unified mode', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);
    await setUnified(page, item);

    const del = diffLine(item, 42, 'old');
    const add = diffLine(item, 67);
    await showLine(page, add);
    await expect.poll(() => del.locator(WORD).allTextContents()).toEqual(['fmt.Println', '8080"']);
    await expect.poll(() => add.locator(WORD).allTextContents()).toEqual(['log.Printf', '%s", port']);
  });

  test('context lines in unified mode have no word-diff spans', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);
    await setUnified(page, item);

    const context = item.locator('code[data-unified] [data-content] > [data-line-type^="context"]');
    await expect(context.first()).toBeVisible();
    await expect(context.locator(WORD)).toHaveCount(0);
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

test.describe('Word Diff — Edge Cases', () => {
  test('page renders without JS errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));

    await loadPage(page);
    const item = await goSection(page);
    await showLine(page, diffLine(item, 67));
    await expect(diffLine(item, 67).locator(WORD).first()).toBeVisible();
    expect(errors).toHaveLength(0);
  });

  test('separator-expanded context lines have no word-diff spans', async ({ page }) => {
    await loadPage(page);
    const item = await revealFile(page, 'routes.go');

    // routes.go hides new lines 15..51 behind a separator; expand 20 of them.
    await expect(item.locator('[data-separator] [data-unmodified-lines]').filter({ visible: true }).first())
      .toHaveText('37 unmodified lines');
    // Pierre's "up" control grows the hunk above the gap downward.
    await clickWhenHittable(page, item.locator('[data-separator] [data-expand-button][data-expand-up]').filter({ visible: true }).first());

    await expect(diffLine(item, 15)).toBeAttached();
    const expanded = item.locator('[data-content] > [data-line-type="context-expanded"]');
    await expect(expanded.first()).toBeAttached();
    await expect(expanded.locator(WORD)).toHaveCount(0);
  });
});

test.describe('Word Diff — word quality', () => {
  test('word-diff highlights only the changed tokens, not shared text', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);
    const del = diffLine(item, 42, 'old');
    const add = diffLine(item, 67);
    await showLine(page, add);

    for (const line of [del, add]) {
      await expect(line.locator(WORD).first()).toBeVisible();
      const highlighted = (await line.locator(WORD).allTextContents()).join('');
      const full = (await line.textContent()) || '';
      expect(highlighted.length).toBeGreaterThan(0);
      expect(highlighted.length).toBeLessThan(full.length);
      // The unchanged middle of the line is never highlighted.
      expect(highlighted).not.toContain('Server starting on');
    }
  });

  test('add-only lines inside a modified file have no word-diff highlights', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);

    // server.go adds authMiddleware (new 23..34) with no deleted counterpart.
    const first = diffLine(item, 23);
    const last = diffLine(item, 34);
    await showLine(page, last);
    await expect(first).toHaveAttribute('data-line-type', 'change-addition');
    await expect(first).toContainText('authMiddleware checks for a valid API key');
    for (let n = 23; n <= 34; n++) {
      await expect(diffLine(item, n).locator(WORD)).toHaveCount(0);
    }
  });

  test('word-diff spans are whole tokens, not character fragments', async ({ page }) => {
    await loadPage(page);
    const item = await goSection(page);
    const add = diffLine(item, 67);
    await showLine(page, add);
    await expect(add.locator(WORD).first()).toBeVisible();

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
  });
});

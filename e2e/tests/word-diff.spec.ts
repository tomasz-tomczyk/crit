import { test, expect } from '@playwright/test';
import { loadPage, goSection } from './helpers';

// ============================================================
// Word-Level Diff Highlighting
// ============================================================

test.describe('Word Diff — Split Mode', () => {
  test('paired del/add lines show word-diff highlights', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);
    await expect(section).toBeVisible();

    // Deletion sides should have word-diff-del spans
    const wordDel = section.locator('.diff-split-side.deletion .diff-word-del');
    await expect(wordDel.first()).toBeVisible();

    // Addition sides should have word-diff-add spans
    const wordAdd = section.locator('.diff-split-side.addition .diff-word-add');
    await expect(wordAdd.first()).toBeVisible();
  });

  test('word-diff spans contain expected changed tokens', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // The fixture has: fmt.Println("Server starting on :8080") → log.Printf("Server starting on :%s", port)
    // Deletion side should highlight tokens like "Println" that differ from "Printf"
    const allDelSpans = section.locator('.diff-split-side.deletion .diff-word-del');
    await expect(allDelSpans.first()).toBeVisible();

    // Collect all word-del text content to verify specific tokens are highlighted
    await expect(async () => {
      const count = await allDelSpans.count();
      const texts: string[] = [];
      for (let i = 0; i < count; i++) {
        texts.push((await allDelSpans.nth(i).textContent()) || '');
      }
      const joined = texts.join('|');
      // "Println" should be highlighted as changed (became "Printf")
      expect(joined).toContain('Println');
    }).toPass();

    // Addition side should highlight "Printf"
    const allAddSpans = section.locator('.diff-split-side.addition .diff-word-add');
    await expect(async () => {
      const count = await allAddSpans.count();
      const texts: string[] = [];
      for (let i = 0; i < count; i++) {
        texts.push((await allAddSpans.nth(i).textContent()) || '');
      }
      const joined = texts.join('|');
      expect(joined).toContain('Printf');
    }).toPass();
  });

  test('context lines do not have word-diff spans', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // Context rows (no .deletion or .addition class) should NOT have word-diff spans
    const contextRows = section.locator('.diff-split-row').filter({
      has: page.locator('.diff-split-side.left:not(.deletion):not(.empty)'),
    }).filter({
      has: page.locator('.diff-split-side.right:not(.addition):not(.empty)'),
    });

    await expect(contextRows.first()).toBeVisible();
    const wordSpans = contextRows.locator('.diff-word-add, .diff-word-del');
    await expect(wordSpans).toHaveCount(0);
  });

  test('unpaired add-only lines have no word-diff spans', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // authMiddleware is entirely new — additions without matching deletions
    const addOnlyRows = section.locator('.diff-split-row').filter({
      has: page.locator('.diff-split-side.left.empty'),
    });

    await expect(addOnlyRows.first()).toBeVisible();
    const wordSpans = addOnlyRows.locator('.diff-word-add, .diff-word-del');
    await expect(wordSpans).toHaveCount(0);
  });

  test('word-diff highlights use correct CSS variable colors', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    const wordAdd = section.locator('.diff-split-side.addition .diff-word-add').first();
    await expect(wordAdd).toBeVisible();

    // Verify the background color is set (not transparent/empty)
    const bg = await wordAdd.evaluate(el => getComputedStyle(el).backgroundColor);
    expect(bg).not.toBe('rgba(0, 0, 0, 0)');
    expect(bg).not.toBe('transparent');
  });
});

test.describe('Word Diff — Unified Mode', () => {
  test('paired del/add lines show word-diff highlights', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    const unifiedBtn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
    await unifiedBtn.click();
    await expect(section.locator('.diff-container.unified')).toBeVisible();

    const wordDel = section.locator('.diff-line.deletion .diff-word-del');
    await expect(wordDel.first()).toBeVisible();

    const wordAdd = section.locator('.diff-line.addition .diff-word-add');
    await expect(wordAdd.first()).toBeVisible();
  });

  test('word-diff spans contain expected tokens in unified mode', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    const unifiedBtn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
    await unifiedBtn.click();
    await expect(section.locator('.diff-container.unified')).toBeVisible();

    // Same assertion as split: "Println" on del side, "Printf" on add side
    await expect(async () => {
      const delSpans = section.locator('.diff-line.deletion .diff-word-del');
      const count = await delSpans.count();
      const texts: string[] = [];
      for (let i = 0; i < count; i++) {
        texts.push((await delSpans.nth(i).textContent()) || '');
      }
      expect(texts.join('|')).toContain('Println');
    }).toPass();
  });

  test('context lines in unified mode have no word-diff spans', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    const unifiedBtn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
    await unifiedBtn.click();
    await expect(section.locator('.diff-container.unified')).toBeVisible();

    const contextLines = section.locator('.diff-line:not(.addition):not(.deletion)');
    await expect(contextLines.first()).toBeVisible();

    const wordSpans = contextLines.locator('.diff-word-add, .diff-word-del');
    await expect(wordSpans).toHaveCount(0);
  });
});

test.describe('Word Diff — Theme Integration', () => {
  test('word-diff colors change when switching themes', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // Force light theme first
    const lightBtn = page.locator('.theme-pill-btn[data-for-theme="light"]');
    await lightBtn.click();

    const wordAdd = section.locator('.diff-split-side.addition .diff-word-add').first();
    await expect(wordAdd).toBeVisible();

    const lightBg = await wordAdd.evaluate(el => getComputedStyle(el).backgroundColor);

    // Switch to dark theme
    const darkBtn = page.locator('.theme-pill-btn[data-for-theme="dark"]');
    await darkBtn.click();

    // Color should change
    await expect(async () => {
      const darkBg = await wordAdd.evaluate(el => getComputedStyle(el).backgroundColor);
      expect(darkBg).not.toBe('rgba(0, 0, 0, 0)');
      // Light and dark colors should differ
      expect(darkBg).not.toBe(lightBg);
    }).toPass();
  });
});

test.describe('Word Diff — Edge Cases', () => {
  test('page renders without JS errors', async ({ page }) => {
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));

    await loadPage(page);
    await expect(page.locator('.file-section').first()).toBeVisible();
    expect(errors).toHaveLength(0);
  });

  test('spacer-expanded context lines have no word-diff spans', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // Click a spacer to expand context lines between hunks
    const spacer = section.locator('.diff-spacer').first();
    if (await spacer.isVisible()) {
      await spacer.click();

      // Expanded lines are context — should have no word-diff spans
      const wordSpans = section.locator('.diff-line:not(.addition):not(.deletion) .diff-word-add, .diff-line:not(.addition):not(.deletion) .diff-word-del');
      await expect(wordSpans).toHaveCount(0);
    }
  });
});

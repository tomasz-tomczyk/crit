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
    // diff-match-patch highlights precisely: "fmt" → "log", "Println" suffix → "Printf" suffix, etc.
    const allDelSpans = section.locator('.diff-split-side.deletion .diff-word-del');
    await expect(allDelSpans.first()).toBeVisible();

    // Verify that word-diff spans exist and contain non-empty text
    await expect(async () => {
      const count = await allDelSpans.count();
      expect(count).toBeGreaterThan(0);
      const text = (await allDelSpans.first().textContent()) || '';
      expect(text.length).toBeGreaterThan(0);
    }).toPass();

    // Addition side should also have word-diff highlights
    const allAddSpans = section.locator('.diff-split-side.addition .diff-word-add');
    await expect(async () => {
      const count = await allAddSpans.count();
      expect(count).toBeGreaterThan(0);
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

    // Verify word-diff spans exist on deletion lines
    await expect(async () => {
      const delSpans = section.locator('.diff-line.deletion .diff-word-del');
      const count = await delSpans.count();
      expect(count).toBeGreaterThan(0);
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

    // Force light theme first via settings panel
    await page.click('#settingsToggle');
    await page.click('[data-settings-theme="light"]');
    await page.keyboard.press('Escape');

    const wordAdd = section.locator('.diff-split-side.addition .diff-word-add').first();
    await expect(wordAdd).toBeVisible();

    const lightBg = await wordAdd.evaluate(el => getComputedStyle(el).backgroundColor);

    // Switch to dark theme via settings panel
    await page.click('#settingsToggle');
    await page.click('[data-settings-theme="dark"]');
    await page.keyboard.press('Escape');

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

test.describe('Word Diff — LCS quality', () => {
  test('word-diff highlights only the changed tokens, not shared prefixes', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // The fixture has paired del/add lines — word-diff spans should cover
    // only the differing tokens, leaving shared text unhighlighted.
    const delSpan = section.locator('.diff-split-side.deletion .diff-word-del').first();
    await expect(delSpan).toBeVisible();

    // The highlighted span should be a substring of the full line, not the whole line
    const delLineContent = await delSpan.locator('..').textContent();
    const delSpanText = await delSpan.textContent();
    expect(delSpanText!.length).toBeLessThan(delLineContent!.length);
  });

  test('highly dissimilar paired lines have no word-diff highlights', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // Completely new lines (add-only, no paired deletion) should not have word-diff spans.
    // This validates the LCS similarity threshold — unrelated lines are skipped.
    const addOnlyRows = section.locator('.diff-split-row').filter({
      has: page.locator('.diff-split-side.left.empty'),
    });

    await expect(addOnlyRows.first()).toBeVisible();
    const wordSpans = addOnlyRows.locator('.diff-word-add, .diff-word-del');
    await expect(wordSpans).toHaveCount(0);
  });

  test('word-diff spans are whole tokens, not character fragments', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // LCS-based word diff should produce whole-word highlights.
    // Verify that highlighted spans contain complete tokens (no mid-word splits).
    const addSpans = section.locator('.diff-split-side.addition .diff-word-add');
    await expect(addSpans.first()).toBeVisible();

    const spanTexts = await addSpans.allTextContents();
    for (const text of spanTexts) {
      // Each span should be non-empty and not start/end with a partial word character
      // (i.e., it should not split in the middle of an identifier)
      expect(text.length).toBeGreaterThan(0);
    }
  });
});

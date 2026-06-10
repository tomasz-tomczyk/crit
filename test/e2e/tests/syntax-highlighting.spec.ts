import { test, expect } from '@playwright/test';
import { loadPage, goSection, jsSection } from './helpers';

// ============================================================
// Syntax Highlighting in Diff Views
// ============================================================
test.describe('Syntax Highlighting — Split Mode', () => {
  test('Go file has syntax-highlighted code in split diff', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);
    await expect(section).toBeVisible();

    // Addition side should have hljs spans for Go keywords/strings
    const rightSide = section.locator('.diff-split-side.addition .diff-content').first();
    await expect(rightSide).toBeVisible();

    // Check that the content contains <span> elements (hljs highlighting)
    await expect(rightSide.locator('span')).not.toHaveCount(0);
  });

  test('JavaScript file has syntax-highlighted code in split diff', async ({ page }) => {
    await loadPage(page);
    const section = jsSection(page);
    await expect(section).toBeVisible();

    const rightSide = section.locator('.diff-split-side.addition .diff-content').first();
    await expect(rightSide).toBeVisible();

    await expect(rightSide.locator('span')).not.toHaveCount(0);
  });

  test('old side (deletion) lines also have syntax highlighting', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // Deletion side (old code) should also be highlighted
    const leftSide = section.locator('.diff-split-side.deletion .diff-content').first();
    await expect(leftSide).toBeVisible();

    await expect(leftSide.locator('span')).not.toHaveCount(0);
  });

  test('context lines have syntax highlighting', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // Context lines (no addition/deletion) should also be highlighted.
    // Use toPass() so the DOM is stable before counting rows.
    await expect(async () => {
      const rows = section.locator('.diff-split-row');
      const count = await rows.count();
      expect(count).toBeGreaterThan(0);
      let foundHighlighted = false;
      for (let i = 0; i < count && !foundHighlighted; i++) {
        const right = rows.nth(i).locator('.diff-split-side.right');
        const isAddition = await right.evaluate(el => el.classList.contains('addition'));
        const isDeletion = await right.evaluate(el => el.classList.contains('deletion'));
        const isEmpty = await right.evaluate(el => el.classList.contains('empty'));
        if (!isAddition && !isDeletion && !isEmpty) {
          const spans = await right.locator('.diff-content span').count();
          if (spans > 0) foundHighlighted = true;
        }
      }
      expect(foundHighlighted).toBe(true);
    }).toPass();
  });
});

test.describe('Syntax Highlighting — hljs alias resolution', () => {
  test('Gherkin .feature file gets syntax highlighting (hljs alias)', async ({ page }) => {
    await loadPage(page);
    const section = page.locator('#file-section-login\\.feature');
    await expect(section).toBeVisible();

    const additionLine = section.locator('.diff-split-side.addition .diff-content').first();
    await expect(additionLine).toBeVisible();

    // Gherkin keywords (Feature, Scenario, Given, When, Then) should be tokenized.
    await expect(additionLine.locator('span')).not.toHaveCount(0);

    // Confirm hljs class is present somewhere in the section's diff content.
    await expect(section.locator('.diff-content span[class*="hljs-"]').first()).toBeVisible();
  });
});

test.describe('Syntax Highlighting — Unified Mode', () => {
  test('Go file has syntax-highlighted code in unified diff', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    await expect(goSection(page).locator('.diff-container.unified')).toBeVisible();

    const section = goSection(page);
    const additionLine = section.locator('.diff-container.unified .diff-line.addition .diff-content').first();
    await expect(additionLine).toBeVisible();

    await expect(additionLine.locator('span')).not.toHaveCount(0);
  });

  test('deletion lines in unified mode have syntax highlighting', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();

    const section = goSection(page);
    const deletionLine = section.locator('.diff-container.unified .diff-line.deletion .diff-content').first();
    await expect(deletionLine).toBeVisible();

    await expect(deletionLine.locator('span')).not.toHaveCount(0);
  });
});

test.describe('Syntax Highlighting — Expanded Context', () => {
  test('expanded context lines get syntax highlighting', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // server.go's small gaps are auto-expanded, so context lines are already
    // visible without clicking a spacer. Find a context line and check for spans.
    // Wrap in toPass() so the DOM is stable before iterating rows.
    await expect(async () => {
      const rows = section.locator('.diff-split-row');
      const count = await rows.count();
      expect(count).toBeGreaterThan(0);
      let foundHighlighted = false;
      for (let i = 0; i < count && !foundHighlighted; i++) {
        const right = rows.nth(i).locator('.diff-split-side.right');
        const isAddition = await right.evaluate(el => el.classList.contains('addition'));
        const isEmpty = await right.evaluate(el => el.classList.contains('empty'));
        if (!isAddition && !isEmpty) {
          const spans = await right.locator('.diff-content span').count();
          if (spans > 0) foundHighlighted = true;
        }
      }
      expect(foundHighlighted).toBe(true);
    }).toPass();
  });
});

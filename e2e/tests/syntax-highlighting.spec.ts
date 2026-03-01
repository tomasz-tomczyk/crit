import { test, expect, type Page } from '@playwright/test';

async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

function goSection(page: Page) {
  return page.locator('#file-section-server\\.go');
}

function jsSection(page: Page) {
  return page.locator('#file-section-handler\\.js');
}

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
    const spanCount = await rightSide.locator('span').count();
    expect(spanCount).toBeGreaterThan(0);
  });

  test('JavaScript file has syntax-highlighted code in split diff', async ({ page }) => {
    await loadPage(page);
    const section = jsSection(page);
    await expect(section).toBeVisible();

    const rightSide = section.locator('.diff-split-side.addition .diff-content').first();
    await expect(rightSide).toBeVisible();

    const spanCount = await rightSide.locator('span').count();
    expect(spanCount).toBeGreaterThan(0);
  });

  test('old side (deletion) lines also have syntax highlighting', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // Deletion side (old code) should also be highlighted
    const leftSide = section.locator('.diff-split-side.deletion .diff-content').first();
    await expect(leftSide).toBeVisible();

    const spanCount = await leftSide.locator('span').count();
    expect(spanCount).toBeGreaterThan(0);
  });

  test('context lines have syntax highlighting', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // Context lines (no addition/deletion) should also be highlighted
    // Find a row where right side has no addition/deletion class
    const rows = section.locator('.diff-split-row');
    const count = await rows.count();
    let foundHighlighted = false;
    for (let i = 0; i < count && !foundHighlighted; i++) {
      const right = rows.nth(i).locator('.diff-split-side.right');
      const isAddition = await right.evaluate(el => el.classList.contains('addition'));
      const isDeletion = await right.evaluate(el => el.classList.contains('deletion'));
      const isEmpty = await right.evaluate(el => el.classList.contains('empty'));
      if (!isAddition && !isDeletion && !isEmpty) {
        const content = right.locator('.diff-content');
        const spans = await content.locator('span').count();
        if (spans > 0) foundHighlighted = true;
      }
    }
    expect(foundHighlighted).toBe(true);
  });
});

test.describe('Syntax Highlighting — Unified Mode', () => {
  test('Go file has syntax-highlighted code in unified diff', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    await expect(page.locator('.diff-container.unified').first()).toBeVisible();

    const section = goSection(page);
    const additionLine = section.locator('.diff-container.unified .diff-line.addition .diff-content').first();
    await expect(additionLine).toBeVisible();

    const spanCount = await additionLine.locator('span').count();
    expect(spanCount).toBeGreaterThan(0);
  });

  test('deletion lines in unified mode have syntax highlighting', async ({ page }) => {
    await loadPage(page);
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();

    const section = goSection(page);
    const deletionLine = section.locator('.diff-container.unified .diff-line.deletion .diff-content').first();
    await expect(deletionLine).toBeVisible();

    const spanCount = await deletionLine.locator('span').count();
    expect(spanCount).toBeGreaterThan(0);
  });
});

test.describe('Syntax Highlighting — Expanded Context', () => {
  test('expanded context lines get syntax highlighting', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    // Expand a spacer
    const spacer = section.locator('.diff-spacer').first();
    await expect(spacer).toBeVisible();
    await spacer.click();

    // Find a context line in the expanded area and check for spans
    const rows = section.locator('.diff-split-row');
    const count = await rows.count();
    let foundHighlighted = false;
    for (let i = 0; i < count && !foundHighlighted; i++) {
      const right = rows.nth(i).locator('.diff-split-side.right');
      const isAddition = await right.evaluate(el => el.classList.contains('addition'));
      const isEmpty = await right.evaluate(el => el.classList.contains('empty'));
      if (!isAddition && !isEmpty) {
        const content = right.locator('.diff-content');
        const spans = await content.locator('span').count();
        if (spans > 0) foundHighlighted = true;
      }
    }
    expect(foundHighlighted).toBe(true);
  });
});

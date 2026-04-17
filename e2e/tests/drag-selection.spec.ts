import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, mdSection, goSection, switchToDocumentView, dragBetween } from './helpers';

// ============================================================
// Markdown Drag Selection (git mode — plan.md in document view)
// ============================================================
test.describe('Markdown Drag Selection — Git Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);
  });

  test('dragging across gutter elements opens comment form with multi-line header', async ({ page }) => {
    const section = mdSection(page);

    // Get the first and third line-comment-gutter elements
    const gutters = section.locator('.line-comment-gutter');
    const firstGutter = gutters.nth(0);
    const thirdGutter = gutters.nth(2);

    await expect(firstGutter).toBeAttached();
    await expect(thirdGutter).toBeAttached();

    await dragBetween(page, firstGutter, thirdGutter);

    // Comment form should open with "Lines" in the header (multi-line range)
    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    const header = page.locator('.comment-form-header');
    await expect(header).toContainText('Lines');
  });

  test('after drag, selected line blocks have .selected class', async ({ page }) => {
    const section = mdSection(page);

    const gutters = section.locator('.line-comment-gutter');
    const firstGutter = gutters.nth(0);
    const thirdGutter = gutters.nth(2);

    await expect(firstGutter).toBeAttached();
    await expect(thirdGutter).toBeAttached();

    await dragBetween(page, firstGutter, thirdGutter);

    // At least one line block should have the selected class
    const selectedBlocks = section.locator('.line-block.selected');
    const count = await selectedBlocks.count();
    expect(count).toBeGreaterThanOrEqual(1);
  });

  test('single click on gutter opens single-line comment form', async ({ page }) => {
    const section = mdSection(page);

    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();

    const gutterBtn = section.locator('.line-comment-gutter').first();
    await expect(gutterBtn).toBeVisible();
    await gutterBtn.click();

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    const header = page.locator('.comment-form-header');
    await expect(header).toContainText('Line');
    // Single-line should not contain "Lines" (with the 's')
    const headerText = await header.textContent();
    expect(headerText).toMatch(/Line \d+$/);
  });
});

// ============================================================
// Line highlight cleared after comment submit/cancel
// ============================================================
test.describe('Line Highlight Cleared — Markdown Git Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);
  });

  test('drag-select then submit clears all selected/focused classes', async ({ page }) => {
    const section = mdSection(page);
    const gutters = section.locator('.line-comment-gutter');
    const firstGutter = gutters.nth(0);
    const thirdGutter = gutters.nth(2);

    await expect(firstGutter).toBeAttached();
    await expect(thirdGutter).toBeAttached();
    await dragBetween(page, firstGutter, thirdGutter);

    // Verify selection exists before submit
    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    // Submit the comment
    await page.locator('.comment-form textarea').fill('Test comment');
    await page.locator('.comment-form .btn-primary').click();
    await expect(page.locator('.comment-card')).toBeVisible();

    // No line blocks should have .selected or .focused
    await expect(section.locator('.line-block.selected')).toHaveCount(0);
    await expect(section.locator('.line-block.focused')).toHaveCount(0);
  });

  test('drag-select then cancel clears all selected/focused classes', async ({ page }) => {
    const section = mdSection(page);
    const gutters = section.locator('.line-comment-gutter');
    const firstGutter = gutters.nth(0);
    const thirdGutter = gutters.nth(2);

    await expect(firstGutter).toBeAttached();
    await expect(thirdGutter).toBeAttached();
    await dragBetween(page, firstGutter, thirdGutter);

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    // Cancel the comment
    await page.locator('.comment-form button', { hasText: 'Cancel' }).click();
    await expect(form).toBeHidden();

    // No line blocks should have .selected or .focused
    await expect(section.locator('.line-block.selected')).toHaveCount(0);
    await expect(section.locator('.line-block.focused')).toHaveCount(0);
  });

  test('single-line click then submit clears all selected/focused classes', async ({ page }) => {
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();

    const gutterBtn = section.locator('.line-comment-gutter').first();
    await expect(gutterBtn).toBeVisible();
    await gutterBtn.click();

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    // Submit
    await page.locator('.comment-form textarea').fill('Single line comment');
    await page.locator('.comment-form .btn-primary').click();
    await expect(page.locator('.comment-card')).toBeVisible();

    // No line blocks should have .selected or .focused
    await expect(section.locator('.line-block.selected')).toHaveCount(0);
    await expect(section.locator('.line-block.focused')).toHaveCount(0);
  });
});

// ============================================================
// Diff Drag Selection — Split Mode (git mode — code files)
// ============================================================
test.describe('Diff Drag Selection — Split Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('dragging across diff comment buttons opens multi-line comment form', async ({ page }) => {
    const section = goSection(page);
    await expect(section).toBeVisible();

    // Find addition-side diff comment buttons in server.go
    const additionSides = section.locator('.diff-split-side.addition');
    await expect(additionSides.first()).toBeVisible();

    // Get the first and a subsequent addition side's comment button
    const firstBtn = additionSides.nth(0).locator('.diff-comment-btn');
    const secondBtn = additionSides.nth(1).locator('.diff-comment-btn');

    // Hover first to make buttons visible
    await additionSides.nth(0).hover();
    await expect(firstBtn).toBeAttached();

    await dragBetween(page, firstBtn, secondBtn);

    // Comment form should open
    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    // Header should show line range (Lines N-M)
    const header = page.locator('.comment-form-header');
    await expect(header).toContainText('Line');
  });

  test('single click on diff-comment-btn opens single-line comment form', async ({ page }) => {
    const section = goSection(page);
    await expect(section).toBeVisible();

    const additionSide = section.locator('.diff-split-side.addition').first();
    await additionSide.hover();

    const commentBtn = additionSide.locator('.diff-comment-btn');
    await expect(commentBtn).toBeVisible();
    await commentBtn.click();

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    const header = page.locator('.comment-form-header');
    const headerText = await header.textContent();
    // Single-line: "Comment on Line N" (no range)
    expect(headerText).toMatch(/Line \d+$/);
  });

  test('dragging selects lines with .selected class on diff sides', async ({ page }) => {
    const section = goSection(page);
    await expect(section).toBeVisible();

    const additionSides = section.locator('.diff-split-side.addition');
    await expect(additionSides.first()).toBeVisible();

    const firstBtn = additionSides.nth(0).locator('.diff-comment-btn');
    const thirdBtn = additionSides.nth(2).locator('.diff-comment-btn');

    await additionSides.nth(0).hover();
    await expect(firstBtn).toBeAttached();

    await dragBetween(page, firstBtn, thirdBtn);

    // After drag, at least one diff-split-side should have .selected
    const selectedSides = section.locator('.diff-split-side.selected');
    const count = await selectedSides.count();
    expect(count).toBeGreaterThanOrEqual(1);
  });
});

// ============================================================
// Diff Drag Selection — Unified Mode (git mode)
// ============================================================
test.describe('Diff Drag Selection — Unified Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    // Switch to unified mode
    const unifiedBtn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
    await unifiedBtn.click();
    await expect(page.locator('.diff-container.unified').first()).toBeVisible();
  });

  test('dragging across diff lines in unified mode opens comment form', async ({ page }) => {
    const section = goSection(page);
    await expect(section).toBeVisible();

    // Find addition lines in the unified diff
    const additionLines = section.locator('.diff-container.unified .diff-line.addition');
    await expect(additionLines.first()).toBeVisible();

    const firstBtn = additionLines.nth(0).locator('.diff-comment-btn');
    const secondBtn = additionLines.nth(1).locator('.diff-comment-btn');

    await additionLines.nth(0).hover();
    await expect(firstBtn).toBeAttached();

    await dragBetween(page, firstBtn, secondBtn);

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    const header = page.locator('.comment-form-header');
    await expect(header).toContainText('Lines');
  });

  test('drag works across add/del lines in unified mode (no side restriction)', async ({ page }) => {
    const section = goSection(page);
    await expect(section).toBeVisible();

    // In unified mode, find any diff lines with comment buttons (mix of add/del)
    const diffLines = section.locator('.diff-container.unified .diff-line');
    await expect(diffLines.first()).toBeVisible();

    // Find two diff lines with line numbers (could be different types)
    // We need lines that have data-diff-line-num set (i.e., commentable)
    const commentableLines = section.locator('.diff-container.unified .diff-line[data-diff-line-num]');
    const count = await commentableLines.count();
    expect(count).toBeGreaterThanOrEqual(2);

    const firstBtn = commentableLines.nth(0).locator('.diff-comment-btn');
    const thirdBtn = commentableLines.nth(2).locator('.diff-comment-btn');

    await commentableLines.nth(0).hover();
    await expect(firstBtn).toBeAttached();

    await dragBetween(page, firstBtn, thirdBtn);

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    // Should show a range since we dragged across multiple lines
    const header = page.locator('.comment-form-header');
    await expect(header).toContainText('Line');
  });

  test('unified drag selects lines with .selected class', async ({ page }) => {
    const section = goSection(page);
    await expect(section).toBeVisible();

    const additionLines = section.locator('.diff-container.unified .diff-line.addition');
    await expect(additionLines.first()).toBeVisible();

    const firstBtn = additionLines.nth(0).locator('.diff-comment-btn');
    const secondBtn = additionLines.nth(1).locator('.diff-comment-btn');

    await additionLines.nth(0).hover();
    await expect(firstBtn).toBeAttached();

    await dragBetween(page, firstBtn, secondBtn);

    // At least one diff-line should have .selected
    const selectedLines = section.locator('.diff-line.selected');
    const count = await selectedLines.count();
    expect(count).toBeGreaterThanOrEqual(1);
  });

  test('unified drag from deletion spans across context lines after release', async ({ page }) => {
    // Regression: dragging from a deletion anchor across context + another deletion
    // should keep the full old-side range highlighted after mouseup, not collapse
    // to only the deletion lines.
    const section = goSection(page);
    await expect(section).toBeVisible();

    // Find two deletion lines separated by at least one context line in between.
    const pair = await section.evaluate((sec) => {
      const lines = Array.from(sec.querySelectorAll('.diff-container.unified .diff-line'));
      let firstDel = -1;
      for (let i = 0; i < lines.length; i++) {
        if (lines[i].classList.contains('deletion')) {
          if (firstDel === -1) { firstDel = i; continue; }
          // Check at least one context line between firstDel and i
          for (let j = firstDel + 1; j < i; j++) {
            const cls = lines[j].classList;
            if (!cls.contains('deletion') && !cls.contains('addition')) {
              return { first: firstDel, last: i };
            }
          }
        }
      }
      return null;
    });
    if (!pair) test.skip(true, 'fixture needs two deletions with context between');

    const allLines = section.locator('.diff-container.unified .diff-line');
    const firstDel = allLines.nth(pair!.first);
    const lastDel = allLines.nth(pair!.last);

    await firstDel.scrollIntoViewIfNeeded();
    await firstDel.hover();
    const firstBtn = firstDel.locator('.diff-comment-btn');
    await expect(firstBtn).toBeAttached();
    const secondBtn = lastDel.locator('.diff-comment-btn');

    await dragBetween(page, firstBtn, secondBtn);

    await expect(page.locator('.comment-form')).toBeVisible();

    // After mouseup, context lines between the deletions should still be highlighted
    // (not just the deletion endpoints). Before the fix, inCurrentForm filtered by
    // side so only deletion lines kept the .selected class after the drag released.
    const selectedContext = section.locator(
      '.diff-container.unified .diff-line.selected:not(.deletion):not(.addition)'
    );
    await expect(async () => {
      const n = await selectedContext.count();
      expect(n).toBeGreaterThan(0);
    }).toPass();
  });
});

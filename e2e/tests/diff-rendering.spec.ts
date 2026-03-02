import { test, expect } from '@playwright/test';

// Helper: navigate and wait for page load
async function loadPage(page: import('@playwright/test').Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

test.describe('Diff Rendering — Split Mode (default)', () => {
  test('shows split diff by default', async ({ page }) => {
    await loadPage(page);

    const splitContainer = page.locator('.diff-container.split');
    await expect(splitContainer.first()).toBeVisible();
  });

  test('split diff has left and right sides', async ({ page }) => {
    await loadPage(page);

    const row = page.locator('.diff-split-row').first();
    await expect(row).toBeVisible();

    const left = row.locator('.diff-split-side.left');
    await expect(left).toBeVisible();

    const right = row.locator('.diff-split-side.right');
    await expect(right).toBeVisible();
  });

  test('addition lines have addition class', async ({ page }) => {
    await loadPage(page);

    const additionSide = page.locator('.diff-split-side.addition');
    await expect(additionSide.first()).toBeVisible();
  });

  test('deletion lines have deletion class', async ({ page }) => {
    await loadPage(page);

    const deletionSide = page.locator('.diff-split-side.deletion');
    await expect(deletionSide.first()).toBeVisible();
  });

  test('hunk headers show @@ notation', async ({ page }) => {
    await loadPage(page);

    const hunkHeader = page.locator('.diff-hunk-header').first();
    await expect(hunkHeader).toBeVisible();

    const hunkText = hunkHeader.locator('.hunk-text');
    await expect(hunkText).toContainText('@@');
  });

  test('deleted file shows "This file was deleted."', async ({ page }) => {
    await loadPage(page);

    // The deleted file section starts collapsed (<details> closed).
    // Click on its header to expand it first.
    const deletedSection = page.locator('#file-section-deleted\\.txt');
    await expect(deletedSection).toBeAttached();

    const header = deletedSection.locator('summary.file-header');
    await header.click();

    const placeholder = deletedSection.locator('.diff-deleted-placeholder');
    await expect(placeholder).toBeVisible();
    await expect(placeholder).toHaveText('This file was deleted.');
  });

  test('spacer shows "Expand" between hunks', async ({ page }) => {
    await loadPage(page);

    // server.go has a multi-hunk diff, so spacers should exist
    const spacer = page.locator('.diff-spacer').first();
    await expect(spacer).toBeVisible();
    await expect(spacer).toContainText('Expand');
    await expect(spacer).toContainText('unchanged line');
  });

  test('clicking spacer expands context lines', async ({ page }) => {
    await loadPage(page);

    // Find the server.go section which has multi-hunk diffs with spacers
    const serverSection = page.locator('#file-section-server\\.go');
    await expect(serverSection).toBeVisible();

    // Count spacers before click
    const spacersBefore = serverSection.locator('.diff-spacer');
    const spacerCountBefore = await spacersBefore.count();
    expect(spacerCountBefore).toBeGreaterThan(0);

    // Count diff rows before expansion
    const rowsBefore = await serverSection.locator('.diff-split-row').count();

    // Click the first spacer
    const firstSpacer = spacersBefore.first();
    await firstSpacer.click();

    // After clicking, the spacer count should decrease by 1 (it gets merged)
    const spacerCountAfter = await serverSection.locator('.diff-spacer').count();
    expect(spacerCountAfter).toBeLessThan(spacerCountBefore);

    // More rows should be visible after expansion
    const rowsAfter = await serverSection.locator('.diff-split-row').count();
    expect(rowsAfter).toBeGreaterThan(rowsBefore);
  });

  test('expanded lines have comment gutter (+ button) on hover', async ({ page }) => {
    await loadPage(page);

    const serverSection = page.locator('#file-section-server\\.go');
    const spacer = serverSection.locator('.diff-spacer').first();
    await expect(spacer).toBeVisible();

    // Click the spacer to expand context lines
    await spacer.click();

    // Wait for re-render — new rows should appear
    await expect(serverSection.locator('.diff-split-row').first()).toBeVisible();

    // Hover over one of the split sides in the section — the comment button should become visible
    const splitSide = serverSection.locator('.diff-split-side').first();
    await splitSide.hover();

    const commentBtn = splitSide.locator('.diff-comment-btn');
    await expect(commentBtn).toBeVisible();
  });
});

test.describe('Diff Mode Toggle', () => {
  test('can switch to unified mode', async ({ page }) => {
    await loadPage(page);

    // Click the unified toggle button
    const unifiedBtn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
    await expect(unifiedBtn).toBeVisible();
    await unifiedBtn.click();

    // Unified container should now be visible
    const unifiedContainer = page.locator('.diff-container.unified');
    await expect(unifiedContainer.first()).toBeVisible();

    // Split container should no longer exist
    await expect(page.locator('.diff-container.split')).toHaveCount(0);
  });

  test('unified mode shows single-pane diff lines', async ({ page }) => {
    await loadPage(page);

    // Switch to unified mode
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();

    const diffLine = page.locator('.diff-container.unified .diff-line');
    await expect(diffLine.first()).toBeVisible();
  });

  test('unified mode addition lines have + sign', async ({ page }) => {
    await loadPage(page);

    // Switch to unified mode
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();

    // Find an addition line's gutter sign
    const additionLine = page.locator('.diff-container.unified .diff-line.addition').first();
    await expect(additionLine).toBeVisible();

    const sign = additionLine.locator('.diff-gutter-sign');
    await expect(sign).toHaveText('+');
  });

  test('diff mode persists across reload', async ({ page, context }) => {
    // Clear cookie first to start fresh
    await context.clearCookies();

    await loadPage(page);

    // Default should be split
    await expect(page.locator('.diff-container.split').first()).toBeVisible();

    // Switch to unified
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    await expect(page.locator('.diff-container.unified').first()).toBeVisible();

    // Reload page
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Should still be in unified mode after reload
    await expect(page.locator('.diff-container.unified').first()).toBeVisible();
    await expect(page.locator('.diff-container.split')).toHaveCount(0);
  });

  test('can switch back to split', async ({ page }) => {
    await loadPage(page);

    // Switch to unified first
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    await expect(page.locator('.diff-container.unified').first()).toBeVisible();

    // Switch back to split
    const splitBtn = page.locator('#diffModeToggle .toggle-btn[data-mode="split"]');
    await splitBtn.click();

    await expect(page.locator('.diff-container.split').first()).toBeVisible();
    await expect(page.locator('.diff-container.unified')).toHaveCount(0);
  });
});

test.describe('Unified Mode — Drag Indicator Across Line Types', () => {
  test('drag indicator shows on deletion lines when dragging from addition line', async ({ page }) => {
    await loadPage(page);

    // Switch to unified mode
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    await expect(page.locator('.diff-container.unified').first()).toBeVisible();

    // Find the server.go section — the second hunk has both del and add lines
    const serverSection = page.locator('#file-section-server\\.go');
    await expect(serverSection).toBeVisible();

    // Scroll to the deletion line area (second hunk has del+add pairs)
    const deletionLine = serverSection.locator('.diff-container.unified .diff-line.deletion').first();
    await deletionLine.scrollIntoViewIfNeeded();
    await expect(deletionLine).toBeVisible();

    // Find adjacent del+add lines within the same hunk
    const allLines = serverSection.locator('.diff-container.unified .diff-line');
    const lineCount = await allLines.count();

    // Find a deletion line index and the next addition line index
    let delIdx = -1;
    let addIdx = -1;
    for (let i = 0; i < lineCount; i++) {
      const line = allLines.nth(i);
      if (delIdx === -1 && await line.evaluate(el => el.classList.contains('deletion'))) {
        delIdx = i;
      }
      if (delIdx !== -1 && addIdx === -1 && await line.evaluate(el => el.classList.contains('addition'))) {
        addIdx = i;
        break;
      }
    }

    expect(delIdx).toBeGreaterThanOrEqual(0);
    expect(addIdx).toBeGreaterThan(delIdx);

    // Start drag from the addition line
    const addLine = allLines.nth(addIdx);
    await addLine.scrollIntoViewIfNeeded();
    const addBtn = addLine.locator('.diff-comment-btn');
    await addLine.hover();
    await expect(addBtn).toBeVisible();

    const addBtnBox = await addBtn.boundingBox();
    const delLine = allLines.nth(delIdx);
    await delLine.scrollIntoViewIfNeeded();
    const delLineBox = await delLine.boundingBox();

    if (addBtnBox && delLineBox) {
      // Start drag on the addition line's + button
      await page.mouse.move(addBtnBox.x + addBtnBox.width / 2, addBtnBox.y + addBtnBox.height / 2);
      await page.mouse.down();

      // Move to the deletion line
      await page.mouse.move(delLineBox.x + delLineBox.width / 2, delLineBox.y + delLineBox.height / 2);

      // The deletion line should have the 'selected' class
      const selectedDeletionLines = serverSection.locator('.diff-container.unified .diff-line.deletion.selected');
      await expect(selectedDeletionLines.first()).toBeVisible({ timeout: 2000 });

      // Also check that drag-range class appears on gutters between the lines
      const dragRangeGutters = serverSection.locator('.diff-container.unified .diff-comment-gutter.drag-range');
      const gutterCount = await dragRangeGutters.count();
      expect(gutterCount).toBeGreaterThan(0);

      // Release the mouse
      await page.mouse.up();
    }
  });

  test('all lines between drag endpoints get selected class in unified mode', async ({ page }) => {
    await loadPage(page);

    // Switch to unified mode
    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    await expect(page.locator('.diff-container.unified').first()).toBeVisible();

    const serverSection = page.locator('#file-section-server\\.go');
    await expect(serverSection).toBeVisible();

    // Find adjacent lines of different types (del then add) in the same hunk
    const allLines = serverSection.locator('.diff-container.unified .diff-line');
    const lineCount = await allLines.count();

    // Find a run of 4+ consecutive lines (any types) starting from a deletion
    let startIdx = -1;
    for (let i = 0; i < lineCount - 3; i++) {
      const line = allLines.nth(i);
      if (await line.evaluate(el => el.classList.contains('deletion'))) {
        startIdx = i;
        break;
      }
    }
    expect(startIdx).toBeGreaterThanOrEqual(0);

    // Drag from startIdx to startIdx+3 (4 lines)
    const startLine = allLines.nth(startIdx);
    const endLine = allLines.nth(startIdx + 3);
    await startLine.scrollIntoViewIfNeeded();

    const startBtn = startLine.locator('.diff-comment-btn');
    await startLine.hover();
    await expect(startBtn).toBeVisible();

    const startBox = await startBtn.boundingBox();
    await endLine.scrollIntoViewIfNeeded();
    const endBox = await endLine.boundingBox();

    if (startBox && endBox) {
      await page.mouse.move(startBox.x + startBox.width / 2, startBox.y + startBox.height / 2);
      await page.mouse.down();
      await page.mouse.move(endBox.x + endBox.width / 2, endBox.y + endBox.height / 2);

      // All 4 lines should have the selected class regardless of type (del/add/context)
      const selectedLines = serverSection.locator('.diff-container.unified .diff-line.selected');
      const selectedCount = await selectedLines.count();
      expect(selectedCount).toBeGreaterThanOrEqual(4);

      await page.mouse.up();
    }
  });
});

import { test, expect, type Page } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// utils.go has a staged change that appends a Reverse function at the end.
// The diff hunk starts mid-file (OldStart=8, NewStart=8), NOT at line 1,
// so a leading spacer should appear before the first hunk.
function utilsSection(page: Page) {
  return page.locator('#file-section-utils\\.go');
}

// server.go has multi-hunk diffs. The first hunk starts at line 2 (imports),
// so a leading spacer should appear (gap=1 for the package declaration).
// The last hunk ends at EOF, so NO trailing spacer should appear.
function serverSection(page: Page) {
  return page.locator('#file-section-server\\.go');
}

// handler.js is a newly added file — its single hunk starts at NewStart=1.
// No leading spacer should appear for new files.
function handlerSection(page: Page) {
  return page.locator('#file-section-handler\\.js');
}

async function switchToUnified(page: Page) {
  const btn = page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]');
  await expect(btn).toBeVisible();
  await btn.click();
  await expect(page.locator('.diff-container.unified').first()).toBeVisible();
}

// ============================================================
// Leading Spacer — Split Mode
// ============================================================
test.describe('Leading Spacer — Split Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('leading spacer appears before first hunk when it does not start at line 1', async ({ page }) => {
    const section = utilsSection(page);
    await expect(section).toBeVisible();

    const leadingSpacer = section.locator('.diff-spacer-leading');
    await expect(leadingSpacer).toBeVisible();
    await expect(leadingSpacer).toContainText('Expand');
  });

  test('no leading spacer when first hunk starts at line 1 (new file)', async ({ page }) => {
    const section = handlerSection(page);
    await expect(section).toBeVisible();

    // handler.js is a new file; its hunk starts at line 1, so no leading spacer
    const leadingSpacer = section.locator('.diff-spacer-leading');
    await expect(leadingSpacer).toHaveCount(0);
  });

  test('clicking leading spacer reveals context lines above the first hunk', async ({ page }) => {
    const section = utilsSection(page);
    await expect(section).toBeVisible();

    // Count rows before clicking
    const rowsBefore = section.locator('.diff-split-row');
    const countBefore = await rowsBefore.count();

    const leadingSpacer = section.locator('.diff-spacer-leading');
    await expect(leadingSpacer).toBeVisible();
    await leadingSpacer.click();

    // After clicking, more rows should appear
    await expect(async () => {
      const countAfter = await section.locator('.diff-split-row').count();
      expect(countAfter).toBeGreaterThan(countBefore);
    }).toPass();
  });

  test('leading spacer disappears after expanding all lines to line 1', async ({ page }) => {
    const section = utilsSection(page);
    await expect(section).toBeVisible();

    const leadingSpacer = section.locator('.diff-spacer-leading');
    await expect(leadingSpacer).toBeVisible();

    // For utils.go, the gap before the first hunk is small (< 20 lines),
    // so one click should expand all and remove the spacer.
    await leadingSpacer.click();

    await expect(section.locator('.diff-spacer-leading')).toHaveCount(0);
  });

  test('server.go leading spacer shows for 1-line gap', async ({ page }) => {
    const section = serverSection(page);
    await expect(section).toBeVisible();

    // server.go hunk starts at line 2, so there's a 1-line gap (package main)
    const leadingSpacer = section.locator('.diff-spacer-leading');
    await expect(leadingSpacer).toBeVisible();
    await expect(leadingSpacer).toContainText('Expand 1 unchanged line');
  });
});

// ============================================================
// Trailing Spacer — Split Mode
// ============================================================
test.describe('Trailing Spacer — Split Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('no trailing spacer when last hunk reaches EOF (server.go)', async ({ page }) => {
    const section = serverSection(page);
    await expect(section).toBeVisible();

    const trailingSpacer = section.locator('.diff-spacer-trailing');
    await expect(trailingSpacer).toHaveCount(0);
  });

  test('no trailing spacer when last hunk reaches EOF (utils.go)', async ({ page }) => {
    const section = utilsSection(page);
    await expect(section).toBeVisible();

    const trailingSpacer = section.locator('.diff-spacer-trailing');
    await expect(trailingSpacer).toHaveCount(0);
  });
});

// ============================================================
// Leading Spacer — Unified Mode
// ============================================================
test.describe('Leading Spacer — Unified Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToUnified(page);
  });

  test('leading spacer appears in unified mode', async ({ page }) => {
    const section = utilsSection(page);
    await expect(section).toBeVisible();

    const leadingSpacer = section.locator('.diff-spacer-leading');
    await expect(leadingSpacer).toBeVisible();
    await expect(leadingSpacer).toContainText('Expand');
  });

  test('clicking leading spacer in unified mode reveals context lines', async ({ page }) => {
    const section = utilsSection(page);
    await expect(section).toBeVisible();

    const linesBefore = section.locator('.diff-line');
    const countBefore = await linesBefore.count();

    const leadingSpacer = section.locator('.diff-spacer-leading');
    await expect(leadingSpacer).toBeVisible();
    await leadingSpacer.click();

    await expect(async () => {
      const countAfter = await section.locator('.diff-line').count();
      expect(countAfter).toBeGreaterThan(countBefore);
    }).toPass();
  });

  test('no leading spacer in unified mode for new file', async ({ page }) => {
    const section = handlerSection(page);
    await expect(section).toBeVisible();

    const leadingSpacer = section.locator('.diff-spacer-leading');
    await expect(leadingSpacer).toHaveCount(0);
  });
});

// ============================================================
// Trailing Spacer — Unified Mode
// ============================================================
test.describe('Trailing Spacer — Unified Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToUnified(page);
  });

  test('no trailing spacer in unified mode when last hunk reaches EOF', async ({ page }) => {
    const section = utilsSection(page);
    await expect(section).toBeVisible();

    const trailingSpacer = section.locator('.diff-spacer-trailing');
    await expect(trailingSpacer).toHaveCount(0);
  });
});

// ============================================================
// Expanded context lines are commentable
// ============================================================
test.describe('Leading Spacer — Expanded Lines Are Commentable', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('expanded leading context lines have comment gutter buttons', async ({ page }) => {
    const section = utilsSection(page);
    await expect(section).toBeVisible();

    const leadingSpacer = section.locator('.diff-spacer-leading');
    await expect(leadingSpacer).toBeVisible();
    await leadingSpacer.click();

    // Wait for re-render
    await expect(section.locator('.diff-spacer-leading')).toHaveCount(0);

    // Hover over one of the new split sides — comment button should appear
    const splitSide = section.locator('.diff-split-side').first();
    await splitSide.scrollIntoViewIfNeeded();
    await splitSide.hover();

    const commentBtn = splitSide.locator('.diff-comment-btn');
    await expect(commentBtn).toBeVisible();
  });
});

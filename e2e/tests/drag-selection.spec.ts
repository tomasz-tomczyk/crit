import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

// Helper: clean all comments via API so each test starts fresh.
async function clearAllComments(request: APIRequestContext) {
  const sessionRes = await request.get('/api/session');
  const session = await sessionRes.json();
  const files = session.files || [];

  for (const f of files) {
    const commentsRes = await request.get(`/api/file/comments?path=${encodeURIComponent(f.path)}`);
    const comments = await commentsRes.json();
    if (Array.isArray(comments)) {
      for (const c of comments) {
        await request.delete(`/api/comment/${c.id}?path=${encodeURIComponent(f.path)}`);
      }
    }
  }
}

// Helper: navigate and wait for page load
async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

// Helper: scope selectors to plan.md file section
function mdSection(page: Page) {
  return page.locator('.file-section').filter({ hasText: 'plan.md' });
}

// Helper: scope selectors to server.go file section
function goSection(page: Page) {
  return page.locator('#file-section-server\\.go');
}

// Helper: switch plan.md to document view (markdown defaults to diff in git mode)
async function switchToDocumentView(page: Page) {
  const section = mdSection(page);
  await expect(section).toBeVisible();
  const docBtn = section.locator('.file-header-toggle .toggle-btn[data-mode="document"]');
  await expect(docBtn).toBeVisible();
  await docBtn.click();
  await expect(section.locator('.document-wrapper')).toBeVisible();
}

// Helper: perform a mouse drag between two elements
async function dragBetween(page: Page, startEl: ReturnType<Page['locator']>, endEl: ReturnType<Page['locator']>) {
  const startBox = await startEl.boundingBox();
  const endBox = await endEl.boundingBox();

  expect(startBox).toBeTruthy();
  expect(endBox).toBeTruthy();

  if (startBox && endBox) {
    await page.mouse.move(startBox.x + startBox.width / 2, startBox.y + startBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(endBox.x + endBox.width / 2, endBox.y + endBox.height / 2, { steps: 5 });
    await page.mouse.up();
  }
}

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
});

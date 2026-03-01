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
// Markdown Drag Selection — File Mode (plan.md, document view by default)
// ============================================================
test.describe('Drag Selection — File Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    // In file mode, plan.md is already in document view — no toggle needed
    const section = mdSection(page);
    await expect(section.locator('.document-wrapper')).toBeVisible();
  });

  test('dragging across gutter elements opens comment form with multi-line header', async ({ page }) => {
    const section = mdSection(page);

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
    // Single-line should show "Line N" without the range
    const headerText = await header.textContent();
    expect(headerText).toMatch(/Line \d+$/);
  });

  test('drag selection allows submitting a multi-line comment', async ({ page }) => {
    const section = mdSection(page);

    const gutters = section.locator('.line-comment-gutter');
    const firstGutter = gutters.nth(0);
    const thirdGutter = gutters.nth(2);

    await expect(firstGutter).toBeAttached();
    await expect(thirdGutter).toBeAttached();

    await dragBetween(page, firstGutter, thirdGutter);

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    // Fill and submit the comment
    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Multi-line drag comment in file mode');
    await page.locator('.comment-form .btn-primary').click();

    // Comment card should appear
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Multi-line drag comment in file mode');
  });
});

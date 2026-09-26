import { test, expect, type Locator, type Page } from '@playwright/test';
import {
  clearAllComments, loadPage, goSection, switchToDocumentView, dragBetween,
  dragLineRange, openLineComment, diffLine, waitUntilHittable, setDiffStyle,
} from './helpers';

// Switching to Document view scrolls: wait out Pierre's pointer-events
// pause on the start gutter, then drag.
async function dragGutters(page: Page, from: Locator, to: Locator) {
  await expect(from).toBeAttached();
  await expect(to).toBeAttached();
  await from.scrollIntoViewIfNeeded();
  await waitUntilHittable(from);
  await dragBetween(page, from, to);
}

// ============================================================
// Markdown Drag Selection (git mode — plan.md in document view)
// ============================================================
test.describe('Markdown Drag Selection — Git Mode', () => {
  let section: Locator;

  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    section = await switchToDocumentView(page);
  });

  test('dragging across gutter elements opens comment form with multi-line header', async ({ page }) => {

    // Get the first and third line-comment-gutter elements
    const gutters = section.locator('.line-comment-gutter');
    const firstGutter = gutters.nth(0);
    const thirdGutter = gutters.nth(2);

    await dragGutters(page, firstGutter, thirdGutter);

    // Comment form should open with "Lines" in the header (multi-line range)
    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    const header = page.locator('.comment-form-header');
    await expect(header).toContainText('Lines');
  });

  test('after drag, selected line blocks have .selected class', async ({ page }) => {

    const gutters = section.locator('.line-comment-gutter');
    const firstGutter = gutters.nth(0);
    const thirdGutter = gutters.nth(2);

    await dragGutters(page, firstGutter, thirdGutter);

    // Every block from the first to the third gutter is selected (the form
    // spans them), and nothing past it.
    const blocks = section.locator('.line-block.kb-nav');
    await expect(blocks.nth(0)).toHaveClass(/selected/);
    await expect(blocks.nth(1)).toHaveClass(/selected/);
    await expect(blocks.nth(2)).toHaveClass(/selected/);
    await expect(blocks.nth(3)).not.toHaveClass(/selected/);
  });

  test('single click on gutter opens single-line comment form', async ({ page }) => {

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
  let section: Locator;

  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    section = await switchToDocumentView(page);
  });

  test('drag-select then submit clears selected class and keeps keyboard focus', async ({ page }) => {
    const gutters = section.locator('.line-comment-gutter');
    const firstGutter = gutters.nth(0);
    const thirdGutter = gutters.nth(2);

    await dragGutters(page, firstGutter, thirdGutter);

    // Verify selection exists before submit
    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    // Submit the comment
    await page.locator('.comment-form textarea').fill('Test comment');
    await page.locator('.comment-form .btn-primary').click();
    await expect(page.locator('.comment-card')).toBeVisible();

    await expect(section.locator('.line-block.selected')).toHaveCount(0);
    await expect(section.locator('.line-block.focused')).toHaveCount(1);
  });

  test('drag-select then cancel clears selected class and keeps keyboard focus', async ({ page }) => {
    const gutters = section.locator('.line-comment-gutter');
    const firstGutter = gutters.nth(0);
    const thirdGutter = gutters.nth(2);

    await dragGutters(page, firstGutter, thirdGutter);

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    // Cancel the comment
    await page.locator('.comment-form button', { hasText: 'Cancel' }).click();
    await expect(form).toBeHidden();

    await expect(section.locator('.line-block.selected')).toHaveCount(0);
    await expect(section.locator('.line-block.focused')).toHaveCount(1);
  });

  test('single-line click then submit clears selected class and keeps keyboard focus', async ({ page }) => {
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

    await expect(section.locator('.line-block.selected')).toHaveCount(0);
    await expect(section.locator('.line-block.focused')).toHaveCount(1);
  });
});

// ============================================================
// Diff Drag Selection (git mode — code files through Pierre)
//
// Dragging the gutter "+" opens a Crit form for exactly that range, and the
// range stays tinted (form-selected, --crit-brand-subtle) while it is open.
// ============================================================

// Rows tinted as an open form's range, as "line:type" in visual order.
// `column` narrows to one split side (or the unified column).
function selectedRows(item: Locator, column = 'code') {
  return item.evaluate((host, column) => {
    const probe = document.createElement('div');
    probe.style.backgroundColor = 'var(--crit-brand-subtle)';
    document.body.appendChild(probe);
    const tint = getComputedStyle(probe).backgroundColor;
    probe.remove();
    return Array.from(host.shadowRoot!.querySelectorAll(`${column} [data-content] > [data-line]`))
      .filter(el => getComputedStyle(el).backgroundColor === tint)
      .map(el => `${(el as HTMLElement).dataset.line}:${(el as HTMLElement).dataset.lineType}`);
  }, column);
}

async function submitForm(page: Page, form: Locator, body: string) {
  await form.locator('textarea').fill(body);
  await form.locator('.btn-primary').click();
  await expect(page.locator('#filesContainer .comment-card', { hasText: body })).toBeVisible();
}

async function persisted(page: Page, body: string) {
  const res = await page.request.get('/api/file/comments?path=server.go');
  const comments = await res.json() as Array<{ body: string; start_line: number; end_line: number; side?: string }>;
  const c = comments.find(x => x.body === body);
  expect(c).toBeTruthy();
  return c!;
}

test.describe('Diff Drag Selection — Split Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  // server.go new lines 23-34 are one run of additions (authMiddleware).
  test('dragging the gutter + across addition lines opens multi-line comment form', async ({ page }) => {
    const item = await goSection(page);
    const form = await dragLineRange(page, item, 23, 25);
    await expect(form.locator('.comment-form-header')).toHaveText('Comment on Lines 23-25');

    await submitForm(page, form, 'split drag range');
    const c = await persisted(page, 'split drag range');
    expect([c.start_line, c.end_line, c.side || '']).toEqual([23, 25, '']);
  });

  test('single click on the gutter + opens single-line comment form', async ({ page }) => {
    const item = await goSection(page);
    const form = await openLineComment(page, item, 23);
    await expect(form.locator('.comment-form-header')).toHaveText('Comment on Line 23');
  });

  test('dragging selects exactly the dragged lines on the new side', async ({ page }) => {
    const item = await goSection(page);
    await dragLineRange(page, item, 23, 25);
    await expect.poll(() => selectedRows(item)).toEqual([
      '23:change-addition', '24:change-addition', '25:change-addition',
    ]);
    // The old side of those rows is not part of the range.
    expect(await selectedRows(item, 'code[data-deletions]')).toEqual([]);
  });
});

test.describe('Diff Drag Selection — Unified Mode', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await setDiffStyle(page, 'unified');
    const item = await goSection(page);
    await expect(item.locator('code[data-unified]')).toBeVisible();
  });

  test('dragging across addition lines in unified mode opens comment form', async ({ page }) => {
    const item = await goSection(page);
    const form = await dragLineRange(page, item, 23, 24);
    await expect(form.locator('.comment-form-header')).toHaveText('Comment on Lines 23-24');
  });

  test('drag works from a context line onto an addition (no type restriction)', async ({ page }) => {
    // server.go new 35 is context (`func main() {`), 36-40 are additions.
    const item = await goSection(page);
    await expect(diffLine(item, 35)).toHaveAttribute('data-line-type', 'context');
    const form = await dragLineRange(page, item, 35, 37);
    await expect(form.locator('.comment-form-header')).toHaveText('Comment on Lines 35-37');

    await submitForm(page, form, 'unified mixed drag');
    const c = await persisted(page, 'unified mixed drag');
    expect([c.start_line, c.end_line, c.side || '']).toEqual([35, 37, '']);
  });

  test('unified drag selects the dragged lines', async ({ page }) => {
    const item = await goSection(page);
    await dragLineRange(page, item, 23, 24);
    await expect.poll(() => selectedRows(item)).toEqual(['23:change-addition', '24:change-addition']);
  });

  test('unified drag from deletion spans across context lines after release', async ({ page }) => {
    // Regression: a drag from one deletion to a later deletion keeps the whole
    // span selected after mouseup (context between included), and the form
    // is an old-side comment over the old-line range.
    // server.go: old 21 and old 23 are deletions; old 22 / new 42 is context
    // between them.
    const item = await goSection(page);
    await expect(diffLine(item, 21, 'old')).toHaveAttribute('data-line-type', 'change-deletion');
    // Pierre virtualizes rows within a file; bring the span on screen.
    await diffLine(item, 21, 'old').scrollIntoViewIfNeeded();
    await expect(diffLine(item, 23, 'old')).toHaveAttribute('data-line-type', 'change-deletion');

    const form = await dragLineRange(page, item, 21, 23, 'old');
    await expect(form.locator('.comment-form-header')).toHaveText('Comment on Lines 21-23');

    await expect.poll(async () => (await selectedRows(item)).filter(r => r.endsWith(':context'))).toEqual(['42:context']);
    const rows = await selectedRows(item);
    expect(rows[0]).toBe('21:change-deletion');
    expect(rows[rows.length - 1]).toBe('23:change-deletion');

    await submitForm(page, form, 'old side span');
    const c = await persisted(page, 'old side span');
    expect([c.start_line, c.end_line, c.side]).toEqual([21, 23, 'old']);
  });
});

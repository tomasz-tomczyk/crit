import { test, expect, type Locator, type Page } from '@playwright/test';
import {
  clearAllComments, loadPage, goSection, switchToDocumentView, dragBetween,
  dragLineRange, diffLine, waitUntilHittable, setDiffStyle, selectedRows,
  openDocLineComment, showLine, waitForScrollStable,
} from './helpers';

// Switching to Document view scrolls: wait until the start gutter takes
// pointer hits, then drag.
async function dragGutters(page: Page, from: Locator, to: Locator) {
  await expect(from).toBeAttached();
  await expect(to).toBeAttached();
  await from.scrollIntoViewIfNeeded();
  await waitUntilHittable(from);
  await dragBetween(page, from, to);
}

// ============================================================
// Markdown Drag Selection (git mode — plan.md in document view)
// A single gutter click's single-line header: comments.spec.ts.
// ============================================================
test.describe('Markdown Drag Selection — Git Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('gutter drag selects a range; submit or cancel clears the selection and keeps keyboard focus', async ({ page }) => {
    await loadPage(page);
    const section = await switchToDocumentView(page);
    const gutters = section.locator('.line-comment-gutter');
    const blocks = section.locator('.line-block.kb-nav');
    const form = section.locator('.comment-form');

    // Dragging across gutters opens a multi-line form, and every block from
    // the first to the third gutter is selected — nothing past it.
    await dragGutters(page, gutters.nth(0), gutters.nth(2));
    await expect(form).toBeVisible();
    await expect(form.locator('.comment-form-header')).toContainText('Lines');
    await expect(blocks.nth(0)).toHaveClass(/selected/);
    await expect(blocks.nth(1)).toHaveClass(/selected/);
    await expect(blocks.nth(2)).toHaveClass(/selected/);
    await expect(blocks.nth(3)).not.toHaveClass(/selected/);

    // Cancel clears the selection; keyboard focus stays.
    await form.locator('button', { hasText: 'Cancel' }).click();
    await expect(form).toBeHidden();
    await expect(section.locator('.line-block.selected')).toHaveCount(0);
    await expect(section.locator('.line-block.focused')).toHaveCount(1);

    // Single-line click then submit.
    const textarea = await openDocLineComment(page, section, 0);
    await textarea.fill('Single line comment');
    await form.locator('.btn-primary').click();
    await expect(section.locator('.comment-card', { hasText: 'Single line comment' })).toBeVisible();
    await expect(section.locator('.line-block.selected')).toHaveCount(0);
    await expect(section.locator('.line-block.focused')).toHaveCount(1);

    // Drag-select then submit.
    await dragGutters(page, gutters.nth(0), gutters.nth(2));
    await expect(form).toBeVisible();
    await form.locator('textarea').fill('Test comment');
    await form.locator('.btn-primary').click();
    await expect(section.locator('.comment-card', { hasText: 'Test comment' })).toBeVisible();
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
  // A single click on the "+" (single-line header): comments.spec.ts.
  test('dragging the gutter + across addition lines selects exactly them and comments on the range', async ({ page }) => {
    const item = await goSection(page);
    const form = await dragLineRange(page, item, 23, 25);
    await expect(form.locator('.comment-form-header')).toHaveText('Comment on Lines 23-25');
    await expect.poll(() => selectedRows(item)).toEqual([
      '23:change-addition', '24:change-addition', '25:change-addition',
    ]);
    // The old side of those rows is not part of the range.
    expect(await selectedRows(item, 'code[data-deletions]')).toEqual([]);

    await submitForm(page, form, 'split drag range');
    const c = await persisted(page, 'split drag range');
    expect([c.start_line, c.end_line, c.side || '']).toEqual([23, 25, '']);
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

  test('unified drag selects the dragged lines, and works from a context line onto additions', async ({ page }) => {
    const item = await goSection(page);
    const form = await dragLineRange(page, item, 23, 24);
    await expect(form.locator('.comment-form-header')).toHaveText('Comment on Lines 23-24');
    await expect.poll(() => selectedRows(item)).toEqual(['23:change-addition', '24:change-addition']);
    await form.locator('button', { hasText: 'Cancel' }).click();
    await expect(page.locator('#filesContainer .comment-form')).toHaveCount(0);

    // No type restriction: server.go new 35 is context (`func main() {`),
    // 36-40 are additions.
    await showLine(page, diffLine(item, 37));
    await expect(diffLine(item, 35)).toHaveAttribute('data-line-type', 'context');
    const mixed = await dragLineRange(page, item, 35, 37);
    await expect(mixed.locator('.comment-form-header')).toHaveText('Comment on Lines 35-37');

    await submitForm(page, mixed, 'unified mixed drag');
    const c = await persisted(page, 'unified mixed drag');
    expect([c.start_line, c.end_line, c.side || '']).toEqual([35, 37, '']);
  });

  test('unified drag from deletion spans across context lines after release', async ({ page }) => {
    // Regression: a drag from one deletion to a later deletion keeps the whole
    // span selected after mouseup (context between included), and the form
    // is an old-side comment over the old-line range.
    // server.go: old 21 and old 23 are deletions; old 22 / new 42 is context
    // between them.
    const item = await goSection(page);
    // Pierre virtualizes rows within a file and re-mounts them while the list
    // settles: bring both ends of the span on screen (a raw scrollIntoView on
    // a row that is not mounted yet fails with "not attached").
    await showLine(page, diffLine(item, 21, 'old'));
    await showLine(page, diffLine(item, 23, 'old'));
    await expect(diffLine(item, 21, 'old')).toHaveAttribute('data-line-type', 'change-deletion');
    await expect(diffLine(item, 23, 'old')).toHaveAttribute('data-line-type', 'change-deletion');
    await waitForScrollStable(page);

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

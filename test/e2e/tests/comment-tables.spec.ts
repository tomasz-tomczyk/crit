import { test, expect, type Locator } from '@playwright/test';
import { addComment, clearAllComments, getMdPath, goSection, loadPage, mdSection, switchToDocumentView } from './helpers';

// A markdown table reaches a comment body through the sanitizer with its
// classes removed, so only element rules can draw it. Without them the browser
// default applies and the cells run together with no border at all.
const TABLE_MD = [
  '| Field | Type | Notes |',
  '| --- | --- | --- |',
  '| id | string | primary key |',
  '| name | string | display name |',
].join('\n');

async function expectCellBorder(cell: Locator) {
  for (const side of ['top', 'right', 'bottom', 'left']) {
    await expect(cell).toHaveCSS(`border-${side}-style`, 'solid');
    await expect(cell).toHaveCSS(`border-${side}-width`, '1px');
  }
}

async function expectAllCellBorders(table: Locator) {
  const cells = table.locator('th, td');
  await expect(cells).not.toHaveCount(0);
  for (const cell of await cells.all()) {
    await expectCellBorder(cell);
  }
}

test.describe('Comment tables', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('a table in a diff comment draws a border on every cell', async ({ page }) => {
    await loadPage(page);
    const section = goSection(page);

    const additionSide = section.locator('.diff-split-side.addition').first();
    await additionSide.hover();
    await additionSide.locator('.diff-comment-btn').click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill(TABLE_MD);
    await page.locator('.comment-form .btn-primary').click();

    const table = section.locator('.comment-body table');
    await expect(table).toBeVisible();
    await expect(table).toHaveCSS('border-collapse', 'collapse');
    await expect(table.locator('th')).toHaveCount(3);
    await expect(table.locator('tbody tr')).toHaveCount(2);

    await expectAllCellBorders(table);
  });

  test('a table in a reply draws a border on every cell', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Which fields does this cover?');
    const reply = await request.post(`/api/comment/${comment.id}/replies?path=${encodeURIComponent(mdPath)}`, {
      data: { body: TABLE_MD, author: 'agent' },
    });
    expect(reply.status()).toBe(201);

    await loadPage(page);
    await switchToDocumentView(page);

    const table = mdSection(page).locator('.reply-body table');
    await expect(table).toBeVisible();
    await expect(table.locator('th')).toHaveCount(3);

    await expectAllCellBorders(table);
  });
});

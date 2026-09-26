import { test, expect, type Locator, type Page, type APIRequestContext } from '@playwright/test';
import {
  clearAllComments, loadPage, goSection, diffLine, openLineComment,
  type DiffSide, setDiffStyle,
} from './helpers';

// server.go has two hunks separated by a small unchanged gap that Crit
// expands automatically (new lines 12-19 / old lines 9-16). Comments on a
// line inside that gap exercise the "expanded context" path: the line is
// not part of any hunk in the raw diff.
const GAP_LINE: Record<DiffSide, number> = { new: 14, old: 11 };
const GAP_TEXT = 'w.WriteHeader(status)';

async function serverComments(request: APIRequestContext) {
  const res = await request.get('/api/file/comments?path=server.go');
  expect(res.ok()).toBeTruthy();
  return (await res.json()) as Array<{ start_line: number; end_line: number; side?: string; body: string }>;
}

// Open a form on the expanded gap line and submit `body`; returns the card.
async function commentOnGapLine(page: Page, item: Locator, side: DiffSide, body: string): Promise<Locator> {
  const line = diffLine(item, GAP_LINE[side], side).first();
  await expect(line).toContainText(GAP_TEXT);
  await expect(line).toHaveAttribute('data-line-type', /^context/);
  const form = await openLineComment(page, item, GAP_LINE[side], side);
  await form.locator('textarea').fill(body);
  await form.locator('.btn-primary').click();
  const card = item.locator('.comment-card');
  await expect(card).toHaveCount(1);
  await expect(card.locator('.comment-body')).toContainText(body);
  return card;
}

async function expectAnchored(request: APIRequestContext, side: DiffSide, body: string) {
  await expect.poll(async () => (await serverComments(request)).map(c => ({
    line: c.start_line, end: c.end_line, old: c.side === 'old', body: c.body,
  }))).toEqual([{ line: GAP_LINE[side], end: GAP_LINE[side], old: side === 'old', body }]);
}

async function editCard(page: Page, item: Locator, from: string, to: string) {
  const card = item.locator('.comment-card');
  await card.hover();
  await card.locator('.comment-actions button[title="Edit"]').click();
  const textarea = item.locator('.comment-form textarea');
  await expect(textarea).toHaveValue(from);
  await textarea.fill(to);
  await item.locator('.comment-form .btn-primary').click();
  await expect(item.locator('.comment-card .comment-body')).toHaveText(to);
  await expect(item.locator('.comment-form')).toHaveCount(0);
}

async function deleteCard(item: Locator) {
  const card = item.locator('.comment-card');
  await card.hover();
  await card.locator('.comment-actions .delete-btn').click();
  await expect(item.locator('.comment-card')).toHaveCount(0);
}

for (const mode of ['split', 'unified'] as const) {
  for (const side of (mode === 'split' ? ['new', 'old'] : ['new']) as DiffSide[]) {
    const label = mode === 'split' ? `Split Mode (${side === 'new' ? 'New' : 'Old'} Side)` : 'Unified Mode';
    const where = mode === 'split' ? `(${side} side)` : 'in unified mode';

    test.describe(`Expanded Context Comments — ${label}`, () => {
      test.beforeEach(async ({ page, request }) => {
        await clearAllComments(request);
        await loadPage(page);
        if (mode === 'unified') {
          await setDiffStyle(page, 'unified');
          const item = await goSection(page);
          await expect(item.locator('code[data-unified]').first()).toBeVisible();
        }
      });

      test(`submit comment on expanded context line ${where}`, async ({ page, request }) => {
        const item = await goSection(page);
        const body = `Comment on expanded context line ${where}`;
        await commentOnGapLine(page, item, side, body);
        await expectAnchored(request, side, body);
      });

      test(`edit comment on expanded context line ${where}`, async ({ page, request }) => {
        const item = await goSection(page);
        await commentOnGapLine(page, item, side, 'Original expanded comment');
        await editCard(page, item, 'Original expanded comment', 'Edited expanded comment');
        await expectAnchored(request, side, 'Edited expanded comment');
      });

      test(`delete comment on expanded context line ${where}`, async ({ page, request }) => {
        const item = await goSection(page);
        await commentOnGapLine(page, item, side, 'Delete me expanded');
        await deleteCard(item);
        await expect.poll(async () => (await serverComments(request)).length).toBe(0);
      });
    });
  }
}

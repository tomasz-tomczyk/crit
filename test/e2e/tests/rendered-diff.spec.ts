import { test, expect, type APIRequestContext, type Page, type Locator } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { clearAllComments, loadPage, mdSection, fileHeader } from './helpers';

// Git mode: the per-file Rendered view shows a markdown file's Before
// (base) and After blocks. plan.md is new on the fixture branch, so in the
// All scope it has no base. An unstaged edit gives it one in the Unstaged
// scope (base = the committed version). The edit is undone after the tests.

const OLD_LINE = "We're adding API key authentication to the server. This is phase 1 of the auth system.";
const NEW_LINE = "We're adding token authentication to the server. This is phase 1 of the auth system.";
// Line of OLD_LINE / NEW_LINE in plan.md (same on both sides).
const CHANGED_LINE = 7;

let planPath = '';
let original = '';

async function waitForUnstagedDiff(request: APIRequestContext, present: boolean) {
  await expect(async () => {
    const res = await request.get('/api/file/diff?path=plan.md&scope=unstaged');
    const diff = res.ok() ? await res.json() : {};
    if (present) {
      expect(diff.previous_content).toContain(OLD_LINE);
      expect(diff.hunks?.length).toBeGreaterThan(0);
    } else {
      expect(diff.hunks?.length ?? 0).toBe(0);
    }
  }).toPass({ timeout: 10_000 });
}

async function loadUnstaged(page: Page) {
  await loadPage(page);
  const btn = page.locator('#scopeToggle .toggle-btn[data-scope="unstaged"]');
  await expect(btn).toBeVisible();
  const loaded = page.waitForResponse(resp => resp.url().includes('/api/session') && resp.status() === 200);
  await btn.click();
  await loaded;
  await expect(btn).toHaveClass(/active/);
}

function toggleBtn(page: Page, mode: 'document' | 'rendered-diff' | 'diff'): Locator {
  return fileHeader(page, 'plan.md').locator(`.file-header-toggle .toggle-btn[data-mode="${mode}"]`);
}

async function addOldSideComment(request: APIRequestContext, body: string) {
  const res = await request.post('/api/file/comments?path=plan.md', {
    data: { start_line: CHANGED_LINE, end_line: CHANGED_LINE, side: 'old', body },
  });
  expect(res.ok()).toBeTruthy();
}

test.describe.serial('Rendered Diff — Git Mode', () => {
  test.beforeAll(async ({ request }) => {
    const session = await (await request.get('/api/session')).json();
    planPath = path.join(session.cwd, 'plan.md');
    original = fs.readFileSync(planPath, 'utf-8');
    expect(original).toContain(OLD_LINE);
    fs.writeFileSync(planPath, original.replace(OLD_LINE, NEW_LINE));
    await waitForUnstagedDiff(request, true);
  });

  test.afterAll(async ({ request }) => {
    await clearAllComments(request);
    if (original) fs.writeFileSync(planPath, original);
    await waitForUnstagedDiff(request, false);
  });

  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('toggle is a labelled group and has no Rendered button without base content', async ({ page }) => {
    await loadPage(page);
    await mdSection(page);
    const toggle = fileHeader(page, 'plan.md').locator('.file-header-toggle');
    await expect(toggle).toHaveAttribute('role', 'group');
    await expect(toggle).toHaveAttribute('aria-label', 'View for plan.md');
    // All scope: plan.md is new on the branch, so there is no Before.
    await expect(toggleBtn(page, 'rendered-diff')).toHaveCount(0);
  });

  test('Rendered shows Before/After in split and unified layouts', async ({ page }) => {
    await loadUnstaged(page);
    const section = await mdSection(page);
    const rendered = toggleBtn(page, 'rendered-diff');
    await expect(rendered).toBeVisible();
    await rendered.click();
    await expect(rendered).toHaveClass(/active/);
    await expect(rendered).toHaveAttribute('aria-pressed', 'true');

    const split = section.locator('.diff-view');
    await expect(split).toBeVisible();
    const labels = split.locator('.diff-view-side-label');
    await expect(labels).toHaveText(['Before', 'After']);
    await expect(split.locator('.line-block.diff-removed').filter({ hasText: 'API key authentication' })).toHaveCount(1);
    await expect(split.locator('.line-block.diff-added').filter({ hasText: 'token authentication' })).toHaveCount(1);

    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    const unified = section.locator('.diff-view-unified');
    await expect(unified).toBeVisible();
    await expect(section.locator('.diff-view')).toHaveCount(0);
    await expect(unified.locator('.line-block.diff-removed').filter({ hasText: 'API key authentication' })).toHaveCount(1);
    await expect(unified.locator('.line-block.diff-added').filter({ hasText: 'token authentication' })).toHaveCount(1);

    await page.locator('#diffModeToggle .toggle-btn[data-mode="split"]').click();
    await expect(section.locator('.diff-view')).toBeVisible();
  });

  test('can comment on an After block', async ({ page }) => {
    await loadUnstaged(page);
    const section = await mdSection(page);
    await toggleBtn(page, 'rendered-diff').click();
    const added = section.locator('.diff-view .line-block.diff-added').filter({ hasText: 'token authentication' });
    await added.scrollIntoViewIfNeeded();
    await added.hover();
    await added.locator('.line-comment-gutter').click();
    await page.locator('.comment-form textarea').fill('After block comment');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.diff-view .comment-card .comment-body')).toContainText('After block comment');
  });

  test('old-side comments stay on the Before side', async ({ page, request }) => {
    await addOldSideComment(request, 'Old side note');
    await loadUnstaged(page);
    const section = await mdSection(page);
    await toggleBtn(page, 'rendered-diff').click();

    const split = section.locator('.diff-view');
    await expect(split).toBeVisible();
    const card = section.locator('.comment-card').filter({ hasText: 'Old side note' });
    await expect(card).toHaveCount(1);
    // Cells alternate Before (even) / After (odd); the card sits in a Before cell.
    const inBefore = await split.evaluate(el => {
      const cells = Array.from(el.querySelectorAll(':scope > .diff-view-cell'));
      const i = cells.findIndex(c => c.textContent!.includes('Old side note'));
      return i >= 0 && i % 2 === 0;
    });
    expect(inBefore).toBe(true);

    // Document view renders only the current version: the comment is listed
    // above the document instead of after the After block with that number.
    await toggleBtn(page, 'document').click();
    const doc = section.locator('.document-wrapper');
    await expect(doc).toBeVisible();
    await expect(doc.locator('.old-side-comments .comment-card').filter({ hasText: 'Old side note' })).toHaveCount(1);
    await expect(doc.locator('.comment-card').filter({ hasText: 'Old side note' })).toHaveCount(1);
  });
});

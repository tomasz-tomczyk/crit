import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import {
  clearAllComments, loadPage, switchToDocumentView, addComment, getMdPath, getReviewFilePath, roundComplete,
} from './helpers';

function commentsPanel(page: Page) {
  return page.locator('#commentsPanel');
}

function panelCards(page: Page) {
  return page.locator('.panel-comment-block .comment-card');
}

function filterBtn(page: Page, filter: 'all' | 'open' | 'resolved') {
  return page.locator(`#commentsFilterPill .toggle-btn[data-filter="${filter}"]`);
}

function filterCount(page: Page, filter: 'all' | 'open' | 'resolved') {
  return filterBtn(page, filter).locator('.filter-count');
}

function expandAllBtn(page: Page) {
  return page.locator('#commentsPanelExpandAll');
}

function countBadge(page: Page) {
  return page.locator('#commentsPanelCountBadge');
}

async function openPanel(page: Page) {
  await page.keyboard.press('Shift+C');
  await expect(commentsPanel(page)).not.toHaveClass(/comments-panel-hidden/);
}

// Finish the round and let the "agent" resolve every comment in review.json,
// then wait for round-complete to carry them forward.
async function finishAndResolve(request: APIRequestContext) {
  await expect(await request.post('/api/finish')).toBeOK();
  const critJsonPath = await getReviewFilePath(request);
  const critJson = JSON.parse(fs.readFileSync(critJsonPath, 'utf-8'));
  for (const fileKey of Object.keys(critJson.files)) {
    for (const comment of critJson.files[fileKey].comments) {
      comment.resolved = true;
      comment.resolution_note = 'Fixed';
    }
  }
  fs.writeFileSync(critJsonPath, JSON.stringify(critJson, null, 2));
  await roundComplete(request);
}

// ============================================================
// Panel Redesign — Header, Segmented Filter, File Groups, Expand All
// ============================================================
test.describe('Panel Redesign', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('empty review: count badge shows 0', async ({ page }) => {
    await loadPage(page);
    await openPanel(page);
    await expect(countBadge(page)).toHaveText('0');
  });

  test('count badge, segmented filter counts and filtering, and per-file groups', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const session = await (await request.get('/api/session')).json();
    const goPath: string = session.files.find((f: { path: string }) => f.path.endsWith('.go'))?.path;
    expect(goPath).toBeTruthy();

    // 1 resolved + 2 open on plan.md, 1 open on a Go file = 4 total.
    await addComment(request, mdPath, 1, 'Will be resolved');
    await finishAndResolve(request);
    await addComment(request, mdPath, 2, 'Open one');
    await addComment(request, mdPath, 3, 'Open two');
    await addComment(request, goPath, 1, 'Go comment');
    await loadPage(page);
    await openPanel(page);

    await expect(countBadge(page)).toHaveText('4');

    // All is active by default and shows every card.
    await expect(filterBtn(page, 'all')).toHaveClass(/active/);
    await expect(filterBtn(page, 'open')).not.toHaveClass(/active/);
    await expect(filterBtn(page, 'resolved')).not.toHaveClass(/active/);
    await expect(panelCards(page)).toHaveCount(4);
    await expect(filterCount(page, 'all')).toHaveText('4');
    await expect(filterCount(page, 'open')).toHaveText('3');
    await expect(filterCount(page, 'resolved')).toHaveText('1');

    // File groups: chevron and per-file count.
    const fileGroups = page.locator('.comments-panel-file-group');
    await expect(fileGroups).toHaveCount(2);
    const mdHeader = fileGroups.filter({ hasText: mdPath }).locator('.comments-panel-file-name');
    const goHeader = fileGroups.filter({ hasText: goPath }).locator('.comments-panel-file-name');
    await expect(mdHeader.locator('.comments-panel-file-chevron')).toBeVisible();
    await expect(goHeader.locator('.comments-panel-file-chevron')).toBeVisible();
    await expect(mdHeader.locator('.comments-panel-file-count')).toHaveText('3');
    await expect(goHeader.locator('.comments-panel-file-count')).toHaveText('1');

    // Clicking a group header collapses and expands just that group.
    const firstGroup = fileGroups.first();
    await firstGroup.locator('.comments-panel-file-name').click();
    await expect(firstGroup).toHaveClass(/collapsed/);
    await expect(firstGroup.locator('.comment-card').first()).toBeHidden();
    await expect(fileGroups.nth(1).locator('.comment-card').first()).toBeVisible();
    await firstGroup.locator('.comments-panel-file-name').click();
    await expect(firstGroup).not.toHaveClass(/collapsed/);
    await expect(firstGroup.locator('.comment-card').first()).toBeVisible();

    // Open shows only unresolved; Resolved only the resolved one.
    await filterBtn(page, 'open').click();
    await expect(filterBtn(page, 'open')).toHaveClass(/active/);
    await expect(panelCards(page)).toHaveCount(3);
    await expect(panelCards(page).filter({ hasText: 'Will be resolved' })).toHaveCount(0);

    await filterBtn(page, 'resolved').click();
    await expect(filterBtn(page, 'resolved')).toHaveClass(/active/);
    await expect(panelCards(page)).toHaveCount(1);
    await expect(panelCards(page).first()).toHaveClass(/resolved-card/);
    await expect(panelCards(page).first().locator('.comment-body')).toContainText('Will be resolved');
  });

  test('filter counts update when a comment is added, and Collapse/Expand all toggle panel and inline cards', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'First');
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await openPanel(page);

    await expect(filterCount(page, 'all')).toHaveText('1');
    await expect(filterCount(page, 'open')).toHaveText('1');

    // Add a comment via UI
    await doc.locator('.line-block').nth(2).hover();
    await doc.locator('.line-comment-gutter').nth(2).click();
    await page.locator('.comment-form textarea').fill('Second via UI');
    await page.locator('.comment-form .btn-primary').click();
    await expect(filterCount(page, 'all')).toHaveText('2');
    await expect(filterCount(page, 'open')).toHaveText('2');

    // Cards start expanded; the button says "Collapse all". It collapses (and
    // re-expands) both the panel cards and the inline cards in the document.
    const inlineCard = doc.locator('.comment-card[data-comment-id]').first();
    await expect(inlineCard).toBeVisible();
    await expect(expandAllBtn(page)).toHaveText('Collapse all');
    await expect(inlineCard).not.toHaveClass(/collapsed/);

    await expandAllBtn(page).click();
    await expect(expandAllBtn(page)).toHaveText('Expand all');
    await expect(panelCards(page)).toHaveCount(2);
    await expect(panelCards(page).first()).toHaveClass(/collapsed/);
    await expect(inlineCard).toHaveClass(/collapsed/);

    await expandAllBtn(page).click();
    await expect(expandAllBtn(page)).toHaveText('Collapse all');
    await expect(panelCards(page).first()).not.toHaveClass(/collapsed/);
    await expect(inlineCard).not.toHaveClass(/collapsed/);
  });
});

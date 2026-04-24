import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import { clearAllComments, loadPage, mdSection, switchToDocumentView, addComment, getMdPath } from './helpers';

function commentsPanel(page: Page) {
  return page.locator('#commentsPanel');
}

function panelCards(page: Page) {
  return page.locator('.panel-comment-block .comment-card');
}

function filterPill(page: Page) {
  return page.locator('#commentsFilterPill');
}

function filterBtn(page: Page, filter: 'all' | 'open' | 'resolved') {
  return filterPill(page).locator(`.toggle-btn[data-filter="${filter}"]`);
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

async function waitForRound(request: APIRequestContext, previousRound: number) {
  await expect(async () => {
    const session = await request.get('/api/session').then(r => r.json());
    expect(session.review_round).toBeGreaterThan(previousRound);
  }).toPass({ timeout: 5000 });
}

async function finishAndResolve(request: APIRequestContext) {
  const session = await request.get('/api/session').then(r => r.json());
  const currentRound = session.review_round;

  const finishRes = await request.post('/api/finish');
  const finishData = await finishRes.json();
  const critJsonPath = finishData.review_file;

  const critJson = JSON.parse(fs.readFileSync(critJsonPath, 'utf-8'));
  for (const fileKey of Object.keys(critJson.files)) {
    for (const comment of critJson.files[fileKey].comments) {
      comment.resolved = true;
      comment.resolution_note = 'Fixed';
    }
  }
  fs.writeFileSync(critJsonPath, JSON.stringify(critJson, null, 2));

  await request.post('/api/round-complete');
  await waitForRound(request, currentRound);
}

// ============================================================
// Panel Redesign — Header, Segmented Filter, File Groups, Expand All
// ============================================================
test.describe('Panel Redesign', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  // ----------------------------------------------------------
  // 1. Panel header shows count badge
  // ----------------------------------------------------------
  test('count badge shows correct total', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'First comment');
    await addComment(request, mdPath, 3, 'Second comment');
    await addComment(request, mdPath, 5, 'Third comment');
    await loadPage(page);

    await openPanel(page);
    await expect(countBadge(page)).toHaveText('3');
  });

  test('count badge shows 0 when no comments', async ({ page }) => {
    await loadPage(page);
    await openPanel(page);
    await expect(countBadge(page)).toHaveText('0');
  });

  // ----------------------------------------------------------
  // 2. Segmented filter — All / Open / Resolved
  // ----------------------------------------------------------
  test('All filter is active by default', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'A comment');
    await loadPage(page);
    await openPanel(page);

    await expect(filterBtn(page, 'all')).toHaveClass(/active/);
    await expect(filterBtn(page, 'open')).not.toHaveClass(/active/);
    await expect(filterBtn(page, 'resolved')).not.toHaveClass(/active/);
  });

  test('Open filter shows only unresolved comments', async ({ page, request }) => {
    const mdPath = await getMdPath(request);

    // Create a comment, resolve it, then add a new unresolved one
    await addComment(request, mdPath, 1, 'Will be resolved');
    await finishAndResolve(request);
    await addComment(request, mdPath, 2, 'Still open');

    await loadPage(page);
    await openPanel(page);

    // All shows both
    await expect(panelCards(page)).toHaveCount(2);

    // Open shows only unresolved
    await filterBtn(page, 'open').click();
    await expect(filterBtn(page, 'open')).toHaveClass(/active/);
    await expect(panelCards(page)).toHaveCount(1);
    await expect(panelCards(page).first().locator('.comment-body')).toContainText('Still open');
  });

  test('Resolved filter shows only resolved comments', async ({ page, request }) => {
    const mdPath = await getMdPath(request);

    await addComment(request, mdPath, 1, 'Will be resolved');
    await finishAndResolve(request);
    await addComment(request, mdPath, 2, 'Still open');

    await loadPage(page);
    await openPanel(page);

    await filterBtn(page, 'resolved').click();
    await expect(filterBtn(page, 'resolved')).toHaveClass(/active/);
    await expect(panelCards(page)).toHaveCount(1);
    await expect(panelCards(page).first()).toHaveClass(/resolved-card/);
  });

  // ----------------------------------------------------------
  // 3. Filter counts are correct
  // ----------------------------------------------------------
  test('filter pill counts match actual comment counts', async ({ page, request }) => {
    const mdPath = await getMdPath(request);

    // 1 resolved + 2 open = 3 total
    await addComment(request, mdPath, 1, 'Will be resolved');
    await finishAndResolve(request);
    await addComment(request, mdPath, 2, 'Open one');
    await addComment(request, mdPath, 3, 'Open two');

    await loadPage(page);
    await openPanel(page);

    await expect(filterCount(page, 'all')).toHaveText('3');
    await expect(filterCount(page, 'open')).toHaveText('2');
    await expect(filterCount(page, 'resolved')).toHaveText('1');
  });

  test('filter counts update when a comment is added', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'First');
    await loadPage(page);
    await switchToDocumentView(page);
    await openPanel(page);

    await expect(filterCount(page, 'all')).toHaveText('1');
    await expect(filterCount(page, 'open')).toHaveText('1');

    // Add a comment via UI
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').nth(2);
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').nth(2).click();
    await page.locator('.comment-form textarea').fill('Second via UI');
    await page.locator('.comment-form .btn-primary').click();

    await expect(filterCount(page, 'all')).toHaveText('2');
    await expect(filterCount(page, 'open')).toHaveText('2');
  });

  // ----------------------------------------------------------
  // 4. Collapsible file groups
  // ----------------------------------------------------------
  test('clicking file group header collapses and expands comments', async ({ page, request }) => {
    const session = await (await request.get('/api/session')).json();
    const mdPath = session.files.find((f: { path: string }) => f.path.endsWith('.md'))?.path;
    const goPath = session.files.find((f: { path: string }) => f.path.endsWith('.go'))?.path;
    expect(mdPath).toBeTruthy();
    expect(goPath).toBeTruthy();

    await addComment(request, mdPath!, 1, 'MD comment');
    await addComment(request, goPath!, 1, 'Go comment');
    await loadPage(page);
    await openPanel(page);

    // Both groups visible, 2 cards total
    const fileGroups = page.locator('.comments-panel-file-group');
    await expect(fileGroups).toHaveCount(2);
    await expect(panelCards(page)).toHaveCount(2);

    // Click first file group header to collapse
    const firstGroupHeader = fileGroups.first().locator('.comments-panel-file-name');
    await firstGroupHeader.click();
    await expect(fileGroups.first()).toHaveClass(/collapsed/);

    // Cards in collapsed group are hidden; other group still shows its card
    const firstGroupCards = fileGroups.first().locator('.comment-card');
    await expect(firstGroupCards.first()).toBeHidden();

    // Click again to expand
    await firstGroupHeader.click();
    await expect(fileGroups.first()).not.toHaveClass(/collapsed/);
    await expect(firstGroupCards.first()).toBeVisible();
  });

  test('file group header shows chevron and comment count', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Comment A');
    await addComment(request, mdPath, 3, 'Comment B');

    const session = await (await request.get('/api/session')).json();
    const goPath = session.files.find((f: { path: string }) => f.path.endsWith('.go'))?.path;
    expect(goPath).toBeTruthy();
    await addComment(request, goPath!, 1, 'Go comment');

    await loadPage(page);
    await openPanel(page);

    const fileGroups = page.locator('.comments-panel-file-group');
    await expect(fileGroups).toHaveCount(2);

    // Each group header has a chevron and count
    const firstHeader = fileGroups.first().locator('.comments-panel-file-name');
    await expect(firstHeader.locator('.comments-panel-file-chevron')).toBeVisible();
    await expect(firstHeader.locator('.comments-panel-file-count')).toBeVisible();
  });

  // ----------------------------------------------------------
  // 5. Expand all / Collapse all toggle
  // ----------------------------------------------------------
  test('Expand all button label starts as "Expand all"', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Test comment');
    await loadPage(page);
    await openPanel(page);

    await expect(expandAllBtn(page)).toHaveText('Expand all');
  });

  test('clicking Expand all expands cards and label changes to Collapse all', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Comment to expand');
    await loadPage(page);
    await openPanel(page);

    // Click Expand all
    await expandAllBtn(page).click();
    await expect(expandAllBtn(page)).toHaveText('Collapse all');

    // Cards should not have collapsed class
    const cards = panelCards(page);
    await expect(cards).toHaveCount(1);
    await expect(cards.first()).not.toHaveClass(/collapsed/);
  });

  test('clicking Collapse all collapses cards and label reverts', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Comment to collapse');
    await loadPage(page);
    await openPanel(page);

    // Expand then collapse
    await expandAllBtn(page).click();
    await expect(expandAllBtn(page)).toHaveText('Collapse all');

    await expandAllBtn(page).click();
    await expect(expandAllBtn(page)).toHaveText('Expand all');

    // Cards should have collapsed class
    await expect(panelCards(page).first()).toHaveClass(/collapsed/);
  });

  // ----------------------------------------------------------
  // 6. Expand all affects inline comments in the document body
  // ----------------------------------------------------------
  test('Collapse all also collapses inline comment blocks in document', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Inline test comment');
    await loadPage(page);
    await switchToDocumentView(page);
    await openPanel(page);

    // Ensure inline comment card exists and is visible
    const inlineCard = mdSection(page).locator('.comment-card[data-comment-id]').first();
    await expect(inlineCard).toBeVisible();

    // Expand all first (ensure everything is expanded)
    await expandAllBtn(page).click();
    await expect(expandAllBtn(page)).toHaveText('Collapse all');
    await expect(inlineCard).not.toHaveClass(/collapsed/);

    // Collapse all — should affect both panel and inline cards
    await expandAllBtn(page).click();
    await expect(expandAllBtn(page)).toHaveText('Expand all');
    await expect(inlineCard).toHaveClass(/collapsed/);
  });

  test('Expand all expands previously collapsed inline comments', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Inline expand test');
    await loadPage(page);
    await switchToDocumentView(page);
    await openPanel(page);

    const inlineCard = mdSection(page).locator('.comment-card[data-comment-id]').first();
    await expect(inlineCard).toBeVisible();

    // Collapse all first
    await expandAllBtn(page).click();
    await expandAllBtn(page).click();
    await expect(inlineCard).toHaveClass(/collapsed/);

    // Expand all — inline card should be expanded again
    await expandAllBtn(page).click();
    await expect(expandAllBtn(page)).toHaveText('Collapse all');
    await expect(inlineCard).not.toHaveClass(/collapsed/);
  });
});

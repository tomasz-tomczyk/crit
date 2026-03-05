import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import { clearAllComments, loadPage, mdSection, switchToDocumentView, addComment, getMdPath } from './helpers';

function commentsPanel(page: Page) {
  return page.locator('#commentsPanel');
}

function panelCards(page: Page) {
  return page.locator('.comments-panel-card');
}

async function waitForRound(request: APIRequestContext, previousRound: number) {
  await expect(async () => {
    const session = await request.get('/api/session').then(r => r.json());
    expect(session.review_round).toBeGreaterThan(previousRound);
  }).toPass({ timeout: 5000 });
}

// ============================================================
// Comments Panel — Git Mode
// ============================================================
test.describe('Comments Panel — Git Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('panel is hidden by default', async ({ page }) => {
    await loadPage(page);
    await expect(commentsPanel(page)).toHaveClass(/comments-panel-hidden/);
  });

  test('Shift+C toggles panel open and closed', async ({ page }) => {
    await loadPage(page);

    await page.keyboard.press('Shift+C');
    await expect(commentsPanel(page)).not.toHaveClass(/comments-panel-hidden/);

    await page.keyboard.press('Shift+C');
    await expect(commentsPanel(page)).toHaveClass(/comments-panel-hidden/);
  });

  test('clicking comment count opens panel', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Panel toggle test');
    await loadPage(page);

    const countEl = page.locator('#commentCount');
    await expect(countEl).toBeVisible();
    await countEl.click();

    await expect(commentsPanel(page)).not.toHaveClass(/comments-panel-hidden/);
  });

  test('close button hides panel', async ({ page }) => {
    await loadPage(page);
    await page.keyboard.press('Shift+C');
    await expect(commentsPanel(page)).not.toHaveClass(/comments-panel-hidden/);

    await page.locator('.comments-panel-close').click();
    await expect(commentsPanel(page)).toHaveClass(/comments-panel-hidden/);
  });

  test('empty state when no comments', async ({ page }) => {
    await loadPage(page);
    await page.keyboard.press('Shift+C');

    await expect(page.locator('.comments-panel-empty')).toBeVisible();
    await expect(page.locator('.comments-panel-empty')).toContainText('No unresolved comments');
  });

  test('panel shows comment cards', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'First git comment');
    await addComment(request, mdPath, 3, 'Second git comment');
    await loadPage(page);

    await page.keyboard.press('Shift+C');
    await expect(panelCards(page)).toHaveCount(2);
    await expect(panelCards(page).first().locator('.comments-panel-card-body')).toContainText('First git comment');
    await expect(panelCards(page).nth(1).locator('.comments-panel-card-body')).toContainText('Second git comment');
  });

  test('panel shows line references', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 5, 'Line ref test');
    await loadPage(page);

    await page.keyboard.press('Shift+C');
    await expect(panelCards(page).first().locator('.comments-panel-card-line')).toContainText('Line 5');
  });

  test('panel shows line range for multi-line comments', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const resp = await request.post(`/api/file/comments?path=${encodeURIComponent(mdPath)}`, {
      data: { start_line: 2, end_line: 4, body: 'Range comment' },
    });
    expect(resp.ok()).toBeTruthy();
    await loadPage(page);

    await page.keyboard.press('Shift+C');
    await expect(panelCards(page).first().locator('.comments-panel-card-line')).toContainText('Lines 2-4');
  });

  test('panel updates when comment is added via UI', async ({ page }) => {
    await loadPage(page);
    await switchToDocumentView(page);

    // Open panel first
    await page.keyboard.press('Shift+C');
    await expect(page.locator('.comments-panel-empty')).toBeVisible();

    // Add a comment through the UI
    const section = mdSection(page);
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Added via UI');
    await page.locator('.comment-form .btn-primary').click();

    // Panel should now show the comment
    await expect(panelCards(page)).toHaveCount(1);
    await expect(panelCards(page).first().locator('.comments-panel-card-body')).toContainText('Added via UI');
  });

  test('panel updates when comment is deleted', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Will be deleted');
    await loadPage(page);
    await switchToDocumentView(page);

    await page.keyboard.press('Shift+C');
    await expect(panelCards(page)).toHaveCount(1);

    // Delete through UI
    const section = mdSection(page);
    const deleteBtn = section.locator('.comment-card .delete-btn');
    await deleteBtn.click();

    // Panel should update to empty state
    await expect(page.locator('.comments-panel-empty')).toBeVisible();
  });

  test('clicking panel card scrolls to inline comment', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Scroll target');
    await loadPage(page);
    await switchToDocumentView(page);

    await page.keyboard.press('Shift+C');
    await panelCards(page).first().click();

    // The inline comment card should get the highlight animation class
    const inlineCard = mdSection(page).locator('.comment-card[data-comment-id]').first();
    await expect(inlineCard).toBeVisible();
    await expect(inlineCard).toHaveClass(/comment-card-highlight/);
  });

  test('panel does not persist open state across reloads', async ({ page }) => {
    await loadPage(page);
    await page.keyboard.press('Shift+C');
    await expect(commentsPanel(page)).not.toHaveClass(/comments-panel-hidden/);

    await loadPage(page);
    await expect(commentsPanel(page)).toHaveClass(/comments-panel-hidden/);
  });

  test('resolved filter is hidden when no resolved comments exist', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Active comment');
    await loadPage(page);
    await page.keyboard.press('Shift+C');

    await expect(page.locator('#commentsPanelFilter')).toBeHidden();
  });

  test('panel shows file name headers in multi-file mode', async ({ page, request }) => {
    const session = await (await request.get('/api/session')).json();
    const mdPath = session.files.find((f: { path: string }) => f.path.endsWith('.md'))?.path;
    const goPath = session.files.find((f: { path: string }) => f.path.endsWith('.go'))?.path;
    expect(mdPath).toBeTruthy();
    expect(goPath).toBeTruthy();

    await addComment(request, mdPath!, 1, 'MD comment');
    await addComment(request, goPath!, 1, 'Go comment');
    await loadPage(page);

    await page.keyboard.press('Shift+C');
    await expect(panelCards(page)).toHaveCount(2);

    const fileNames = page.locator('.comments-panel-file-name');
    await expect(fileNames).toHaveCount(2);
  });

  test('keyboard shortcut in shortcuts overlay', async ({ page }) => {
    await loadPage(page);
    await page.keyboard.press('?');
    const overlay = page.locator('.shortcuts-overlay.active');
    await expect(overlay).toBeVisible();
    await expect(overlay).toContainText('Toggle comments panel');
  });

  // --- Resolved / carried-forward comment tests ---

  test('resolved carried-forward comment shows Resolved badge, not Unresolved', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Will be resolved');

    // Finish to write .crit.json
    const finishRes = await request.post('/api/finish');
    const finishData = await finishRes.json();
    const critJsonPath = finishData.review_file;

    // Mark comment as resolved in .crit.json
    const critJson = JSON.parse(fs.readFileSync(critJsonPath, 'utf-8'));
    for (const fileKey of Object.keys(critJson.files)) {
      for (const comment of critJson.files[fileKey].comments) {
        comment.resolved = true;
        comment.resolution_note = 'Fixed';
      }
    }
    fs.writeFileSync(critJsonPath, JSON.stringify(critJson, null, 2));

    // Round-complete to carry forward
    const round = (await request.get('/api/session').then(r => r.json())).review_round;
    await request.post('/api/round-complete');
    await waitForRound(request, round);

    await loadPage(page);
    await page.keyboard.press('Shift+C');

    // Toggle show resolved
    await page.locator('.comments-panel-switch-track').click();

    const card = panelCards(page).first();
    await expect(card).toBeVisible();
    await expect(card.locator('.comments-panel-badge-resolved')).toContainText('Resolved');
    // Should NOT have an unresolved badge
    await expect(card.locator('.comments-panel-badge-unresolved')).toHaveCount(0);
  });

  test('unresolved carried-forward comment shows Unresolved badge', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Still unresolved');

    // Finish + round-complete to carry forward
    await request.post('/api/finish');
    const round = (await request.get('/api/session').then(r => r.json())).review_round;
    await request.post('/api/round-complete');
    await waitForRound(request, round);

    await loadPage(page);
    await page.keyboard.press('Shift+C');

    const card = panelCards(page).first();
    await expect(card).toBeVisible();
    await expect(card.locator('.comments-panel-badge-unresolved')).toContainText('Unresolved');
    // Should NOT have a resolved badge
    await expect(card.locator('.comments-panel-badge-resolved')).toHaveCount(0);
  });

  test('clicking resolved comment in panel scrolls to inline resolved comment', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Resolve and scroll');

    const finishRes = await request.post('/api/finish');
    const finishData = await finishRes.json();
    const critJsonPath = finishData.review_file;

    const critJson = JSON.parse(fs.readFileSync(critJsonPath, 'utf-8'));
    for (const fileKey of Object.keys(critJson.files)) {
      for (const comment of critJson.files[fileKey].comments) {
        comment.resolved = true;
      }
    }
    fs.writeFileSync(critJsonPath, JSON.stringify(critJson, null, 2));

    const round = (await request.get('/api/session').then(r => r.json())).review_round;
    await request.post('/api/round-complete');
    await waitForRound(request, round);

    await loadPage(page);

    // After round-complete, .crit.json appears in session, so mdSection helper
    // can match multiple sections. Use the plan.md section by ID directly.
    const mdSectionById = page.locator('#file-section-plan\\.md');
    const docBtn = mdSectionById.locator('.file-header-toggle .toggle-btn[data-mode="document"]');
    await expect(docBtn).toBeVisible();
    await docBtn.click();
    await expect(mdSectionById.locator('.document-wrapper')).toBeVisible();

    await page.keyboard.press('Shift+C');

    // Show resolved comments
    await page.locator('.comments-panel-switch-track').click();
    await expect(panelCards(page)).toHaveCount(1);

    // Click the resolved card
    await panelCards(page).first().click();

    // The resolved inline comment should be visible and highlighted
    const inlineResolved = mdSectionById.locator('.resolved-comment[data-comment-id]').first();
    await expect(inlineResolved).toBeVisible();
    await expect(inlineResolved).toHaveClass(/comment-card-highlight/);
  });

  test('new comments do not show carried-forward badges', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Fresh comment');
    await loadPage(page);
    await page.keyboard.press('Shift+C');

    const card = panelCards(page).first();
    await expect(card).toBeVisible();
    await expect(card.locator('.comments-panel-badge')).toHaveCount(0);
  });
});

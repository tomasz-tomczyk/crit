import { test, expect, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import { clearAllComments, loadPage, mdSection, switchToDocumentView, addComment, getMdPath } from './helpers';

// Poll session API until the review round increments past `previousRound`.
async function waitForRound(request: APIRequestContext, previousRound: number) {
  await expect(async () => {
    const session = await request.get('/api/session').then(r => r.json());
    expect(session.review_round).toBeGreaterThan(previousRound);
  }).toPass({ timeout: 5000 });
}

// Finish round, mark all comments as resolved in .crit.json, then complete the round.
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
// Comment Count Badge — header badge shows comment count
// Shows unresolved count when unresolved comments exist,
// falls back to total count when all are resolved.
// ============================================================
test.describe('Comment Count Badge', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);
  });

  test('badge is visible but empty when there are no comments', async ({ page }) => {
    const countEl = page.locator('#commentCount');
    const badgeEl = page.locator('#commentCountNumber');
    await expect(countEl).toBeVisible();
    await expect(badgeEl).toHaveText('');
  });

  test('badge shows 1 after adding a comment', async ({ page }) => {
    const section = mdSection(page);
    const badgeEl = page.locator('#commentCountNumber');

    // Add a comment via UI
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('Badge test comment');
    await page.locator('.comment-form .btn-primary').click();

    await expect(page.locator('#commentCount')).toBeVisible();
    await expect(badgeEl).toHaveText('1');
  });

  test('badge shows unresolved count, not total', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const countEl = page.locator('#commentCount');
    const badgeEl = page.locator('#commentCountNumber');

    // Add a comment, resolve it via .crit.json, then complete the round
    await addComment(request, mdPath, 1, 'Will be resolved');
    await finishAndResolve(request);

    // Now add 2 new unresolved comments (round 2)
    await addComment(request, mdPath, 1, 'Unresolved one');
    await addComment(request, mdPath, 2, 'Unresolved two');

    await loadPage(page);

    // 1 resolved + 2 unresolved → badge should show 2 (unresolved only).
    // Use toPass() to retry: an SSE comments-changed event may transiently
    // update the badge after the initial page load.
    await expect(async () => {
      await expect(countEl).toBeVisible();
      await expect(countEl).not.toHaveClass(/comment-count-resolved/);
      await expect(badgeEl).toHaveText('2');
    }).toPass({ timeout: 5000 });
  });

  test('badge increments when adding multiple comments', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const badgeEl = page.locator('#commentCountNumber');

    // Add two comments via API
    await addComment(request, mdPath, 1, 'First comment');
    await addComment(request, mdPath, 2, 'Second comment');

    // Reload to pick up API-added comments
    await loadPage(page);

    await expect(page.locator('#commentCount')).toBeVisible();
    await expect(badgeEl).toHaveText('2');
  });

  test('badge decrements when a comment is deleted', async ({ page }) => {
    const section = mdSection(page);
    const badgeEl = page.locator('#commentCountNumber');

    // Add two comments via UI
    const lineBlocks = section.locator('.line-block');

    await lineBlocks.first().hover();
    await section.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('Comment one');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toHaveCount(1);

    const secondBlock = lineBlocks.nth(1);
    await secondBlock.hover();
    await section.locator('.line-comment-gutter').nth(1).click();
    await page.locator('.comment-form textarea').fill('Comment two');
    await page.locator('.comment-form .btn-primary').click();
    await expect(section.locator('.comment-card')).toHaveCount(2);
    await expect(badgeEl).toHaveText('2');

    // Delete first comment
    await section.locator('.comment-actions .delete-btn').first().click();
    await expect(section.locator('.comment-card')).toHaveCount(1);
    await expect(badgeEl).toHaveText('1');
  });

  test('badge shows empty text when all comments are deleted', async ({ page }) => {
    const section = mdSection(page);
    const countEl = page.locator('#commentCount');
    const badgeEl = page.locator('#commentCountNumber');

    // Add a comment
    const lineBlock = section.locator('.line-block').first();
    await lineBlock.hover();
    await section.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('Temporary comment');
    await page.locator('.comment-form .btn-primary').click();
    await expect(countEl).toBeVisible();
    await expect(badgeEl).toHaveText('1');

    // Delete it
    await section.locator('.comment-actions .delete-btn').click();
    await expect(countEl).toBeVisible();
    await expect(badgeEl).toHaveText('');
  });

  test('clicking the badge number toggles comments panel', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const badgeEl = page.locator('#commentCountNumber');

    // Add a comment via API and reload
    await addComment(request, mdPath, 1, 'Panel toggle test');
    await loadPage(page);

    await expect(badgeEl).toBeVisible();

    // Click the number to toggle the comments panel
    await badgeEl.click();
    const panel = page.locator('#commentsPanel');
    await expect(panel).not.toHaveClass(/comments-panel-hidden/);

    // Click again to close
    await badgeEl.click();
    await expect(panel).toHaveClass(/comments-panel-hidden/);
  });

  test('badge falls back to total count with resolved styling when only resolved comments exist', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const countEl = page.locator('#commentCount');
    const badgeEl = page.locator('#commentCountNumber');

    // Add a comment, resolve it via .crit.json, then complete the round
    await addComment(request, mdPath, 1, 'Will be resolved');
    await finishAndResolve(request);

    await loadPage(page);

    // 0 unresolved + 1 resolved → badge shows total (1) with muted styling.
    // Use toPass() to retry: an SSE comments-changed event may transiently
    // update the badge after the initial page load.
    await expect(async () => {
      await expect(countEl).toBeVisible();
      await expect(countEl).toHaveClass(/comment-count-resolved/);
      await expect(badgeEl).toHaveText('1');
    }).toPass({ timeout: 5000 });
  });
});

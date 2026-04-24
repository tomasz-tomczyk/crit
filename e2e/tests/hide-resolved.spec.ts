import { test, expect, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import { clearAllComments, loadPage, getMdPath, addComment } from './helpers';

// Create a resolved comment by finishing a round, marking resolved, and round-completing.
async function setupResolvedComment(request: APIRequestContext) {
  const mdPath = await getMdPath(request);
  await addComment(request, mdPath, 1, 'Resolved comment');

  // Finish to write the review file
  const finishRes = await request.post('/api/finish');
  const finishData = await finishRes.json();
  const critJsonPath = finishData.review_file;

  // Mark comment as resolved in the review file
  const critJson = JSON.parse(fs.readFileSync(critJsonPath, 'utf-8'));
  for (const fileKey of Object.keys(critJson.files)) {
    for (const comment of critJson.files[fileKey].comments) {
      comment.resolved = true;
      comment.resolution_note = 'Done';
    }
  }
  fs.writeFileSync(critJsonPath, JSON.stringify(critJson, null, 2));

  // Round-complete to carry forward
  const round = (await request.get('/api/session').then(r => r.json())).review_round;
  await request.post('/api/round-complete');
  await expect(async () => {
    const session = await request.get('/api/session').then(r => r.json());
    expect(session.review_round).toBeGreaterThan(round);
  }).toPass({ timeout: 5000 });
}

test.describe('Hide Resolved', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    // Clear localStorage to start fresh
    await page.goto('/');
    await page.evaluate(() => localStorage.removeItem('crit-hide-resolved'));
  });

  test('settings panel shows Hide resolved toggle', async ({ page }) => {
    await loadPage(page);
    await page.click('#settingsToggle');
    const pane = page.locator('.settings-pane[data-pane="settings"]');
    await expect(pane.locator('.settings-display-label').filter({ hasText: 'Hide resolved' })).toBeVisible();
    await expect(pane.locator('#hideResolvedToggle')).toBeAttached();
  });

  test('toggle hides resolved inline comments', async ({ page, request }) => {
    await setupResolvedComment(request);
    await loadPage(page);

    // Wait for resolved card to render
    await expect(page.locator('.comment-card.resolved-card').first()).toBeVisible();

    // Resolved inline comment block should be visible by default
    const resolvedBlock = page.locator('.comment-block:not(.panel-comment-block)').filter({
      has: page.locator('.resolved-card'),
    });
    await expect(resolvedBlock.first()).toBeVisible();

    // Enable "Hide resolved" via keyboard shortcut
    await page.keyboard.press('h');

    // Resolved inline comment block should now be hidden
    await expect(resolvedBlock.first()).toBeHidden();
  });

  test('toggle does NOT affect resolved comments in side panel', async ({ page, request }) => {
    await setupResolvedComment(request);
    await loadPage(page);

    // Wait for resolved card to render
    await expect(page.locator('.comment-card.resolved-card').first()).toBeVisible();

    // Enable "Hide resolved" via keyboard shortcut
    await page.keyboard.press('h');

    // Open comments panel and show resolved via toggle track
    await page.keyboard.press('Shift+C');
    await page.locator('.comments-panel-filter .comments-panel-switch-track').click();

    // Panel comment cards should still be visible
    const panelCards = page.locator('.panel-comment-block .comment-card');
    await expect(panelCards.first()).toBeVisible();
  });

  test('h keyboard shortcut toggles resolved inline comment visibility', async ({ page, request }) => {
    await setupResolvedComment(request);
    await loadPage(page);

    const resolvedBlock = page.locator('.comment-block:not(.panel-comment-block)').filter({
      has: page.locator('.resolved-card'),
    });
    await expect(resolvedBlock.first()).toBeVisible();

    // Press h to hide
    await page.keyboard.press('h');
    await expect(resolvedBlock.first()).toBeHidden();

    // Press h again to show
    await page.keyboard.press('h');
    await expect(resolvedBlock.first()).toBeVisible();
  });

  test('hide resolved persists via localStorage across reload', async ({ page, request }) => {
    await setupResolvedComment(request);
    await loadPage(page);

    const resolvedBlock = page.locator('.comment-block:not(.panel-comment-block)').filter({
      has: page.locator('.resolved-card'),
    });

    // Enable hide resolved
    await page.keyboard.press('h');
    await expect(resolvedBlock.first()).toBeHidden();

    // Reload
    await loadPage(page);

    // Should still be hidden after reload
    const resolvedBlockAfter = page.locator('.comment-block:not(.panel-comment-block)').filter({
      has: page.locator('.resolved-card'),
    });
    await expect(resolvedBlockAfter.first()).toBeHidden();

    // Verify localStorage value
    const stored = await page.evaluate(() => localStorage.getItem('crit-hide-resolved'));
    expect(stored).toBe('true');
  });
});

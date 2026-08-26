import { test, expect, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import { clearAllComments, loadPage, getMdPath, addComment, getReviewFilePath } from './helpers';

// Create a resolved comment by finishing a round, marking resolved, and round-completing.
async function setupResolvedComment(request: APIRequestContext, line = 1) {
  const mdPath = await getMdPath(request);
  await addComment(request, mdPath, line, 'Resolved comment');

  // Finish to write the review file
  await request.post('/api/finish');
  const critJsonPath = await getReviewFilePath(request);

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

  // Cards hide via CSS; line highlights must update without a full file rebuild.
  test('toggling hide-resolved drops has-comment on resolved ranges without rebuilding', async ({ page, request }) => {
    await setupResolvedComment(request, 1);
    await loadPage(page);
    await page.locator('.file-section').filter({ hasText: 'plan.md' }).locator('.file-header-toggle .toggle-btn[data-mode="document"]').click();
    await expect(page.locator('.document-wrapper')).toBeVisible();

    const section = page.locator('.file-section').filter({ hasText: 'plan.md' });
    await expect(section.locator('.line-block.has-comment').first()).toBeVisible();

    await section.evaluate(el => { (el as HTMLElement).dataset.critPreserveProbe = '1'; });

    await page.keyboard.press('h');
    await expect(section.locator('.comment-block:not(.panel-comment-block)').filter({
      has: page.locator('.resolved-card'),
    }).first()).toBeHidden();
    await expect(section.locator('.line-block.has-comment')).toHaveCount(0);

    const sameNode = await section.evaluate(el => (el as HTMLElement).dataset.critPreserveProbe === '1');
    expect(sameNode).toBe(true);

    await page.keyboard.press('h');
    await expect(section.locator('.line-block.has-comment').first()).toBeVisible();
  });

  test('comment arrows skip hidden resolved comments in both directions', async ({ page, request }) => {
    await setupResolvedComment(request, 3);
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 1, 'Open A');
    await addComment(request, mdPath, 5, 'Open C');
    await loadPage(page);

    const openA = page.locator('.comment-card:not(.resolved-card)').filter({ hasText: 'Open A' });
    const resolvedCard = page.locator('.comment-card.resolved-card').first();
    const openC = page.locator('.comment-card:not(.resolved-card)').filter({ hasText: 'Open C' });

    await page.locator('#commentNavNext').click();
    await expect(openA).toHaveClass(/comment-nav-highlight/);
    await page.locator('#commentNavNext').click();
    await expect(resolvedCard).toHaveClass(/comment-nav-highlight/);

    await page.keyboard.press('h');
    await expect(resolvedCard).toBeHidden();

    await page.locator('#commentNavPrev').click();
    await expect(openA).toHaveClass(/comment-nav-highlight/);
    await expect(openC).not.toHaveClass(/comment-nav-highlight/);
    await expect(resolvedCard).not.toHaveClass(/comment-nav-highlight/);

    await page.keyboard.press('h');
    await page.locator('#commentNavNext').click();
    await expect(resolvedCard).toHaveClass(/comment-nav-highlight/);

    await page.keyboard.press('h');
    await page.locator('#commentNavNext').click();
    await expect(openC).toHaveClass(/comment-nav-highlight/);
    await expect(resolvedCard).not.toHaveClass(/comment-nav-highlight/);
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

    // Verify setting persisted in the consolidated crit-settings cookie
    const stored = await page.evaluate(() => {
      const match = document.cookie.match(/(?:^|;\s*)crit-settings=([^;]+)/);
      if (!match) return null;
      try { return JSON.parse(decodeURIComponent(match[1])).hideResolved; }
      catch { return null; }
    });
    expect(stored).toBe(true);
  });
});

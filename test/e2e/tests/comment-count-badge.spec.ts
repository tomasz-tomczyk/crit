import { test, expect } from '@playwright/test';
import {
  clearAllComments, loadPage, switchToDocumentView, addComment, getMdPath, resolveComment,
  openDocLineComment,
} from './helpers';

// ============================================================
// Comment Count Badge — header badge shows comment count
// Shows unresolved count when unresolved comments exist,
// falls back to total count when all are resolved.
// (Clicking the badge to toggle the panel: comments-panel.spec.ts.)
// ============================================================
test.describe('Comment Count Badge', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('badge counts comments added and deleted through the UI', async ({ page }) => {
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    const countEl = page.locator('#commentCount');
    const badgeEl = page.locator('#commentCountNumber');

    // No comments: visible but empty.
    await expect(countEl).toBeVisible();
    await expect(badgeEl).toHaveText('');

    await (await openDocLineComment(page, doc, 0)).fill('Comment one');
    await doc.locator('.comment-form .btn-primary').click();
    await expect(doc.locator('.comment-card')).toHaveCount(1);
    await expect(countEl).toBeVisible();
    await expect(badgeEl).toHaveText('1');

    await (await openDocLineComment(page, doc, 1)).fill('Comment two');
    await doc.locator('.comment-form .btn-primary').click();
    await expect(doc.locator('.comment-card')).toHaveCount(2);
    await expect(badgeEl).toHaveText('2');

    // Deleting decrements, and the last delete empties the badge.
    await doc.locator('.comment-actions .delete-btn').first().click();
    await expect(doc.locator('.comment-card')).toHaveCount(1);
    await expect(badgeEl).toHaveText('1');

    await doc.locator('.comment-actions .delete-btn').click();
    await expect(doc.locator('.comment-card')).toHaveCount(0);
    await expect(countEl).toBeVisible();
    await expect(badgeEl).toHaveText('');
  });

  test('badge shows unresolved count, not total', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const resolved = await addComment(request, mdPath, 1, 'Will be resolved');
    await resolveComment(request, mdPath, resolved.id);
    await addComment(request, mdPath, 1, 'Unresolved one');
    await addComment(request, mdPath, 2, 'Unresolved two');

    await loadPage(page);

    // 1 resolved + 2 unresolved → badge shows 2 (unresolved only).
    const countEl = page.locator('#commentCount');
    await expect(countEl).toBeVisible();
    await expect(page.locator('#commentCountNumber')).toHaveText('2');
    await expect(countEl).not.toHaveClass(/comment-count-resolved/);
  });

  test('badge falls back to total count with resolved styling when only resolved comments exist', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const resolved = await addComment(request, mdPath, 1, 'Will be resolved');
    await resolveComment(request, mdPath, resolved.id);

    await loadPage(page);

    // 0 unresolved + 1 resolved → badge shows total (1) with muted styling.
    const countEl = page.locator('#commentCount');
    await expect(countEl).toBeVisible();
    await expect(page.locator('#commentCountNumber')).toHaveText('1');
    await expect(countEl).toHaveClass(/comment-count-resolved/);
  });
});

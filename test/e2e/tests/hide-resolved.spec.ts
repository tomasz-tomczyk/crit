import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import {
  clearAllComments, loadPage, getMdPath, addComment, resolveComment, mdSection,
  switchToDocumentView, mdDocument, clickWhenHittable, recordClassAdditions, expectClassAddedTo,
} from './helpers';

// A resolved comment on plan.md, through the resolve API.
async function seedResolvedComment(request: APIRequestContext, line = 1): Promise<string> {
  const mdPath = await getMdPath(request);
  const comment = await addComment(request, mdPath, line, 'Resolved comment');
  await resolveComment(request, mdPath, comment.id);
  return comment.id;
}

// Inline (not panel) comment blocks that hold a resolved card.
function resolvedBlocks(page: Page) {
  return page.locator('.comment-block:not(.panel-comment-block)').filter({
    has: page.locator('.resolved-card'),
  });
}

test.describe('Hide Resolved', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('settings toggle exists; h hides and shows resolved comments and their document highlight', async ({ page, request }) => {
    await seedResolvedComment(request, 1);
    await loadPage(page);

    // The settings panel offers the toggle.
    await page.click('#settingsToggle');
    const pane = page.locator('.settings-pane[data-pane="settings"]');
    await expect(pane.locator('.settings-display-label').filter({ hasText: 'Hide resolved' })).toBeVisible();
    await expect(pane.locator('#hideResolvedToggle')).toBeAttached();
    await page.keyboard.press('Escape');
    await expect(page.locator('.settings-overlay')).not.toHaveClass(/active/);

    // Diff view: the resolved inline comment is visible by default; h toggles it.
    await mdSection(page);
    await expect(resolvedBlocks(page).first()).toBeVisible();
    await page.keyboard.press('h');
    await expect(resolvedBlocks(page).first()).toBeHidden();
    await page.keyboard.press('h');
    await expect(resolvedBlocks(page).first()).toBeVisible();

    // Document view: hiding also drops the has-comment highlight on the
    // resolved range, and showing brings it back. (Under Pierre the document
    // annotation is rebuilt on toggle, so node identity is not asserted.)
    await switchToDocumentView(page);
    const doc = mdDocument(page);
    await expect(doc.locator('.line-block.has-comment').first()).toBeVisible();
    await expect(doc.locator('.resolved-card').first()).toBeVisible();

    await page.keyboard.press('h');
    await expect(doc.locator('.comment-block:not(.panel-comment-block)').filter({
      has: page.locator('.resolved-card'),
    }).first()).toBeHidden();
    await expect(doc.locator('.document-wrapper')).toBeVisible();
    await expect(doc.locator('.line-block.has-comment')).toHaveCount(0);

    await page.keyboard.press('h');
    await expect(doc.locator('.line-block.has-comment').first()).toBeVisible();
    await expect(doc.locator('.resolved-card').first()).toBeVisible();
  });

  test('comment arrows skip hidden resolved comments in both directions', async ({ page, request }) => {
    const resolved = await seedResolvedComment(request, 3);
    const mdPath = await getMdPath(request);
    const openA = (await addComment(request, mdPath, 1, 'Open A')).id as string;
    const openC = (await addComment(request, mdPath, 5, 'Open C')).id as string;
    await loadPage(page);
    await mdSection(page);
    await expect(page.locator('.comment-card.resolved-card')).toBeVisible();
    await recordClassAdditions(page, 'comment-nav-highlight');

    const next = () => clickWhenHittable(page, page.locator('#commentNavNext'));
    const prev = () => clickWhenHittable(page, page.locator('#commentNavPrev'));

    await expectClassAddedTo(page, 'comment-nav-highlight', next, openA);
    await expectClassAddedTo(page, 'comment-nav-highlight', next, resolved);

    // Hidden: from the (now hidden) resolved comment, prev skips to A.
    await page.keyboard.press('h');
    await expect(page.locator('.comment-card.resolved-card')).toBeHidden();
    await expectClassAddedTo(page, 'comment-nav-highlight', prev, openA);

    // Shown again: next lands on the resolved comment.
    await page.keyboard.press('h');
    await expect(page.locator('.comment-card.resolved-card')).toBeVisible();
    await expectClassAddedTo(page, 'comment-nav-highlight', next, resolved);

    // Hidden: next from it skips straight to C.
    await page.keyboard.press('h');
    await expect(page.locator('.comment-card.resolved-card')).toBeHidden();
    await expectClassAddedTo(page, 'comment-nav-highlight', next, openC);
  });

  test('hide resolved persists in the settings cookie across reload', async ({ page, request }) => {
    await seedResolvedComment(request, 1);
    const mdPath = await getMdPath(request);
    await addComment(request, mdPath, 3, 'Open comment');
    await loadPage(page);
    await mdSection(page);
    await expect(resolvedBlocks(page).first()).toBeVisible();

    await page.keyboard.press('h');
    await expect(resolvedBlocks(page).first()).toBeHidden();

    await loadPage(page);
    await mdSection(page);
    // The open comment rendered, so the resolved one would have too: it is
    // hidden because the setting survived the reload.
    await expect(page.locator('.comment-card', { hasText: 'Open comment' })).toBeVisible();
    await expect(resolvedBlocks(page)).toBeHidden();
    await expect(page.locator('.comment-card.resolved-card')).toBeHidden();

    // Persisted in the consolidated crit-settings cookie
    const stored = await page.evaluate(() => {
      const match = document.cookie.match(/(?:^|;\s*)crit-settings=([^;]+)/);
      if (!match) return null;
      try { return JSON.parse(decodeURIComponent(match[1])).hideResolved; }
      catch { return null; }
    });
    expect(stored).toBe(true);
  });
});

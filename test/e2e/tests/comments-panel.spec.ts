import { test, expect, type Page } from '@playwright/test';
import * as fs from 'fs';
import {
  clearAllComments, loadPage, switchToDocumentView, addComment, getMdPath, getReviewFilePath,
  openDocLineComment, resolveComment, roundComplete, waitForScrollStable, reviewScroller,
  recordClassAdditions, expectClassAddedTo, classAdditions,
} from './helpers';

// Comment links and highlighted code in panel cards: comment-markdown.spec.ts.

const HIGHLIGHT = 'comment-card-highlight';

function commentsPanel(page: Page) {
  return page.locator('#commentsPanel');
}

function panelCards(page: Page) {
  return page.locator('.panel-comment-block .comment-card');
}

function panelCard(page: Page, id: string) {
  return page.locator(`.panel-comment-block .comment-card[data-comment-id="${id}"]`);
}

// The inline (not panel) card for a comment. Not revealed through the tree:
// mdSection() would scroll the file header back into view and undo a jump.
function inlineCard(page: Page, id: string) {
  return page.locator(`.comment-block:not(.panel-comment-block) .comment-card[data-comment-id="${id}"]`);
}

// ============================================================
// Comments Panel — Git Mode
// ============================================================
test.describe('Comments Panel — Git Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('panel is hidden by default, toggles with Shift+C and the close button, and is not kept across reloads', async ({ page }) => {
    await loadPage(page);
    await expect(commentsPanel(page)).toHaveClass(/comments-panel-hidden/);

    await page.keyboard.press('Shift+C');
    await expect(commentsPanel(page)).not.toHaveClass(/comments-panel-hidden/);
    // Empty state when there are no comments.
    await expect(page.locator('.comments-panel-empty')).toBeVisible();
    await expect(page.locator('.comments-panel-empty')).toContainText('No comments yet');

    await page.keyboard.press('Shift+C');
    await expect(commentsPanel(page)).toHaveClass(/comments-panel-hidden/);

    await page.keyboard.press('Shift+C');
    await expect(commentsPanel(page)).not.toHaveClass(/comments-panel-hidden/);
    await page.locator('.comments-panel-close').click();
    await expect(commentsPanel(page)).toHaveClass(/comments-panel-hidden/);

    // The shortcuts pane documents the toggle.
    await page.keyboard.press('?');
    await expect(page.locator('.settings-overlay')).toHaveClass(/active/);
    await expect(page.locator('.settings-pane[data-pane="shortcuts"]')).toContainText('Toggle comments panel');
    await page.keyboard.press('Escape');
    await expect(page.locator('.settings-overlay')).not.toHaveClass(/active/);

    // Open state is not persisted across reloads.
    await page.keyboard.press('Shift+C');
    await expect(commentsPanel(page)).not.toHaveClass(/comments-panel-hidden/);
    await loadPage(page);
    await expect(commentsPanel(page)).toHaveClass(/comments-panel-hidden/);
  });

  test('panel lists comments with line references, file groups and actions', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const session = await (await request.get('/api/session')).json();
    const goPath = session.files.find((f: { path: string }) => f.path.endsWith('.go'))?.path;
    expect(goPath).toBeTruthy();

    await addComment(request, mdPath, 1, 'First git comment');
    await addComment(request, mdPath, 3, 'Second git comment');
    const lineRef = await addComment(request, mdPath, 5, 'Line ref test');
    const rangeResp = await request.post(`/api/file/comments?path=${encodeURIComponent(mdPath)}`, {
      data: { start_line: 2, end_line: 4, body: 'Range comment' },
    });
    expect(rangeResp.ok()).toBeTruthy();
    const range = await rangeResp.json();
    await addComment(request, goPath, 1, 'Go comment');
    await loadPage(page);

    // Clicking the count badge number opens the panel (and closes it again below).
    const badgeEl = page.locator('#commentCountNumber');
    await expect(badgeEl).toBeVisible();
    await badgeEl.click();
    await expect(commentsPanel(page)).not.toHaveClass(/comments-panel-hidden/);

    await expect(panelCards(page)).toHaveCount(5);
    const bodies = await panelCards(page).locator('.comment-body').allTextContents();
    const firstIdx = bodies.findIndex(b => b.includes('First git comment'));
    const secondIdx = bodies.findIndex(b => b.includes('Second git comment'));
    expect(firstIdx).toBeGreaterThanOrEqual(0);
    expect(secondIdx).toBeGreaterThan(firstIdx);

    // Line references: single line and multi-line range.
    await expect(panelCard(page, lineRef.id).locator('.comment-line-ref')).toContainText('Line 5');
    await expect(panelCard(page, range.id).locator('.comment-line-ref')).toContainText('Lines 2-4');

    // One file name header per file with comments.
    await expect(page.locator('.comments-panel-file-name')).toHaveCount(2);

    // Fresh comments: no carried-forward badges, a Resolve action, and no
    // resolved filter while nothing is resolved.
    const card = panelCard(page, lineRef.id);
    await expect(card.locator('.carried-forward-label')).toHaveCount(0);
    await expect(card.locator('.resolve-btn--active')).toHaveCount(0);
    await expect(card.locator('.comment-actions button[title="Resolve"]')).toBeVisible();
    await expect(page.locator('#commentsPanelFilter')).toBeHidden();

    await badgeEl.click();
    await expect(commentsPanel(page)).toHaveClass(/comments-panel-hidden/);
  });

  test('panel follows comments added and deleted inline', async ({ page }) => {
    await loadPage(page);
    const doc = await switchToDocumentView(page);

    await page.keyboard.press('Shift+C');
    await expect(page.locator('.comments-panel-empty')).toBeVisible();

    await (await openDocLineComment(page, doc)).fill('Added via UI');
    await doc.locator('.comment-form .btn-primary').click();
    await expect(panelCards(page)).toHaveCount(1);
    await expect(panelCards(page).first().locator('.comment-body')).toContainText('Added via UI');

    await doc.locator('.comment-card .delete-btn').click();
    await expect(page.locator('.comments-panel-empty')).toBeVisible();
  });

  test('clicking a panel card scrolls to and flashes the inline comment', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const file = await request.get(`/api/file?path=${encodeURIComponent(mdPath)}`).then(r => r.json());
    const targetLine = (file.content as string).split('\n').reduce((last, line, index) => line.trim() ? index + 1 : last, 0);
    const target = await addComment(request, mdPath, targetLine, 'Scroll target');
    await loadPage(page);
    await switchToDocumentView(page);
    await recordClassAdditions(page, HIGHLIGHT);

    // Start away from the comment (the last line of plan.md).
    await reviewScroller(page).evaluate(el => { el.scrollTop = 0; });
    await waitForScrollStable(page);
    await expect(inlineCard(page, target.id)).not.toBeInViewport();

    await page.keyboard.press('Shift+C');
    await expectClassAddedTo(page, HIGHLIGHT, () => panelCard(page, target.id).click(), target.id);
    await waitForScrollStable(page);
    await expect(inlineCard(page, target.id)).toBeVisible();
    await expect(inlineCard(page, target.id)).toBeInViewport();
  });

  test('resolve and unresolve from the panel, for file and review-level comments', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const fileComment = await addComment(request, mdPath, 1, 'Panel resolve test');
    const reviewResp = await request.post('/api/comments', { data: { body: 'Review-level resolve test' } });
    expect(reviewResp.ok()).toBeTruthy();
    const reviewComment = await reviewResp.json();

    await loadPage(page);
    await switchToDocumentView(page);
    await expect(inlineCard(page, fileComment.id)).toBeVisible();
    await recordClassAdditions(page, HIGHLIGHT);
    await page.keyboard.press('Shift+C');

    // Resolve: card turns resolved and offers Unresolve.
    const card = panelCard(page, fileComment.id);
    await card.locator('.comment-actions button[title="Resolve"]').click();
    await expect(card).toHaveClass(/resolved-card/);
    await expect(card.locator('.comment-actions button[title="Unresolve"]')).toBeVisible();

    // Resolving did not navigate to the inline card: the only highlight is the
    // one a deliberate click on the (now collapsed) card produces afterwards.
    await expectClassAddedTo(page, HIGHLIGHT, () => card.locator('.comment-header-left').click(), fileComment.id);
    expect(await classAdditions(page, HIGHLIGHT)).toEqual([fileComment.id]);

    // Unresolve from the Resolved filter; it shows under Open again.
    await page.locator('#commentsFilterPill .toggle-btn[data-filter="resolved"]').click();
    await expect(panelCards(page)).toHaveCount(1);
    await card.locator('.comment-actions button[title="Unresolve"]').click();
    await page.locator('#commentsFilterPill .toggle-btn[data-filter="open"]').click();
    await expect(card).toBeVisible();
    await expect(card).not.toHaveClass(/resolved-card/);
    await expect(card.locator('.comment-actions button[title="Resolve"]')).toBeVisible();

    // Review-level comments resolve from the panel too (All filter keeps
    // the card listed once it is resolved).
    await page.locator('#commentsFilterPill .toggle-btn[data-filter="all"]').click();
    const reviewCard = panelCard(page, reviewComment.id);
    await expect(reviewCard.locator('.comment-actions button[title="Resolve"]')).toBeVisible();
    await reviewCard.locator('.comment-actions button[title="Resolve"]').click();
    await expect(reviewCard).toHaveClass(/resolved-card/);
    await expect(reviewCard.locator('.comment-actions button[title="Unresolve"]')).toBeVisible();
  });

  // Resolution written by the agent into the review file and carried into the
  // next round (not the resolve API).
  test('a resolved carried-forward comment shows as resolved and its card scrolls to the inline comment', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, 1, 'Resolve and scroll');

    // Finish to write the review file, mark the comment resolved in it, then
    // complete the round so it is carried forward.
    await request.post('/api/finish');
    const critJsonPath = await getReviewFilePath(request);
    const critJson = JSON.parse(fs.readFileSync(critJsonPath, 'utf-8'));
    for (const fileKey of Object.keys(critJson.files)) {
      for (const c of critJson.files[fileKey].comments) {
        c.resolved = true;
        c.resolution_note = 'Fixed';
      }
    }
    fs.writeFileSync(critJsonPath, JSON.stringify(critJson, null, 2));
    await roundComplete(request);

    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await recordClassAdditions(page, HIGHLIGHT);
    await page.keyboard.press('Shift+C');

    await page.locator('#commentsFilterPill .toggle-btn[data-filter="resolved"]').click();
    await expect(panelCards(page)).toHaveCount(1);
    const card = panelCards(page).first();
    await expect(card).toHaveClass(/resolved-card/);
    const id = await card.getAttribute('data-comment-id');
    expect(id).toBe(comment.id);

    await expectClassAddedTo(page, HIGHLIGHT, () => card.click(), comment.id);
    const inlineResolved = doc.locator(`.comment-card.resolved-card[data-comment-id="${comment.id}"]`);
    await expect(inlineResolved).toBeVisible();
    await expect(inlineResolved).toBeInViewport();
  });
});

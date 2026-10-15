import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import * as fs from 'fs';
import {
  clearAllComments, fileHeader, getReviewFilePath, loadPage, revealFile,
  currentRound, roundComplete, resolveComment, treePaths,
} from './helpers';

// Plain API behaviour of finish / round-complete / wait-for-event is covered
// by Go tests (internal/server/server_test.go TestFinish, TestFinish_NoComments,
// TestFinish_IncludesStructuredComments, TestWaitForEventReturnsOnFinish,
// TestWaitForEventIgnoresOtherEvents; internal/session session_test.go
// TestAddCommentSetsReviewRound, TestCarryForwardPreservesReviewRound;
// round_complete_persist_test.go TestHandleRoundCompleteGit_PersistsCarriedComments).
// This spec keeps the end-to-end paths: the agent editing review.json on disk,
// the browser's waiting overlay and the no-changes confirmation.

const FILE = 'plan.md';

interface ReviewFileComment { resolved?: boolean; resolution_note?: string }
interface ReviewFile { files: Record<string, { comments: ReviewFileComment[] }> }

async function addFileComment(request: APIRequestContext, line: number, body: string) {
  const resp = await request.post(`/api/file/comments?path=${encodeURIComponent(FILE)}`, {
    data: { start_line: line, end_line: line, body },
  });
  await expect(resp).toBeOK();
  return resp.json() as Promise<{ id: string }>;
}

async function fileComments(request: APIRequestContext) {
  const resp = await request.get(`/api/file/comments?path=${encodeURIComponent(FILE)}`);
  await expect(resp).toBeOK();
  return resp.json() as Promise<Array<{ id: string; body: string; resolved?: boolean; carried_forward?: boolean; review_round: number }>>;
}

// Finish the round (writes review.json), let the agent edit review.json the
// way a real agent does, then signal round-complete and wait for carry-forward.
// `edit` receives each file's comments in creation order.
async function agentRound(request: APIRequestContext, edit?: (comments: ReviewFileComment[]) => void) {
  await expect(await request.post('/api/finish')).toBeOK();
  if (edit) {
    const reviewPath = await getReviewFilePath(request);
    const review = JSON.parse(fs.readFileSync(reviewPath, 'utf-8')) as ReviewFile;
    for (const key of Object.keys(review.files)) edit(review.files[key].comments);
    fs.writeFileSync(reviewPath, JSON.stringify(review, null, 2));
  }
  await roundComplete(request);
}

const resolveAll = (note: string) => (comments: ReviewFileComment[]) => {
  for (const c of comments) { c.resolved = true; c.resolution_note = note; }
};

async function clickFinish(page: Page) {
  await page.locator('#finishBtn').click();
  await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
}

// ============================================================
// Multi-Round — agent edits review.json on disk
// ============================================================
test.describe('Multi-Round — API', () => {
  test.beforeEach(async ({ request }) => {
    await roundComplete(request);
    await clearAllComments(request);
  });

  test('round-complete advances the round, keeps the file list, and carries forward agent-resolved comments', async ({ request }) => {
    const before = await request.get('/api/session').then(r => r.json());
    const filesBefore = before.files.map((f: { path: string }) => f.path).sort();
    await addFileComment(request, 1, 'Will be resolved');

    // Simulate the agent marking the comment resolved in the review file.
    await agentRound(request, resolveAll('Fixed'));

    const after = await request.get('/api/session').then(r => r.json());
    expect(after.review_round).toBe(before.review_round + 1);
    expect(after.files.map((f: { path: string }) => f.path).sort()).toEqual(filesBefore);

    const comments = await fileComments(request);
    expect(comments).toHaveLength(1);
    expect(comments[0].body).toBe('Will be resolved');
    expect(comments[0].resolved).toBe(true);
    expect(comments[0].carried_forward).toBe(true);
    expect(comments[0].review_round).toBe(before.review_round);
  });
});

// ============================================================
// Multi-Round — Frontend Behavior
// ============================================================
test.describe('Multi-Round — Frontend', () => {
  test.beforeEach(async ({ request }) => {
    // Reset server to reviewing state in case a previous test left it in
    // waiting/finished state, and wait for the round to settle so the review
    // file write it triggers can't land in the middle of the next test.
    await roundComplete(request);
    await clearAllComments(request);
  });

  test('finish shows the waiting overlay; round-complete SSE exits it and re-renders with the comment carried forward', async ({ page, request }) => {
    await addFileComment(request, 1, 'Unresolved survives round');
    await loadPage(page);
    await revealFile(page, FILE);
    await expect(page.locator('.comment-card .comment-body')).toContainText('Unresolved survives round');
    const files = (await treePaths(page)).length;

    await clickFinish(page);
    await expect(page.locator('#waitingPrompt')).toContainText('crit');

    // Agent signals round-complete; the UI leaves the waiting state.
    await request.post('/api/round-complete');
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/);

    // Finish is available again — the comment persists, so "Finish Review".
    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');
    await expect(page.locator('#finishBtn')).toBeEnabled();

    // File sections re-rendered, and the unresolved comment was carried forward.
    await expect(page.locator('.tree-file')).toHaveCount(files);
    await expect(page.locator('#filesContainer .loading')).toHaveCount(0);
    await revealFile(page, FILE);
    await expect(page.locator('.comment-card')).toHaveCount(1);
    await expect(page.locator('.comment-card .comment-body')).toContainText('Unresolved survives round');
    await expect(page.locator('#commentCount')).toBeVisible();
  });

  test('finish with no comments approves directly with the "Done reviewing" summary', async ({ page }) => {
    await loadPage(page);
    await clickFinish(page);

    // No comments at all: no no-changes confirmation, straight to approval.
    await expect(page.locator('#noChangesOverlay')).not.toHaveClass(/active/);
    await expect(page.locator('#waitingDialog')).toHaveClass(/approved/);
    await expect(page.locator('#waitingHeading')).toHaveText('Approved');
    await expect(page.locator('#summaryReceipt')).toBeVisible();
    await expect(page.locator('#summaryLine')).toContainText('Done reviewing');
  });

  test('an agent-resolved comment renders collapsed with Unresolve, expands, and turns Finish into Approve', async ({ page, request }) => {
    await addFileComment(request, 1, 'Will be resolved visually');
    await agentRound(request, resolveAll('Done'));
    await loadPage(page);
    await revealFile(page, FILE);

    const resolved = page.locator('.comment-card.resolved-card');
    await expect(resolved).toHaveCount(1);
    await expect(resolved).toHaveClass(/collapsed/);
    await expect(page.locator('.comment-actions button[title="Unresolve"]')).toBeVisible();

    // Chevron expands to the body, then collapses again.
    await resolved.locator('.comment-collapse-btn').click();
    await expect(resolved).not.toHaveClass(/collapsed/);
    await expect(resolved.locator('.comment-body')).toContainText('Will be resolved visually');
    await resolved.locator('.comment-collapse-btn').click();
    await expect(resolved).toHaveClass(/collapsed/);

    // All comments resolved — the button says "Approve".
    await expect(page.locator('#finishBtn')).toHaveText('Approve');
  });

  test('resolved comments are excluded from the comment count', async ({ page, request }) => {
    await addFileComment(request, 1, 'Will be resolved');
    await addFileComment(request, 2, 'Stays open');
    // Resolve only the first comment.
    await agentRound(request, comments => { comments[0].resolved = true; });
    await loadPage(page);
    await revealFile(page, FILE);

    // Only the unresolved comment counts — resolved-only would be dimmed.
    await expect(page.locator('#commentCount')).toBeVisible();
    await expect(page.locator('#commentCount')).not.toHaveClass(/comment-count-resolved/);
    await expect(page.locator('#commentCountNumber')).toHaveText('1');
    await expect(page.locator('.comment-card.resolved-card')).toHaveCount(1);
    await expect(page.locator('.comment-card:not(.resolved-card)')).toHaveCount(1);
  });

  test('viewed state persists across round-complete', async ({ page, request }) => {
    await loadPage(page);
    const firstPath = (await treePaths(page))[0];
    await revealFile(page, firstPath);
    const viewedCheckbox = fileHeader(page, firstPath).locator('.file-header-viewed input');
    await viewedCheckbox.check();
    await expect(viewedCheckbox).toBeChecked();

    await clickFinish(page);
    await request.post('/api/round-complete');
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/);

    // Viewed checkbox should still be checked after the round transition
    await revealFile(page, firstPath);
    await expect(fileHeader(page, firstPath).locator('.file-header-viewed input')).toBeChecked();
    await expect(page.locator(`.tree-file[data-tree-path="${firstPath}"]`)).toHaveClass(/viewed/);
  });

  test('round badges: a carried-forward comment shows its original round as latest, a new one the current round, inline and in the panel', async ({ page, request }) => {
    const round1 = await currentRound(request);
    const carried = await addFileComment(request, 1, 'Carried badge test');
    await agentRound(request);
    const fresh = await addFileComment(request, 2, 'Current round badge test');
    await loadPage(page);
    await revealFile(page, FILE);

    // The comment from round1 carries its original round, and is marked
    // round-latest since it was addressed in the most recent round.
    const badge = (id: string) => page.locator(`#filesContainer .comment-card[data-comment-id="${id}"] .comment-round-badge`);
    await expect(page.locator('#filesContainer .comment-round-badge')).toHaveCount(2);
    await expect(badge(carried.id)).toHaveText('R' + round1);
    await expect(badge(carried.id)).toHaveClass(/round-latest/);
    // A comment made this round carries the current round.
    await expect(badge(fresh.id)).toHaveText('R' + (round1 + 1));
    await expect(badge(fresh.id)).toHaveClass(/round-current/);

    await page.locator('#commentCount').click();
    await expect(page.locator('#commentsPanel')).toBeVisible();
    const panelBadge = (id: string) => page.locator(`#commentsPanel .comment-card[data-comment-id="${id}"] .comment-round-badge`);
    await expect(panelBadge(carried.id)).toHaveText('R' + round1);
    await expect(panelBadge(fresh.id)).toHaveText('R' + (round1 + 1));
  });
});

// ============================================================
// No-Changes Confirmation
// ============================================================
test.describe('No-Changes Confirmation', () => {
  test.beforeEach(async ({ request }) => {
    await roundComplete(request);
    await clearAllComments(request);
  });

  // Round 1: comment(s) and finish; round 2: agent signals round-complete
  // without resolving anything, so every comment is carried forward.
  async function carriedForward(request: APIRequestContext, ...bodies: string[]) {
    for (let i = 0; i < bodies.length; i++) await addFileComment(request, i + 1, bodies[i]);
    await agentRound(request);
    return fileComments(request);
  }

  test('finishing with only carried-forward comments asks first; Escape and Go back return to the review', async ({ page, request }) => {
    await carriedForward(request, 'Carried forward comment');
    await loadPage(page);
    await expect(page.locator('#finishBtn')).toBeEnabled();

    await page.locator('#finishBtn').click();
    await expect(page.locator('#noChangesOverlay')).toHaveClass(/active/);
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/);

    await page.keyboard.press('Escape');
    await expect(page.locator('#noChangesOverlay')).not.toHaveClass(/active/);

    await page.locator('#finishBtn').click();
    await expect(page.locator('#noChangesOverlay')).toHaveClass(/active/);
    await page.locator('#noChangesGoBack').click();
    await expect(page.locator('#noChangesOverlay')).not.toHaveClass(/active/);
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/);
    await expect(page.locator('#finishBtn')).toBeEnabled();
  });

  test('send anyway proceeds to waiting state', async ({ page, request }) => {
    await carriedForward(request, 'Send anyway test');
    await loadPage(page);
    await page.locator('#finishBtn').click();
    await expect(page.locator('#noChangesOverlay')).toHaveClass(/active/);

    await page.locator('#noChangesSendAnyway').click();
    await expect(page.locator('#noChangesOverlay')).not.toHaveClass(/active/);
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
  });

  test('resolve all resolves comments and finishes as approved', async ({ page, request }) => {
    await carriedForward(request, 'Resolve all test');
    await loadPage(page);
    await page.locator('#finishBtn').click();
    await expect(page.locator('#noChangesOverlay')).toHaveClass(/active/);

    await page.locator('#noChangesResolveAll').click();
    await expect(page.locator('#noChangesOverlay')).not.toHaveClass(/active/);
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);

    await expect.poll(async () => (await fileComments(request)).every(c => c.resolved)).toBe(true);
  });

  test('no confirmation when user added a new comment', async ({ page, request }) => {
    await carriedForward(request, 'Original comment');
    // A new comment this round (not carried forward).
    await addFileComment(request, 2, 'New comment this round');
    await loadPage(page);

    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
    await expect(page.locator('#noChangesOverlay')).not.toHaveClass(/active/);
  });

  test('no confirmation when all comments are resolved', async ({ page, request }) => {
    const [comment] = await carriedForward(request, 'To be resolved manually');
    await resolveComment(request, FILE, comment.id);
    await loadPage(page);

    // No unresolved comments: the button says Approve and goes straight to waiting.
    await expect(page.locator('#finishBtn')).toHaveText('Approve');
    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
    await expect(page.locator('#noChangesOverlay')).not.toHaveClass(/active/);
  });

  test('shows confirmation when user resolved some but not all comments', async ({ page, request }) => {
    const comments = await carriedForward(request, 'Will be resolved', 'Will stay unresolved');
    await resolveComment(request, FILE, comments.find(c => c.body === 'Will be resolved')!.id);
    await loadPage(page);

    // One comment still unresolved and no new feedback → confirmation.
    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');
    await page.locator('#finishBtn').click();
    await expect(page.locator('#noChangesOverlay')).toHaveClass(/active/);
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/);
  });
});

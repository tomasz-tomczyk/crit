import { test, expect, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import { clearAllComments, loadPage, switchToDocumentView } from './helpers';

// Find a non-crit.json file path from the session (e.g., plan.md or handler.js)
async function getTestFilePath(request: APIRequestContext): Promise<string> {
  const sessionRes = await request.get('/api/session');
  const session = await sessionRes.json();
  const file = session.files.find((f: any) => f.path !== '.crit.json' && f.status !== 'deleted');
  return file?.path || session.files[0].path;
}

// Poll session API until the review round increments past `previousRound`.
async function waitForRound(request: APIRequestContext, previousRound: number) {
  await expect(async () => {
    const session = await request.get('/api/session').then(r => r.json());
    expect(session.review_round).toBeGreaterThan(previousRound);
  }).toPass({ timeout: 5000 });
}

// ============================================================
// Multi-Round — API Behavior
// ============================================================
test.describe('Multi-Round — API', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('session starts at round 1', async ({ request }) => {
    const res = await request.get('/api/session');
    const session = await res.json();
    expect(session.review_round).toBeGreaterThanOrEqual(1);
  });

  test('POST /api/finish returns status and review_file', async ({ request }) => {
    const res = await request.post('/api/finish');
    const data = await res.json();
    expect(data.status).toBe('finished');
    expect(data.review_file).toContain('.crit.json');
  });

  test('POST /api/finish with comments returns a prompt', async ({ request }) => {
    // Add a comment first
    const filePath = await getTestFilePath(request);

    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Test comment for prompt' },
    });

    const res = await request.post('/api/finish');
    const data = await res.json();
    expect(data.prompt).toContain('.crit.json');
    expect(data.prompt).toContain('crit');
  });

  test('GET /api/wait-for-event returns finish event when review finishes', async ({ request }) => {
    // Add a comment so the prompt is non-empty
    const filePath = await getTestFilePath(request);
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Wait-for-event test' },
    });

    // Start long-poll and finish in parallel
    const [waitRes] = await Promise.all([
      request.get('/api/wait-for-event'),
      // Small delay to ensure long-poll is subscribed before finish fires
      new Promise(resolve => setTimeout(resolve, 50)).then(() =>
        request.post('/api/finish')
      ),
    ]);

    expect(waitRes.ok()).toBeTruthy();
    const event = await waitRes.json();
    expect(event.type).toBe('finish');
    expect(event.content).toContain('.crit.json');
  });

  test('GET /api/wait-for-event ignores non-finish events', async ({ request }) => {
    const filePath = await getTestFilePath(request);
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Ignore test' },
    });

    // Start long-poll, add a comment (non-finish event), then finish
    const [waitRes] = await Promise.all([
      request.get('/api/wait-for-event'),
      (async () => {
        await new Promise(resolve => setTimeout(resolve, 50));
        // This triggers a comments-changed event — should NOT unblock long-poll
        await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
          data: { start_line: 2, end_line: 2, body: 'Another comment' },
        });
        await new Promise(resolve => setTimeout(resolve, 50));
        // This triggers a finish event — SHOULD unblock long-poll
        await request.post('/api/finish');
      })(),
    ]);

    expect(waitRes.ok()).toBeTruthy();
    const event = await waitRes.json();
    expect(event.type).toBe('finish');
  });

  test('POST /api/finish with no comments returns empty prompt', async ({ request }) => {
    const res = await request.post('/api/finish');
    const data = await res.json();
    expect(data.prompt).toBe('');
  });

  test('POST /api/round-complete increments the round', async ({ request }) => {
    // Verify starting round
    let session = await request.get('/api/session').then(r => r.json());
    const startRound = session.review_round;

    // Signal round complete
    const res = await request.post('/api/round-complete');
    expect(res.ok()).toBeTruthy();
    const data = await res.json();
    expect(data.status).toBe('ok');

    // Wait for round to increment
    await waitForRound(request, startRound);

    // Verify round incremented
    session = await request.get('/api/session').then(r => r.json());
    expect(session.review_round).toBe(startRound + 1);
  });

  test('round-complete carries forward unresolved comments', async ({ request }) => {
    // Add a comment (unresolved by default)
    const filePath = await getTestFilePath(request);

    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Unresolved comment' },
    });

    // Finish to write .crit.json
    await request.post('/api/finish');

    // Signal round complete and wait for processing
    const round1 = (await request.get('/api/session').then(r => r.json())).review_round;
    await request.post('/api/round-complete');
    await waitForRound(request, round1);

    // Unresolved comment should be carried forward
    const comments = await request.get(`/api/file/comments?path=${encodeURIComponent(filePath)}`).then(r => r.json());
    expect(comments.length).toBe(1);
    expect(comments[0].body).toBe('Unresolved comment');
    expect(comments[0].carried_forward).toBe(true);
  });

  test('round-complete carries forward resolved comments with resolved fields', async ({ request }) => {
    // Add a comment
    const filePath = await getTestFilePath(request);

    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Will be resolved' },
    });

    // Finish to write .crit.json (get the path from response)
    const finishRes = await request.post('/api/finish');
    const finishData = await finishRes.json();
    const critJsonPath = finishData.review_file;

    // Simulate agent marking comment as resolved by editing .crit.json
    const critJson = JSON.parse(fs.readFileSync(critJsonPath, 'utf-8'));
    for (const fileKey of Object.keys(critJson.files)) {
      for (const comment of critJson.files[fileKey].comments) {
        comment.resolved = true;
      }
    }
    fs.writeFileSync(critJsonPath, JSON.stringify(critJson, null, 2));

    // Signal round complete and wait for processing
    const round2 = (await request.get('/api/session').then(r => r.json())).review_round;
    await request.post('/api/round-complete');
    await waitForRound(request, round2);

    // Resolved comment should be carried forward with resolved fields preserved
    const comments = await request.get(`/api/file/comments?path=${encodeURIComponent(filePath)}`).then(r => r.json());
    expect(comments.length).toBe(1);
    expect(comments[0].body).toBe('Will be resolved');
    expect(comments[0].resolved).toBe(true);
    expect(comments[0].carried_forward).toBe(true);
  });

  test('file list is preserved after round-complete', async ({ request }) => {
    const before = await request.get('/api/session').then(r => r.json());
    const filesBefore = before.files.map((f: any) => f.path).filter((p: string) => p !== '.crit.json').sort();

    await request.post('/api/round-complete');
    await waitForRound(request, before.review_round);

    const after = await request.get('/api/session').then(r => r.json());
    const filesAfter = after.files.map((f: any) => f.path).filter((p: string) => p !== '.crit.json').sort();

    expect(filesAfter).toEqual(filesBefore);
  });

  test('comments include review_round from when they were created', async ({ request }) => {
    const filePath = await getTestFilePath(request);

    const session = await request.get('/api/session').then(r => r.json());
    const currentRound = session.review_round;

    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Round comment' },
    });

    const comments = await request.get(`/api/file/comments?path=${encodeURIComponent(filePath)}`).then(r => r.json());
    expect(comments[0].review_round).toBe(currentRound);
  });

  test('carried-forward comments preserve original review_round', async ({ request }) => {
    const filePath = await getTestFilePath(request);

    let session = await request.get('/api/session').then(r => r.json());
    const round1 = session.review_round;

    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Round 1 comment' },
    });

    await request.post('/api/finish');
    await request.post('/api/round-complete');
    await waitForRound(request, round1);

    const comments = await request.get(`/api/file/comments?path=${encodeURIComponent(filePath)}`).then(r => r.json());
    expect(comments.length).toBe(1);
    expect(comments[0].review_round).toBe(round1);
    expect(comments[0].carried_forward).toBe(true);
  });
});

// ============================================================
// Multi-Round — Frontend Behavior
// ============================================================
test.describe('Multi-Round — Frontend', () => {
  test.beforeEach(async ({ page, request }) => {
    // Reset server to reviewing state in case a previous test left it in
    // waiting/finished state. Without this, clearAllComments writes .crit.json,
    // the file watcher detects the change, and emits edit-detected SSE events
    // that can overwrite UI text in the next test.
    await request.post('/api/round-complete');
    await clearAllComments(request);
    await loadPage(page);
  });

  test('finish review shows waiting overlay with prompt', async ({ page, request }) => {
    // Add a comment so the prompt is non-empty
    const filePath = await getTestFilePath(request);
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Round test comment' },
    });

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Click finish
    await page.locator('#finishBtn').click();

    const overlay = page.locator('#waitingOverlay');
    await expect(overlay).toHaveClass(/active/);

    // Prompt should contain crit
    const prompt = page.locator('#waitingPrompt');
    await expect(prompt).toContainText('crit');
  });

  test('finish review with no comments shows "no feedback" message', async ({ page }) => {
    await page.locator('#finishBtn').click();

    const overlay = page.locator('#waitingOverlay');
    await expect(overlay).toHaveClass(/active/);

    const message = page.locator('#waitingMessage');
    await expect(message).toContainText('close this browser tab', { timeout: 10_000 });
  });

  test('round-complete SSE triggers UI refresh and exits waiting state', async ({ page, request }) => {
    // Add a comment and finish
    const filePath = await getTestFilePath(request);
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'SSE test' },
    });

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Click finish to enter waiting state
    await page.locator('#finishBtn').click();
    const overlay = page.locator('#waitingOverlay');
    await expect(overlay).toHaveClass(/active/);

    // Trigger round-complete via API (simulates agent calling crit)
    await request.post('/api/round-complete');

    // UI should exit waiting state (overlay removed, file sections re-rendered)
    await expect(overlay).not.toHaveClass(/active/, { timeout: 5_000 });

    // Finish button should be available again (comment persists so "Finish Review")
    const finishBtn = page.locator('#finishBtn');
    await expect(finishBtn).toHaveText('Finish Review');
    await expect(finishBtn).toBeEnabled();
  });

  test('unresolved comments persist in UI after round-complete', async ({ page, request }) => {
    // Switch plan.md to document view for commenting
    const mdSection = page.locator('.file-section').filter({ hasText: 'plan.md' });
    const docBtn = mdSection.locator('.file-header-toggle .toggle-btn[data-mode="document"]');
    await docBtn.click();
    await expect(mdSection.locator('.document-wrapper')).toBeVisible();

    // Add a comment via UI
    const lineBlock = mdSection.locator('.line-block').first();
    await lineBlock.hover();
    await mdSection.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('Unresolved survives round');
    await page.locator('.comment-form .btn-primary').click();
    await expect(mdSection.locator('.comment-card')).toBeVisible();

    // Verify comment count icon is visible
    const countEl = page.locator('#commentCount');
    await expect(countEl).toBeVisible();

    // Finish and trigger round-complete
    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
    await request.post('/api/round-complete');

    // Wait for UI to refresh
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/, { timeout: 5_000 });

    // Unresolved comment should still be visible (carried forward)
    await expect(page.locator('.comment-card')).toHaveCount(1);
    await expect(countEl).toBeVisible();
  });

  test('resolved comments render with green checkmark after round-complete', async ({ page, request }) => {
    // Add a comment via API
    const filePath = await getTestFilePath(request);

    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Will be resolved visually' },
    });

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Click Finish to write .crit.json and enter waiting state
    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);

    // Finish already wrote .crit.json; read the path from the finish response
    const finishRes = await request.post('/api/finish');
    const finishData = await finishRes.json();
    const critJsonPath = finishData.review_file;

    const critJson = JSON.parse(fs.readFileSync(critJsonPath, 'utf-8'));
    for (const fileKey of Object.keys(critJson.files)) {
      for (const comment of critJson.files[fileKey].comments) {
        comment.resolved = true;
        comment.resolution_note = 'Done';
      }
    }
    fs.writeFileSync(critJsonPath, JSON.stringify(critJson, null, 2));

    // Trigger round-complete
    await request.post('/api/round-complete');
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/, { timeout: 5_000 });

    // Resolved comment should render as .comment-card.resolved-card
    await expect(page.locator('.comment-card.resolved-card')).toHaveCount(1);

    // Should have Unresolve button indicating resolved state
    await expect(page.locator('.comment-actions button[title="Unresolve"]')).toBeVisible();
    // Expand to see body
    await page.locator('.comment-collapse-btn').click();
    await expect(page.locator('.comment-body')).toContainText('Will be resolved visually');
  });

  test('resolved comments are excluded from comment count', async ({ page, request }) => {
    // Add two comments
    const filePath = await getTestFilePath(request);

    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Will be resolved' },
    });
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 2, end_line: 2, body: 'Stays open' },
    });

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Click Finish to write .crit.json and enter waiting state
    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);

    const finishRes = await request.post('/api/finish');
    const finishData = await finishRes.json();
    const critJsonPath = finishData.review_file;

    // Mark only the first comment as resolved
    const critJson = JSON.parse(fs.readFileSync(critJsonPath, 'utf-8'));
    for (const fileKey of Object.keys(critJson.files)) {
      critJson.files[fileKey].comments[0].resolved = true;
    }
    fs.writeFileSync(critJsonPath, JSON.stringify(critJson, null, 2));

    // Trigger round-complete
    await request.post('/api/round-complete');
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/, { timeout: 5_000 });

    // Only unresolved comment counts — icon visible, resolved-only would be dimmed.
    // Use toPass() to retry: SSE comments-changed may transiently update state.
    const countEl = page.locator('#commentCount');
    await expect(async () => {
      await expect(countEl).toBeVisible();
      await expect(countEl).not.toHaveClass(/comment-count-resolved/);
    }).toPass({ timeout: 5000 });

    // Both should render: 1 resolved + 1 unresolved (both are .comment-card)
    await expect(page.locator('.comment-card.resolved-card')).toHaveCount(1);
    await expect(page.locator('.comment-card:not(.resolved-card)')).toHaveCount(1);
  });

  test('resolved comment is collapsed by default and expandable', async ({ page, request }) => {
    // Add and resolve a comment
    const filePath = await getTestFilePath(request);

    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Expandable comment' },
    });

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Click Finish to write .crit.json and enter waiting state
    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);

    const finishRes = await request.post('/api/finish');
    const finishData = await finishRes.json();
    const critJsonPath = finishData.review_file;

    const critJson = JSON.parse(fs.readFileSync(critJsonPath, 'utf-8'));
    for (const fileKey of Object.keys(critJson.files)) {
      for (const comment of critJson.files[fileKey].comments) {
        comment.resolved = true;
        comment.resolution_note = 'Expanded note';
      }
    }
    fs.writeFileSync(critJsonPath, JSON.stringify(critJson, null, 2));

    await request.post('/api/round-complete');
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/, { timeout: 5_000 });

    const resolved = page.locator('.comment-card.resolved-card');
    await expect(resolved).toBeVisible();

    // Should have collapsed class initially
    await expect(resolved).toHaveClass(/collapsed/);

    // Click chevron to expand
    await resolved.locator('.comment-collapse-btn').click();
    await expect(resolved).not.toHaveClass(/collapsed/);

    // Click again to collapse
    await resolved.locator('.comment-collapse-btn').click();
    await expect(resolved).toHaveClass(/collapsed/);
  });

  test('viewed state persists across round-complete', async ({ page, request }) => {
    // Mark the first non-.crit.json file as viewed
    const section = page.locator('.file-section').filter({ hasNotText: '.crit.json' }).first();
    const viewedCheckbox = section.locator('.file-header-viewed input');
    await viewedCheckbox.check();
    await expect(viewedCheckbox).toBeChecked();

    // Trigger round-complete
    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
    await request.post('/api/round-complete');
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/, { timeout: 5_000 });

    // Viewed checkbox should still be checked after the round transition
    await expect(section.locator('.file-header-viewed input')).toBeChecked();
  });

  test('file sections are re-rendered after round-complete', async ({ page, request }) => {
    // Count non-.crit.json file sections before (its presence depends on disk state)
    const sections = page.locator('.file-section').filter({ hasNotText: '.crit.json' });
    const sectionsBefore = await sections.count();

    // Trigger round-complete
    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
    await request.post('/api/round-complete');
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/, { timeout: 5_000 });

    // Same number of file sections after
    await expect(sections).toHaveCount(sectionsBefore);
  });

  test('finish button shows Approve when all comments are resolved', async ({ page, request }) => {
    const filePath = await getTestFilePath(request);

    // Add a comment
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Will resolve this' },
    });

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // With an unresolved comment, button should say "Finish Review"
    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');

    // Click Finish to write .crit.json
    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);

    // Read .crit.json path and mark comment as resolved
    const finishRes = await request.post('/api/finish');
    const finishData = await finishRes.json();
    const critJsonPath = finishData.review_file;

    const critJson = JSON.parse(fs.readFileSync(critJsonPath, 'utf-8'));
    for (const fileKey of Object.keys(critJson.files)) {
      for (const comment of critJson.files[fileKey].comments) {
        comment.resolved = true;
        comment.resolution_note = 'Done';
      }
    }
    fs.writeFileSync(critJsonPath, JSON.stringify(critJson, null, 2));

    // Trigger round-complete
    await request.post('/api/round-complete');
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/, { timeout: 5_000 });

    // All comments resolved — button should say "Approve"
    await expect(page.locator('#finishBtn')).toHaveText('Approve');
  });

  test('round badge displays on comments', async ({ page, request }) => {
    const filePath = await getTestFilePath(request);

    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Badge test comment' },
    });

    await loadPage(page);
    await switchToDocumentView(page);

    const badge = page.locator('.comment-round-badge');
    await expect(badge.first()).toBeVisible();
    await expect(badge.first()).toHaveText(/^R\d+$/);
  });

  test('round badge shows on carried-forward comments after round-complete', async ({ page, request }) => {
    const filePath = await getTestFilePath(request);

    const session = await request.get('/api/session').then(r => r.json());
    const round1 = session.review_round;

    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Carried badge test' },
    });

    await request.post('/api/finish');
    await request.post('/api/round-complete');
    await waitForRound(request, round1);

    await loadPage(page);
    await switchToDocumentView(page);

    const badge = page.locator('.comment-round-badge');
    await expect(badge.first()).toBeVisible();
    await expect(badge.first()).toHaveText('R' + round1);
  });

  test('round badge has round-latest class for most recently addressed round', async ({ page, request }) => {
    const filePath = await getTestFilePath(request);

    const session = await request.get('/api/session').then(r => r.json());
    const round1 = session.review_round;

    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Latest round test' },
    });

    await request.post('/api/finish');
    await request.post('/api/round-complete');
    await waitForRound(request, round1);

    await loadPage(page);
    await switchToDocumentView(page);

    // The carried-forward comment from round1 should have round-latest class
    // since current round is round1+1, and round1 === current_round - 1
    const badge = page.locator('.comment-round-badge.round-latest');
    await expect(badge.first()).toBeVisible();
    await expect(badge.first()).toHaveText('R' + round1);
  });

  test('round badge appears in comments panel', async ({ page, request }) => {
    const filePath = await getTestFilePath(request);

    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Panel badge test' },
    });

    await loadPage(page);

    // Open comments panel
    await page.locator('#commentCount').click();
    await expect(page.locator('#commentsPanel')).toBeVisible();

    const panelBadge = page.locator('#commentsPanel .comment-round-badge');
    await expect(panelBadge.first()).toBeVisible();
    await expect(panelBadge.first()).toHaveText(/^R\d+$/);
  });
});

// ============================================================
// No-Changes Confirmation
// ============================================================
test.describe('No-Changes Confirmation', () => {
  test.beforeEach(async ({ request }) => {
    await request.post('/api/round-complete');
    await clearAllComments(request);
  });

  test('shows confirmation when finishing with only carried-forward comments', async ({ page, request }) => {
    // Round 1: add a comment and finish
    const filePath = await getTestFilePath(request);
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Carried forward comment' },
    });
    await request.post('/api/finish');

    // Round 2: agent signals round-complete
    const round1 = (await request.get('/api/session').then(r => r.json())).review_round;
    await request.post('/api/round-complete');
    await waitForRound(request, round1);

    // Load page with carried-forward comments
    await loadPage(page);
    await expect(page.locator('#finishBtn')).toBeEnabled();

    // Click finish without taking any action
    await page.locator('#finishBtn').click();

    // Should show the no-changes confirmation, not the waiting overlay
    await expect(page.locator('#noChangesOverlay')).toHaveClass(/active/);
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/);
  });

  test('go back button returns to review', async ({ page, request }) => {
    const filePath = await getTestFilePath(request);
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Go back test' },
    });
    await request.post('/api/finish');

    const round1 = (await request.get('/api/session').then(r => r.json())).review_round;
    await request.post('/api/round-complete');
    await waitForRound(request, round1);

    await loadPage(page);
    await page.locator('#finishBtn').click();
    await expect(page.locator('#noChangesOverlay')).toHaveClass(/active/);

    // Click go back
    await page.locator('#noChangesGoBack').click();
    await expect(page.locator('#noChangesOverlay')).not.toHaveClass(/active/);
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/);

    // Should be back in reviewing state
    await expect(page.locator('#finishBtn')).toBeEnabled();
  });

  test('send anyway proceeds to waiting state', async ({ page, request }) => {
    const filePath = await getTestFilePath(request);
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Send anyway test' },
    });
    await request.post('/api/finish');

    const round1 = (await request.get('/api/session').then(r => r.json())).review_round;
    await request.post('/api/round-complete');
    await waitForRound(request, round1);

    await loadPage(page);
    await page.locator('#finishBtn').click();
    await expect(page.locator('#noChangesOverlay')).toHaveClass(/active/);

    // Click send anyway
    await page.locator('#noChangesSendAnyway').click();
    await expect(page.locator('#noChangesOverlay')).not.toHaveClass(/active/);
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
  });

  test('resolve all resolves comments and finishes as approved', async ({ page, request }) => {
    const filePath = await getTestFilePath(request);
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Resolve all test' },
    });
    await request.post('/api/finish');

    const round1 = (await request.get('/api/session').then(r => r.json())).review_round;
    await request.post('/api/round-complete');
    await waitForRound(request, round1);

    await loadPage(page);
    await page.locator('#finishBtn').click();
    await expect(page.locator('#noChangesOverlay')).toHaveClass(/active/);

    // Click resolve all
    await page.locator('#noChangesResolveAll').click();
    await expect(page.locator('#noChangesOverlay')).not.toHaveClass(/active/);
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);

    // Verify all comments are resolved
    const comments = await request.get(`/api/file/comments?path=${encodeURIComponent(filePath)}`).then(r => r.json());
    expect(comments.every((c: any) => c.resolved)).toBe(true);
  });

  test('no confirmation when user added a new comment', async ({ page, request }) => {
    const filePath = await getTestFilePath(request);
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Original comment' },
    });
    await request.post('/api/finish');

    const round1 = (await request.get('/api/session').then(r => r.json())).review_round;
    await request.post('/api/round-complete');
    await waitForRound(request, round1);

    // Add a new comment via API (simulates user adding a comment this round)
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 2, end_line: 2, body: 'New comment this round' },
    });

    await loadPage(page);

    // Click finish — should go straight to waiting (new non-carried-forward comment exists)
    await page.locator('#finishBtn').click();
    await expect(page.locator('#noChangesOverlay')).not.toHaveClass(/active/);
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
  });

  test('no confirmation when all comments are resolved', async ({ page, request }) => {
    const filePath = await getTestFilePath(request);
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'To be resolved manually' },
    });
    await request.post('/api/finish');

    const round1 = (await request.get('/api/session').then(r => r.json())).review_round;
    await request.post('/api/round-complete');
    await waitForRound(request, round1);

    await loadPage(page);

    // Resolve the carried-forward comment via API
    const comments = await request.get(`/api/file/comments?path=${encodeURIComponent(filePath)}`).then(r => r.json());
    const commentId = comments[0].id;
    await request.put(`/api/comment/${commentId}/resolve?path=${encodeURIComponent(filePath)}`, {
      data: { resolved: true },
    });
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Click finish — no unresolved comments, button says Approve, straight to waiting
    await page.locator('#finishBtn').click();
    await expect(page.locator('#noChangesOverlay')).not.toHaveClass(/active/);
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
  });

  test('shows confirmation when user resolved some but not all comments', async ({ page, request }) => {
    const filePath = await getTestFilePath(request);
    // Add two comments
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Will be resolved' },
    });
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 2, end_line: 2, body: 'Will stay unresolved' },
    });
    await request.post('/api/finish');

    const round1 = (await request.get('/api/session').then(r => r.json())).review_round;
    await request.post('/api/round-complete');
    await waitForRound(request, round1);

    // Resolve only the first comment via API
    const comments = await request.get(`/api/file/comments?path=${encodeURIComponent(filePath)}`).then(r => r.json());
    await request.put(`/api/comment/${comments[0].id}/resolve?path=${encodeURIComponent(filePath)}`, {
      data: { resolved: true },
    });

    await loadPage(page);

    // Click finish — one comment still unresolved, no new feedback → confirmation
    await page.locator('#finishBtn').click();
    await expect(page.locator('#noChangesOverlay')).toHaveClass(/active/);
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/);
  });

  test('no confirmation when there are no unresolved comments', async ({ page }) => {
    // No comments at all — clicking finish should approve directly
    await loadPage(page);
    await page.locator('#finishBtn').click();
    await expect(page.locator('#noChangesOverlay')).not.toHaveClass(/active/);
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
  });

  test('Escape key closes the confirmation', async ({ page, request }) => {
    const filePath = await getTestFilePath(request);
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Escape test' },
    });
    await request.post('/api/finish');

    const round1 = (await request.get('/api/session').then(r => r.json())).review_round;
    await request.post('/api/round-complete');
    await waitForRound(request, round1);

    await loadPage(page);
    await page.locator('#finishBtn').click();
    await expect(page.locator('#noChangesOverlay')).toHaveClass(/active/);

    await page.keyboard.press('Escape');
    await expect(page.locator('#noChangesOverlay')).not.toHaveClass(/active/);
  });
});

import { test, expect, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import { clearAllComments, loadPage } from './helpers';

// Find a non-crit.json file path from the session
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
// Multi-Round — File Mode — API Behavior
// ============================================================
test.describe('Multi-Round — File Mode — API', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('session has a positive review_round', async ({ request }) => {
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
    expect(data.prompt).toContain('crit go');
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
        comment.resolution_note = 'Fixed it';
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
    expect(comments[0].resolution_note).toBe('Fixed it');
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
});

// ============================================================
// Multi-Round — File Mode — Frontend Behavior
// ============================================================
test.describe('Multi-Round — File Mode — Frontend', () => {
  test.beforeEach(async ({ page, request }) => {
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

    // Prompt should contain crit go
    const prompt = page.locator('#waitingPrompt');
    await expect(prompt).toContainText('crit go');
  });

  test('finish review with no comments shows "no feedback" message', async ({ page }) => {
    await page.locator('#finishBtn').click();

    const overlay = page.locator('#waitingOverlay');
    await expect(overlay).toHaveClass(/active/);

    const message = page.locator('#waitingMessage');
    await expect(message).toContainText('close this browser tab');
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

    // Trigger round-complete via API (simulates agent calling crit go)
    await request.post('/api/round-complete');

    // UI should exit waiting state (overlay removed, file sections re-rendered)
    await expect(overlay).not.toHaveClass(/active/, { timeout: 5_000 });

    // Finish button should be available again
    const finishBtn = page.locator('#finishBtn');
    await expect(finishBtn).toHaveText('Finish Review');
    await expect(finishBtn).toBeEnabled();
  });

  test('unresolved comments persist in UI after round-complete', async ({ page, request }) => {
    // Get the plan.md section
    const mdSection = page.locator('.file-section').filter({ hasText: 'plan.md' });
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

    // Resolved comment should render as .resolved-comment (not .comment-card)
    await expect(page.locator('.resolved-comment')).toHaveCount(1);
    await expect(page.locator('.comment-card')).toHaveCount(0);

    // Should have green checkmark and body text
    await expect(page.locator('.resolved-check')).toContainText('\u2713');
    await expect(page.locator('.resolved-body')).toContainText('Will be resolved visually');

    // Resolution note should be present
    await expect(page.locator('.resolved-note')).toContainText('Done');
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

    // Only unresolved comment counts — icon visible, not in resolved state
    const countEl = page.locator('#commentCount');
    await expect(countEl).toBeVisible();
    await expect(countEl).not.toHaveClass(/comment-count-resolved/);

    // Both should render: 1 resolved + 1 unresolved
    await expect(page.locator('.resolved-comment')).toHaveCount(1);
    await expect(page.locator('.comment-card')).toHaveCount(1);
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

    const resolved = page.locator('.resolved-comment');
    await expect(resolved).toBeVisible();

    // Should NOT have expanded class initially
    await expect(resolved).not.toHaveClass(/expanded/);

    // Click to expand
    await resolved.click();
    await expect(resolved).toHaveClass(/expanded/);

    // Click again to collapse
    await resolved.click();
    await expect(resolved).not.toHaveClass(/expanded/);
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
    const sectionsAfter = await sections.count();
    expect(sectionsAfter).toBe(sectionsBefore);
  });
});

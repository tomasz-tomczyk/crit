import { test, expect, type Page, type APIRequestContext } from '@playwright/test';

async function loadPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
}

async function clearAllComments(request: APIRequestContext) {
  const sessionRes = await request.get('/api/session');
  const session = await sessionRes.json();
  for (const f of session.files || []) {
    const commentsRes = await request.get(`/api/file/comments?path=${encodeURIComponent(f.path)}`);
    const comments = await commentsRes.json();
    if (Array.isArray(comments)) {
      for (const c of comments) {
        await request.delete(`/api/comment/${c.id}?path=${encodeURIComponent(f.path)}`);
      }
    }
  }
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
    expect(session.review_round).toBe(1);
  });

  test('POST /api/finish returns status and review_file', async ({ request }) => {
    const res = await request.post('/api/finish');
    const data = await res.json();
    expect(data.status).toBe('finished');
    expect(data.review_file).toContain('.crit.json');
  });

  test('POST /api/finish with comments returns a prompt', async ({ request }) => {
    // Add a comment first
    const sessionRes = await request.get('/api/session');
    const session = await sessionRes.json();
    const filePath = session.files[0].path;

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

    // Allow a moment for async processing
    await new Promise(r => setTimeout(r, 500));

    // Verify round incremented
    session = await request.get('/api/session').then(r => r.json());
    expect(session.review_round).toBe(startRound + 1);
  });

  test('round-complete clears comments', async ({ request }) => {
    // Add a comment
    const sessionRes = await request.get('/api/session');
    const session = await sessionRes.json();
    const filePath = session.files[0].path;

    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Will be cleared' },
    });

    // Verify comment exists
    let comments = await request.get(`/api/file/comments?path=${encodeURIComponent(filePath)}`).then(r => r.json());
    expect(comments.length).toBe(1);

    // Signal round complete
    await request.post('/api/round-complete');
    await new Promise(r => setTimeout(r, 500));

    // Comments should be cleared
    comments = await request.get(`/api/file/comments?path=${encodeURIComponent(filePath)}`).then(r => r.json());
    expect(comments.length).toBe(0);
  });

  test('file list is preserved after round-complete', async ({ request }) => {
    const before = await request.get('/api/session').then(r => r.json());
    const filesBefore = before.files.map((f: any) => f.path).sort();

    await request.post('/api/round-complete');
    await new Promise(r => setTimeout(r, 500));

    const after = await request.get('/api/session').then(r => r.json());
    const filesAfter = after.files.map((f: any) => f.path).sort();

    expect(filesAfter).toEqual(filesBefore);
  });
});

// ============================================================
// Multi-Round — Frontend Behavior
// ============================================================
test.describe('Multi-Round — Frontend', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('finish review shows waiting overlay with prompt', async ({ page, request }) => {
    // Add a comment so the prompt is non-empty
    const sessionRes = await request.get('/api/session');
    const session = await sessionRes.json();
    const filePath = session.files[0].path;
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
    const sessionRes = await request.get('/api/session');
    const session = await sessionRes.json();
    const filePath = session.files[0].path;
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

  test('comments are cleared in UI after round-complete', async ({ page, request }) => {
    // Switch plan.md to document view for commenting
    const mdSection = page.locator('.file-section').filter({ hasText: 'plan.md' });
    const docBtn = mdSection.locator('.file-header-toggle .toggle-btn[data-mode="document"]');
    await docBtn.click();
    await expect(mdSection.locator('.document-wrapper')).toBeVisible();

    // Add a comment via UI
    const lineBlock = mdSection.locator('.line-block').first();
    await lineBlock.hover();
    await mdSection.locator('.line-comment-gutter').first().click();
    await page.locator('.comment-form textarea').fill('Will disappear after round');
    await page.locator('.comment-form .btn-primary').click();
    await expect(mdSection.locator('.comment-card')).toBeVisible();

    // Verify comment count
    const countEl = page.locator('#commentCount');
    await expect(countEl).toContainText('1');

    // Finish and trigger round-complete
    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
    await request.post('/api/round-complete');

    // Wait for UI to refresh
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/, { timeout: 5_000 });

    // Comments should be gone
    await expect(page.locator('.comment-card')).toHaveCount(0);
    await expect(countEl).toHaveText('');
  });

  test('file sections are re-rendered after round-complete', async ({ page, request }) => {
    // Count file sections before
    const sectionsBefore = await page.locator('.file-section').count();

    // Trigger round-complete
    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
    await request.post('/api/round-complete');
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/, { timeout: 5_000 });

    // Same number of file sections after
    const sectionsAfter = await page.locator('.file-section').count();
    expect(sectionsAfter).toBe(sectionsBefore);
  });
});

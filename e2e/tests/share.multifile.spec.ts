import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// ============================================================
// Share Feature — Multi-File Mode
// Verifies the share payload sends a `files` array (not a
// single `content` string) when multiple files are present,
// and that comments include the `file` field.
// ============================================================

test.describe('Share — Multi-File Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('share button is visible when share URL is configured', async ({ page }) => {
    await loadPage(page);

    const shareBtn = page.locator('#shareBtn');
    await expect(shareBtn).toBeVisible();
  });

  test('config API returns a share_url', async ({ request }) => {
    const res = await request.get('/api/config');
    const config = await res.json();
    expect(config.share_url).toBeTruthy();
  });

  test('multi-file share payload sends files array with path and content', async ({ page, request }) => {
    await loadPage(page);

    // Add comments on two different files via API
    await request.post('/api/file/comments?path=main.go', {
      data: { start_line: 1, end_line: 1, body: 'Go file comment' },
    });
    await request.post('/api/file/comments?path=plan.md', {
      data: { start_line: 1, end_line: 1, body: 'Markdown comment' },
    });

    // Reload so the UI picks up the comments
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Intercept the outbound POST to the share URL
    let capturedPayload: Record<string, unknown> | null = null;

    await page.route('**/api/reviews', async (route) => {
      const request = route.request();
      if (request.method() === 'POST') {
        capturedPayload = request.postDataJSON();
        // Respond with a fake success so the UI flow completes
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ url: 'https://example.com/review/abc', delete_token: 'tok123' }),
        });
      } else {
        await route.continue();
      }
    });

    // Click share
    const shareBtn = page.locator('#shareBtn');
    await shareBtn.click();

    // Wait for the share notice to appear (confirms the request completed)
    await expect(page.locator('#toast-share')).toBeVisible();

    // Verify the payload structure
    expect(capturedPayload).not.toBeNull();

    // Multi-file payload should have `files` array, NOT `content`/`filename`
    expect(capturedPayload).not.toHaveProperty('content');
    expect(capturedPayload).not.toHaveProperty('filename');
    expect(capturedPayload).toHaveProperty('files');
    expect(capturedPayload).toHaveProperty('review_round');

    const payload = capturedPayload!;
    const files = payload.files as Array<{ path: string; content: string }>;
    expect(Array.isArray(files)).toBe(true);
    expect(files.length).toBe(5); // plan.md, main.go, handler.ex, lib/utils.ex, lib/config.ex

    // Each file entry has path and content
    for (const file of files) {
      expect(file).toHaveProperty('path');
      expect(file).toHaveProperty('content');
      expect(typeof file.path).toBe('string');
      expect(typeof file.content).toBe('string');
      expect(file.path.length).toBeGreaterThan(0);
      expect(file.content.length).toBeGreaterThan(0);
    }

    // Verify specific files are present
    const paths = files.map(f => f.path);
    expect(paths).toContain('plan.md');
    expect(paths).toContain('main.go');
    expect(paths).toContain('handler.ex');
    expect(paths).toContain('lib/utils.ex');
    expect(paths).toContain('lib/config.ex');

    // Verify comments include the `file` field
    const comments = payload.comments as Array<{ file: string; body: string; start_line: number; end_line: number }>;
    expect(comments.length).toBe(2);

    for (const comment of comments) {
      expect(comment).toHaveProperty('file');
      expect(comment).toHaveProperty('start_line');
      expect(comment).toHaveProperty('end_line');
      expect(comment).toHaveProperty('body');
      expect(typeof comment.file).toBe('string');
    }

    const goComment = comments.find(c => c.file === 'main.go');
    expect(goComment).toBeTruthy();
    expect(goComment!.body).toBe('Go file comment');

    const mdComment = comments.find(c => c.file === 'plan.md');
    expect(mdComment).toBeTruthy();
    expect(mdComment!.body).toBe('Markdown comment');
  });
});

test.describe('Share — Single-File Backward Compatibility', () => {
  // This test verifies that when there's only one file, the payload
  // uses the legacy `content` + `filename` format instead of `files` array.
  // Note: This runs against the multi-file fixture which has 5 files,
  // so we cannot directly test single-file behavior here. Instead, we
  // verify the multi-file path is correctly chosen (files.length > 1).
  // The single-file backward compat is implicitly covered by share.spec.ts
  // and share.filemode.spec.ts running against single-file fixtures.

  test('session has multiple files confirming multi-file path is used', async ({ request }) => {
    const res = await request.get('/api/session');
    const session = await res.json();
    expect(session.files.length).toBeGreaterThan(1);
  });
});

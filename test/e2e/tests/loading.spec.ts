import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage, switchToDocumentView } from './helpers';

// One load covers the git-mode page chrome. File tree status icons and stats
// are covered by file-tree.spec.ts. /api/qr is covered by Go tests
// (internal/server/server_test.go TestHandleQR_Success, TestHandleQR_MissingURL).
test.describe('Page Loading', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('git-mode page loads with branch context, diff toggle, no PR or Share button, and no mermaid', async ({ page }) => {
    // Pluggable renderers (issue #989) load their libraries on first use: a
    // review without diagrams keeps today's page weight.
    const requested: string[] = [];
    page.on('request', req => requested.push(req.url()));
    await loadPage(page);

    // Loading disappears, file sections appear.
    await expect(page.locator('.pierre-file-header').first()).toBeVisible();
    await expect(page.locator('diffs-container [data-line]').first()).toBeVisible();

    // Branch name in the header and the document title.
    await expect(page.locator('#branchContext')).toBeVisible();
    await expect(page.locator('#branchName')).toHaveText('feat/add-auth');
    await expect(page).toHaveTitle(/Crit — feat\/add-auth/);

    await expect(page.locator('#diffModeToggle')).toBeVisible();
    // No PR exists, and sharing is hidden in git mode.
    await expect(page.locator('.pr-toggle-btn')).not.toBeVisible();
    await expect(page.locator('#shareBtn')).toBeHidden();

    const doc = await switchToDocumentView(page);
    await expect(doc.locator('.line-block').first()).toBeVisible();
    await expect(page.locator('.crit-render')).toHaveCount(0);
    expect(requested.some(u => u.endsWith('/app.js'))).toBe(true);
    expect(requested.filter(u => u.includes('mermaid.min.js'))).toEqual([]);
  });

  test('config API reports the default share URL and no hosted review yet', async ({ request }) => {
    const res = await request.get('/api/config');
    await expect(res).toBeOK();
    const config = await res.json();
    expect(config.share_url).toBe('https://crit.md');
    expect(config.hosted_url).toBe('');
    expect(config.delete_token).toBe('');
  });
});

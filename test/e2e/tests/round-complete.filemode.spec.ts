import { test, expect, type APIRequestContext } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// Find a file path from the session
async function getTestFilePath(request: APIRequestContext): Promise<string> {
  const sessionRes = await request.get('/api/session');
  const session = await sessionRes.json();
  const file = session.files.find((f: any) => f.status !== 'deleted');
  return file?.path || session.files[0].path;
}

test.describe('Multi-Round — File Mode — Frontend', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('finish review with no comments shows approval in file mode', async ({ page }) => {
    await page.locator('#finishBtn').click();

    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
    await expect(page.locator('#waitingDialog')).toHaveClass(/approved/, { timeout: 10_000 });
    await expect(page.locator('#waitingHeading')).toHaveText('Approved');
  });

  test('copy prompt button shows "Copied" feedback then reverts', async ({ page, request }) => {
    const filePath = await getTestFilePath(request);
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'Copy feedback test' },
    });

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);

    const copyBtn = page.locator('#waitingClipboard');
    const label = copyBtn.locator('.copy-label');
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);

    await copyBtn.click();
    await expect(label).toHaveText('Copied');
    await expect(copyBtn).toHaveClass(/copied/);
    await expect(label).toHaveText('Copy', { timeout: 5_000 });
    await expect(copyBtn).not.toHaveClass(/copied/);
  });

  test('round-complete SSE exits waiting state in file mode', async ({ page, request }) => {
    const filePath = await getTestFilePath(request);
    await request.post(`/api/file/comments?path=${encodeURIComponent(filePath)}`, {
      data: { start_line: 1, end_line: 1, body: 'SSE test' },
    });

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    await page.locator('#finishBtn').click();
    const overlay = page.locator('#waitingOverlay');
    await expect(overlay).toHaveClass(/active/);

    await request.post('/api/round-complete');
    await expect(overlay).not.toHaveClass(/active/, { timeout: 5_000 });

    const finishBtn = page.locator('#finishBtn');
    await expect(finishBtn).toHaveText('Finish Review');
    await expect(finishBtn).toBeEnabled();
  });
});

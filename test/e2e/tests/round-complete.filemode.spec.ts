import { test, expect, type APIRequestContext } from '@playwright/test';
import { addComment, clearAllComments, loadPage } from './helpers';

// Find a file path from the session
async function getTestFilePath(request: APIRequestContext): Promise<string> {
  const session = await (await request.get('/api/session')).json();
  const files = session.files as Array<{ path: string; status: string }>;
  const file = files.find(f => f.status !== 'deleted');
  return file?.path || files[0].path;
}

test.describe('Multi-Round — File Mode — Frontend', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('finish review with no comments shows approval in file mode', async ({ page }) => {
    await loadPage(page);
    await page.locator('#finishBtn').click();

    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
    await expect(page.locator('#waitingDialog')).toHaveClass(/approved/, { timeout: 10_000 });
    await expect(page.locator('#waitingHeading')).toHaveText('Approved');
  });

  test('copy prompt button shows "Copied" feedback then reverts', async ({ page, request }) => {
    await addComment(request, await getTestFilePath(request), 1, 'Copy feedback test');
    await loadPage(page);

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
    await addComment(request, await getTestFilePath(request), 1, 'SSE test');
    await loadPage(page);

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

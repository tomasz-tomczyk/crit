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

  test('a triple click on Finish Review finishes once', async ({ page, request }) => {
    await addComment(request, await getTestFilePath(request), 1, 'Triple click test');
    await loadPage(page);
    let finishes = 0;
    page.on('request', (r) => {
      if (new URL(r.url()).pathname === '/api/finish' && r.method() === 'POST') finishes++;
    });

    const box = (await page.locator('#finishBtn').boundingBox())!;
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2, { clickCount: 3 });

    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
    await expect(page.locator('#finishBtn')).toHaveText('Waiting...');
    expect(finishes).toBe(1);
  });

  test('a click on the backdrop just after Finish keeps the waiting dialog open', async ({ page, request }) => {
    await addComment(request, await getTestFilePath(request), 1, 'Backdrop test');
    await loadPage(page);
    const overlay = page.locator('#waitingOverlay');

    // The backdrop opens where the button was, so a late extra click lands on it.
    const box = (await page.locator('#finishBtn').boundingBox())!;
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
    await expect(overlay).toHaveClass(/active/);
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
    await expect(overlay).toHaveClass(/active/);
    await expect(page.locator('#finishBtn')).toHaveText('Waiting...');

    // A deliberate backdrop click later still goes back to editing.
    await page.waitForTimeout(1_200);
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
    await expect(overlay).not.toHaveClass(/active/);
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

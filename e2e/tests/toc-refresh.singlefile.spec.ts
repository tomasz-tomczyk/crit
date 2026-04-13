import { test, expect, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { clearAllComments, loadPage } from './helpers';

/** Get the fixture directory from the session API. */
async function getFixtureDir(request: APIRequestContext): Promise<string> {
  const res = await request.get('/api/session');
  const data = await res.json();
  return data.cwd;
}

test.describe('TOC Refresh on File Change — Single File Mode', () => {
  let fixtureDir: string;
  let originalContent: string;

  test.beforeAll(async ({ request }) => {
    fixtureDir = await getFixtureDir(request);
    originalContent = fs.readFileSync(path.join(fixtureDir, 'plan.md'), 'utf-8');
  });

  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
    // Restore original file before each test for isolation
    if (fixtureDir && originalContent) {
      fs.writeFileSync(path.join(fixtureDir, 'plan.md'), originalContent);
    }
  });

  test.afterAll(() => {
    // Restore original file content for other test suites
    if (fixtureDir && originalContent) {
      fs.writeFileSync(path.join(fixtureDir, 'plan.md'), originalContent);
    }
  });

  test('TOC updates when headings change after round-complete', async ({ page, request }) => {
    await loadPage(page);

    // Open TOC
    await page.locator('#tocToggle').click();
    const tocList = page.locator('.toc-list');
    await expect(tocList).toBeVisible();

    // Verify initial heading exists
    await expect(tocList.locator('a', { hasText: 'Authentication Plan' })).toBeVisible();

    // Modify the file: rename the top heading
    const planPath = path.join(fixtureDir, 'plan.md');
    const content = fs.readFileSync(planPath, 'utf-8');
    const modified = content.replace('# Authentication Plan', '# Authorization Plan');
    fs.writeFileSync(planPath, modified);

    // Trigger round-complete so the server re-reads the file and sends file-changed SSE
    await request.post('/api/round-complete');

    // Wait for the TOC to update with the new heading
    await expect(async () => {
      await expect(tocList.locator('a', { hasText: 'Authorization Plan' })).toBeVisible();
    }).toPass({ timeout: 5000 });

    // Old heading should be gone
    await expect(tocList.locator('a', { hasText: 'Authentication Plan' })).toHaveCount(0);
  });

  test('TOC updates when a heading is added after round-complete', async ({ page, request }) => {
    await loadPage(page);

    // Open TOC
    await page.locator('#tocToggle').click();
    const tocList = page.locator('.toc-list');
    await expect(tocList).toBeVisible();

    // Count initial TOC entries
    const initialCount = await tocList.locator('a').count();

    // Append a new heading to the file
    const planPath = path.join(fixtureDir, 'plan.md');
    const content = fs.readFileSync(planPath, 'utf-8');
    fs.writeFileSync(planPath, content + '\n\n## New Section Added\n\nSome content.\n');

    // Trigger round-complete
    await request.post('/api/round-complete');

    // Wait for the new heading to appear in the TOC and count to increase
    await expect(async () => {
      await expect(tocList.locator('a', { hasText: 'New Section Added' })).toBeVisible();
      await expect(tocList.locator('a')).toHaveCount(initialCount + 1);
    }).toPass({ timeout: 5000 });
  });
});

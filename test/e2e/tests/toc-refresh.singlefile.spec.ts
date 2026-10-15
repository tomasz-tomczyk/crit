import { test, expect } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { clearAllComments, loadPage, roundComplete, sessionCwd } from './helpers';

// plan.md's headings: Authentication Plan, Overview, Design Decisions,
// Implementation Steps, Step 1, Step 2, Open Questions, Timeline.
const HEADING_COUNT = 8;

test.describe('TOC Refresh on File Change — Single File Mode', () => {
  let planPath: string;
  let originalContent: string;

  test.beforeAll(async ({ request }) => {
    planPath = path.join(await sessionCwd(request), 'plan.md');
    originalContent = fs.readFileSync(planPath, 'utf-8');
  });

  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
    // Restore the original file and wait until the server has re-read it, so
    // the page never loads a previous test's (possibly headingless) content.
    fs.writeFileSync(planPath, originalContent);
    await roundComplete(request);
  });

  test.afterAll(async ({ request }) => {
    // Restore disk AND server state for the specs that run after this one.
    fs.writeFileSync(planPath, originalContent);
    await roundComplete(request);
  });

  test('TOC updates when headings change after round-complete', async ({ page, request }) => {
    await loadPage(page);

    await page.locator('#tocToggle').click();
    const tocList = page.locator('.toc-list');
    await expect(tocList).toBeVisible();
    await expect(tocList.locator('a', { hasText: 'Authentication Plan' })).toBeVisible();

    // Rename the top heading; round-complete re-reads the file and sends file-changed.
    fs.writeFileSync(planPath, originalContent.replace('# Authentication Plan', '# Authorization Plan'));
    await roundComplete(request);

    await expect(tocList.locator('a', { hasText: 'Authorization Plan' })).toBeVisible();
    await expect(tocList.locator('a', { hasText: 'Authentication Plan' })).toHaveCount(0);
  });

  test('TOC button and panel hide when all headings are removed', async ({ page, request }) => {
    await loadPage(page);

    // Open TOC so the panel is visible — sets up the regression scenario
    const toggle = page.locator('#tocToggle');
    await toggle.click();
    const toc = page.locator('#toc');
    await expect(toc).not.toHaveClass(/toc-hidden/);

    fs.writeFileSync(planPath, 'Just a paragraph.\n\nNo headings here at all.\n');
    await roundComplete(request);

    // Toggle button should hide and panel should re-hide itself
    await expect(toggle).toBeHidden();
    await expect(toc).toHaveClass(/toc-hidden/);
  });

  test('TOC updates when a heading is added after round-complete', async ({ page, request }) => {
    await loadPage(page);

    await page.locator('#tocToggle').click();
    const links = page.locator('.toc-list a');
    await expect(links).toHaveCount(HEADING_COUNT);

    fs.writeFileSync(planPath, originalContent + '\n\n## New Section Added\n\nSome content.\n');
    await roundComplete(request);

    await expect(links.filter({ hasText: 'New Section Added' })).toBeVisible();
    await expect(links).toHaveCount(HEADING_COUNT + 1);
  });
});

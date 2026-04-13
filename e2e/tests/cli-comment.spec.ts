import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import * as fs from 'fs';
import { clearAllComments, addComment, loadPage, mdSection, switchToDocumentView } from './helpers';

// Read fixture state written by setup-fixtures.sh
function readFixtureState(): { critBin: string; fixtureDir: string; fakeHome: string } {
  const raw = fs.readFileSync('/tmp/crit-e2e-state-3123', 'utf8');
  const env: Record<string, string> = {};
  for (const line of raw.trim().split('\n')) {
    const eq = line.indexOf('=');
    if (eq >= 0) {
      env[line.slice(0, eq)] = line.slice(eq + 1);
    }
  }
  if (!env['CRIT_BIN'] || !env['CRIT_FIXTURE_DIR'] || !env['FAKE_HOME']) {
    throw new Error('CRIT_BIN, CRIT_FIXTURE_DIR, or FAKE_HOME not set in state file');
  }
  return { critBin: env['CRIT_BIN'], fixtureDir: env['CRIT_FIXTURE_DIR'], fakeHome: env['FAKE_HOME'] };
}

test.describe('CLI comment sync — live browser update', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);
  });

  test('crit comment adds a comment that appears in the browser via SSE', async ({ page }) => {
    const { critBin, fixtureDir, fakeHome } = readFixtureState();
    const execOpts = { shell: true, timeout: 5000, cwd: fixtureDir, env: { ...process.env, HOME: fakeHome } } as const;
    const section = mdSection(page);

    // Wait for document to be stable before asserting no comments
    await expect(section.locator('.line-block').first()).toBeVisible();
    await expect(section.locator('.comment-card')).toHaveCount(0);

    // Run CLI comment in the fixture dir (uses daemon registry via HOME)
    execSync(`"${critBin}" comment plan.md:1 "Hello from CLI"`, execOpts);

    // SSE should trigger re-fetch; comment card should appear
    await expect(section.locator('.comment-body')).toContainText('Hello from CLI', { timeout: 5000 });
  });

  test('crit comment updates header badge count via SSE', async ({ page }) => {
    const { critBin, fixtureDir, fakeHome } = readFixtureState();
    const execOpts = { shell: true, timeout: 5000, cwd: fixtureDir, env: { ...process.env, HOME: fakeHome } } as const;
    const section = mdSection(page);
    const countEl = page.locator('#commentCount');
    const badgeEl = page.locator('#commentCountNumber');

    // Wait for document to be stable, badge should be visible but empty
    await expect(section.locator('.line-block').first()).toBeVisible();
    await expect(badgeEl).toHaveText('');

    // Add a comment via CLI
    execSync(`"${critBin}" comment plan.md:1 "Badge update test"`, execOpts);

    // Header badge should appear with count 1
    await expect(countEl).toBeVisible({ timeout: 5000 });
    await expect(badgeEl).toHaveText('1');
  });

  test('crit comment --clear removes all comments in the browser via SSE', async ({ page, request }) => {
    const { critBin, fixtureDir, fakeHome } = readFixtureState();
    const execOpts = { shell: true, timeout: 5000, cwd: fixtureDir, env: { ...process.env, HOME: fakeHome } } as const;
    const section = mdSection(page);

    // Add a comment via the API, then reload so the browser picks up the in-memory state.
    await addComment(request, 'plan.md', 1, 'Comment to be cleared');
    await loadPage(page);
    await switchToDocumentView(page);

    await expect(section.locator('.comment-body')).toContainText('Comment to be cleared', { timeout: 5000 });

    // Wait until review file on disk contains the comment (debounce has fired).
    // Discover the review file path from the finish API.
    const finishRes = await request.post('/api/finish');
    const finishData = await finishRes.json();
    const reviewFilePath = finishData.review_file;
    await expect(async () => {
      const content = fs.readFileSync(reviewFilePath, 'utf8');
      expect(content).toContain('Comment to be cleared');
    }).toPass({ timeout: 3000 });

    // Run --clear via CLI
    execSync(`"${critBin}" comment --clear`, execOpts);

    // SSE should trigger re-fetch; no comment cards remain
    await expect(section.locator('.comment-card')).toHaveCount(0, { timeout: 5000 });
  });
});

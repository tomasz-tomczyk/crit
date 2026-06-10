import { test, expect } from '@playwright/test';
import type { APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import {
  addComment,
  clearAllComments,
  getMdPath,
  loadPage,
  mdSection,
  switchToDocumentView,
} from './helpers';

// Poll session API until the review round increments past `previousRound`.
async function waitForRound(request: APIRequestContext, previousRound: number) {
  await expect(async () => {
    const session = await request.get('/api/session').then(r => r.json());
    expect(session.review_round).toBeGreaterThan(previousRound);
  }).toPass({ timeout: 5000 });
}

// GitHub-synced comment badge -- verifies that a comment whose review.json
// entry carries `github_id` renders the .github-badge in the UI.
// The signal mirrors what crit-web will receive in the share payload (#370).
test.describe('GitHub-synced comment badge (#370)', () => {
  test('renders .github-badge when comment.github_id is set', async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
    await switchToDocumentView(page);

    const mdPath = await getMdPath(request);

    // 1. Add a comment via the API so the daemon state is consistent.
    await addComment(request, mdPath, 1, 'Synced from GitHub');

    // 2. Flush to disk to get the review file path.
    const finishRes = await request.post('/api/finish');
    const finishData = await finishRes.json();
    const critJsonPath = finishData.review_file as string;

    // 3. Wait for the comment to be persisted, then stamp github_id.
    await expect(async () => {
      const content = fs.readFileSync(critJsonPath, 'utf8');
      expect(content).toContain('Synced from GitHub');
    }).toPass({ timeout: 3000 });

    const critJson = JSON.parse(fs.readFileSync(critJsonPath, 'utf8'));
    let stamped = false;
    for (const fileKey of Object.keys(critJson.files)) {
      for (const comment of critJson.files[fileKey].comments) {
        comment.github_id = 12345;
        stamped = true;
      }
    }
    expect(stamped).toBeTruthy();
    fs.writeFileSync(critJsonPath, JSON.stringify(critJson, null, 2));

    // 4. Use round-complete to force the daemon to reload the file
    //    deterministically (avoids mtime-poll race on Windows CI).
    const round = (await request.get('/api/session').then(r => r.json())).review_round;
    await request.post('/api/round-complete');
    await waitForRound(request, round);

    // 5. Reload and verify the badge renders.
    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    const badge = section.locator('.github-badge');
    await expect(badge).toHaveCount(1, { timeout: 5000 });
    await expect(badge).toHaveText('GitHub');
    await expect(badge).toHaveAttribute('title', 'Synced from GitHub');
  });

  test('omits .github-badge when github_id is missing', async ({ page, request }) => {
    await clearAllComments(request);

    const mdPath = await getMdPath(request);
    // Add the comment before loading the page so it's present on first render.
    await addComment(request, mdPath, 1, 'Plain local comment');

    await loadPage(page);
    await switchToDocumentView(page);

    const section = mdSection(page);
    await expect(section.locator('.comment-body')).toContainText('Plain local comment', { timeout: 5000 });
    await expect(section.locator('.github-badge')).toHaveCount(0);
  });
});

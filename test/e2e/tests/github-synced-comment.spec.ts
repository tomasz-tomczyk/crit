import { test, expect } from '@playwright/test';
import * as fs from 'fs';
import {
  addComment, clearAllComments, getMdPath, loadPage, mdSection, switchToDocumentView,
  getReviewFilePath, roundComplete,
} from './helpers';

// GitHub-synced comment badge -- verifies that a comment whose review.json
// entry carries `github_id` renders the .github-badge in the UI.
// The signal mirrors what crit-web will receive in the share payload (#370).
// (No badge without github_id: web/__tests__/crit-comment-card.test.js,
// "GitHub badge omitted when comment.github_id is missing or zero".)
test.describe('GitHub-synced comment badge (#370)', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('renders .github-badge when comment.github_id is set in the review file', async ({ page, request }) => {
    const mdPath = await getMdPath(request);

    // 1. Add a comment via the API so the daemon state is consistent.
    await addComment(request, mdPath, 1, 'Synced from GitHub');

    // 2. Flush to disk to get the review file path.
    await request.post('/api/finish');
    const critJsonPath = await getReviewFilePath(request);

    // 3. Wait for the comment to be persisted, then stamp github_id.
    await expect(async () => {
      expect(fs.readFileSync(critJsonPath, 'utf8')).toContain('Synced from GitHub');
    }).toPass();

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
    await roundComplete(request);

    // 5. Load and verify the badge renders.
    await loadPage(page);
    await switchToDocumentView(page);

    const section = await mdSection(page);
    const badge = section.locator('.github-badge');
    await expect(badge).toHaveCount(1);
    await expect(badge).toHaveText('GitHub');
    await expect(badge).toHaveAttribute('title', 'Synced from GitHub');
  });
});

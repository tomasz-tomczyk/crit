import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { clearAllComments, diffLine, loadPage, openLineComment, revealFile, readFixtureState, roundComplete, switchScope } from './helpers';

// config.yaml is the pre-existing unstaged (untracked) file created by setup-fixtures.sh.
// It exists before the server starts, so its diff renders correctly.
const FIXTURE_UNSTAGED_FILE = 'config.yaml';

// unstaged-test.py is created at runtime AFTER the server is already running.
const RUNTIME_UNSTAGED_FILE = 'unstaged-test.py';
const RUNTIME_UNSTAGED_CONTENT = `# Unstaged test file
def hello():
    print("Hello from unstaged file")

def goodbye():
    print("Goodbye from unstaged file")

if __name__ == "__main__":
    hello()
    goodbye()
`;

async function addCommentOnLine1(page: Page, section: Awaited<ReturnType<typeof revealFile>>, body: string) {
  await openLineComment(page, section, 1);
  await page.locator('.comment-form textarea').fill(body);
  await page.locator('.comment-form .btn-primary').click();
  const card = section.locator('.comment-card');
  await expect(card).toBeVisible();
  await expect(card.locator('.comment-body')).toContainText(body);
}

// The file watcher must have picked up (or dropped) the runtime file before
// a page loads, or the page renders a stale file list.
async function waitForSessionFile(request: APIRequestContext, present: boolean) {
  await expect.poll(async () => {
    // Scoped sessions re-read git status per request; the cached "all" list
    // only refreshes on round-complete.
    const session = await (await request.get('/api/session?scope=unstaged')).json();
    return session.files.some((f: { path: string }) => f.path === RUNTIME_UNSTAGED_FILE);
  }).toBe(present);
}

// ============================================================
// Comments on pre-existing unstaged file (config.yaml from fixture)
// ============================================================
test.describe('Unstaged File Comments — pre-existing file', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('commenting on a pre-existing unstaged file updates the badges and persists across reload', async ({ page }) => {
    await loadPage(page);
    await switchScope(page, 'unstaged');
    const section = await revealFile(page, FIXTURE_UNSTAGED_FILE);

    // config.yaml is untracked, shown as all-addition diff
    await expect(diffLine(section, 1)).toHaveAttribute('data-line-type', 'change-addition');
    await expect(page.locator('#commentCountNumber')).toHaveText('');

    await addCommentOnLine1(page, section, 'Persistent unstaged comment');

    // Header count and file-tree badge both update.
    await expect(page.locator('#commentCount')).toBeVisible();
    await expect(page.locator('#commentCount')).toHaveText('1');
    const treeFile = page.locator('.tree-file').filter({ hasText: FIXTURE_UNSTAGED_FILE });
    await expect(treeFile.locator('.tree-comment-badge')).toHaveText('1');

    // Reload: scope persists via cookie and the comment is still there.
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden();
    await expect(page.locator('#scopeToggle .toggle-btn[data-scope="unstaged"]')).toHaveClass(/active/);
    const reloadedSection = await revealFile(page, FIXTURE_UNSTAGED_FILE);
    await expect(reloadedSection.locator('.comment-body')).toContainText('Persistent unstaged comment');
  });
});

// ============================================================
// Regression: an unstaged file created after server start used to show
// "No changes" — the watcher saw it, but git diff returned empty hunks.
// ============================================================
test.describe('Unstaged File Comments — runtime-created file', () => {
  let fixtureDir: string;

  test.beforeAll(async ({ request }) => {
    fixtureDir = readFixtureState().CRIT_FIXTURE_DIR;
    expect(fixtureDir).toBeTruthy();
    fs.writeFileSync(path.join(fixtureDir, RUNTIME_UNSTAGED_FILE), RUNTIME_UNSTAGED_CONTENT);
    await waitForSessionFile(request, true);
  });

  test.afterAll(async ({ request }) => {
    fs.rmSync(path.join(fixtureDir, RUNTIME_UNSTAGED_FILE), { force: true });
    // Round-complete restores a deleted file as a phantom entry while the
    // review file still holds comments on it (the API test above leaves one,
    // and its debounced save may or may not have landed yet). Clearing is
    // synchronous and drops those, so the cleanup below is deterministic.
    await clearAllComments(request);
    // Later specs count the session's files; don't leave this one behind.
    await waitForSessionFile(request, false);
    // The default ("all") list only drops a deleted file on round-complete.
    // If any round-complete ran while the file existed, it is still listed
    // there, so refresh it and check the list the page loads.
    await roundComplete(request);
    await expect.poll(async () => {
      const session = await (await request.get('/api/session')).json();
      return session.files.some((f: { path: string }) => f.path === RUNTIME_UNSTAGED_FILE);
    }).toBe(false);
  });

  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('runtime-created unstaged file is listed, renders its additions, and takes comments', async ({ page }) => {
    await loadPage(page);
    await switchScope(page, 'unstaged');
    await expect(page.locator('.tree-file-name', { hasText: RUNTIME_UNSTAGED_FILE })).toBeVisible();

    const section = await revealFile(page, RUNTIME_UNSTAGED_FILE);
    const first = diffLine(section, 1);
    await expect(first).toHaveAttribute('data-line-type', 'change-addition');
    await expect(first).toContainText('# Unstaged test file');
    await expect(diffLine(section, 10)).toContainText('goodbye()');

    await addCommentOnLine1(page, section, 'Comment on runtime unstaged file');
  });

  test('API accepts comments for a runtime-created unstaged file', async ({ request }) => {
    const resp = await request.post(`/api/file/comments?path=${encodeURIComponent(RUNTIME_UNSTAGED_FILE)}`, {
      data: { start_line: 2, end_line: 2, body: 'API comment on runtime unstaged' },
    });
    expect(resp.ok()).toBeTruthy();
  });
});

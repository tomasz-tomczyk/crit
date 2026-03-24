import { test, expect, type Page } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { clearAllComments, loadPage } from './helpers';

// Read fixture state written by setup-fixtures.sh
function readFixtureState(): { critBin: string; fixtureDir: string } {
  const raw = fs.readFileSync('/tmp/crit-e2e-state-3123', 'utf8');
  const env: Record<string, string> = {};
  for (const line of raw.trim().split('\n')) {
    const eq = line.indexOf('=');
    if (eq >= 0) {
      env[line.slice(0, eq)] = line.slice(eq + 1);
    }
  }
  if (!env['CRIT_BIN'] || !env['CRIT_FIXTURE_DIR']) {
    throw new Error('CRIT_BIN or CRIT_FIXTURE_DIR not set in state file');
  }
  return { critBin: env['CRIT_BIN'], fixtureDir: env['CRIT_FIXTURE_DIR'] };
}

async function switchScope(page: Page, scope: string) {
  const responsePromise = page.waitForResponse(resp =>
    resp.url().includes('/api/session') && resp.status() === 200
  );
  await page.click(`#scopeToggle .toggle-btn[data-scope="${scope}"]`);
  await responsePromise;
  await expect(page.locator(`#scopeToggle .toggle-btn[data-scope="${scope}"]`)).toHaveClass(/active/);
}

// config.yaml is the pre-existing unstaged (untracked) file created by setup-fixtures.sh.
// It exists before the server starts, so its diff renders correctly.
const FIXTURE_UNSTAGED_FILE = 'config.yaml';

function configSection(page: Page) {
  return page.locator('.file-section').filter({ hasText: FIXTURE_UNSTAGED_FILE });
}

// unstaged-test.py is created at runtime AFTER the server is already running.
// The file watcher detects it, but the diff may not render correctly.
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

function runtimeSection(page: Page) {
  return page.locator('.file-section').filter({ hasText: RUNTIME_UNSTAGED_FILE });
}

// ============================================================
// Comments on pre-existing unstaged file (config.yaml from fixture)
// ============================================================
test.describe('Unstaged File Comments — pre-existing file', () => {
  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test.afterEach(async ({ page }) => {
    await page.evaluate(() => {
      document.cookie = 'crit-diff-scope=all; path=/; max-age=31536000; SameSite=Strict';
    });
  });

  test('can add a comment on a pre-existing unstaged file', async ({ page }) => {
    await switchScope(page, 'unstaged');

    const section = configSection(page);
    await expect(section).toBeVisible();

    // config.yaml is untracked, shown as all-addition diff
    const additionSide = section.locator('.diff-split-side.addition').first();
    await expect(additionSide).toBeVisible();
    await additionSide.hover();

    const commentBtn = additionSide.locator('.diff-comment-btn');
    await expect(commentBtn).toBeVisible();
    await commentBtn.click();

    const form = page.locator('.comment-form');
    await expect(form).toBeVisible();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Comment on unstaged config');
    await page.locator('.comment-form .btn-primary').click();

    // Comment card should appear
    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Comment on unstaged config');
  });

  test('comment count badge updates for unstaged file comment', async ({ page }) => {
    await switchScope(page, 'unstaged');

    const section = configSection(page);
    await expect(section).toBeVisible();

    const countEl = page.locator('#commentCount');
    await expect(page.locator('#commentCountNumber')).toHaveText('');

    // Add a comment
    const additionSide = section.locator('.diff-split-side.addition').first();
    await additionSide.hover();
    await additionSide.locator('.diff-comment-btn').click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Badge test');
    await page.locator('.comment-form .btn-primary').click();

    await expect(section.locator('.comment-card')).toBeVisible();
    await expect(countEl).toBeVisible();
    await expect(countEl).toHaveText('1');
  });

  test('file tree shows comment badge for unstaged file', async ({ page }) => {
    await switchScope(page, 'unstaged');

    const section = configSection(page);
    await expect(section).toBeVisible();

    const additionSide = section.locator('.diff-split-side.addition').first();
    await additionSide.hover();
    await additionSide.locator('.diff-comment-btn').click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Tree badge test');
    await page.locator('.comment-form .btn-primary').click();

    await expect(section.locator('.comment-card')).toBeVisible();

    const treeFile = page.locator('.tree-file').filter({ hasText: FIXTURE_UNSTAGED_FILE });
    await expect(treeFile.locator('.tree-comment-badge')).toBeVisible();
    await expect(treeFile.locator('.tree-comment-badge')).toHaveText('1');
  });

  test('unstaged comment persists after page reload', async ({ page }) => {
    await switchScope(page, 'unstaged');

    const section = configSection(page);
    await expect(section).toBeVisible();

    const additionSide = section.locator('.diff-split-side.addition').first();
    await additionSide.hover();
    await additionSide.locator('.diff-comment-btn').click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Persistent unstaged comment');
    await page.locator('.comment-form .btn-primary').click();

    await expect(section.locator('.comment-card')).toBeVisible();

    // Reload
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });

    // Scope persists via cookie
    await expect(page.locator('#scopeToggle .toggle-btn[data-scope="unstaged"]')).toHaveClass(/active/);

    const reloadedSection = configSection(page);
    await expect(reloadedSection).toBeVisible();
    await expect(reloadedSection.locator('.comment-card')).toBeVisible();
    await expect(reloadedSection.locator('.comment-body')).toContainText('Persistent unstaged comment');
  });
});

// ============================================================
// BUG: Unstaged file created after server start shows "No changes"
// The file watcher detects the new file, but git diff returns empty
// hunks, rendering the section with "No changes" and no commentable lines.
// ============================================================
test.describe('Unstaged File Comments — runtime-created file (bug reproduction)', () => {
  let fixtureDir: string;

  test.beforeAll(() => {
    const state = readFixtureState();
    fixtureDir = state.fixtureDir;
    fs.writeFileSync(path.join(fixtureDir, RUNTIME_UNSTAGED_FILE), RUNTIME_UNSTAGED_CONTENT);
  });

  test.afterAll(() => {
    const filePath = path.join(fixtureDir, RUNTIME_UNSTAGED_FILE);
    if (fs.existsSync(filePath)) {
      fs.unlinkSync(filePath);
    }
  });

  test.beforeEach(async ({ page, request }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test.afterEach(async ({ page }) => {
    await page.evaluate(() => {
      document.cookie = 'crit-diff-scope=all; path=/; max-age=31536000; SameSite=Strict';
    });
  });

  test('runtime-created unstaged file appears in file tree', async ({ page }) => {
    await switchScope(page, 'unstaged');
    await expect(page.locator('.tree-file-name', { hasText: RUNTIME_UNSTAGED_FILE })).toBeVisible();
  });

  test('runtime-created unstaged file renders diff with addition lines (not "No changes")', async ({ page }) => {
    await switchScope(page, 'unstaged');

    const section = runtimeSection(page);
    await expect(section).toBeVisible();

    // BUG: The file shows "No changes" instead of an all-addition diff.
    // This assertion will FAIL if the bug is present.
    const additionSide = section.locator('.diff-split-side.addition').first();
    await expect(additionSide).toBeVisible({ timeout: 5000 });
  });

  test('can comment on runtime-created unstaged file', async ({ page }) => {
    await switchScope(page, 'unstaged');

    const section = runtimeSection(page);
    await expect(section).toBeVisible();

    // BUG: Cannot comment because no diff lines are rendered.
    const additionSide = section.locator('.diff-split-side.addition').first();
    await expect(additionSide).toBeVisible({ timeout: 5000 });
    await additionSide.hover();

    const commentBtn = additionSide.locator('.diff-comment-btn');
    await commentBtn.click();

    const textarea = page.locator('.comment-form textarea');
    await textarea.fill('Comment on runtime unstaged file');
    await page.locator('.comment-form .btn-primary').click();

    const card = section.locator('.comment-card');
    await expect(card).toBeVisible();
    await expect(card.locator('.comment-body')).toContainText('Comment on runtime unstaged file');
  });

  test('API can accept comments for runtime-created unstaged file', async ({ request }) => {
    // BUG: The API may reject comments for files it detected via watcher
    // but hasn't fully loaded into the session.
    const resp = await request.post(`/api/file/comments?path=${encodeURIComponent(RUNTIME_UNSTAGED_FILE)}`, {
      data: { start_line: 2, end_line: 2, body: 'API comment on runtime unstaged' },
    });
    expect(resp.ok()).toBeTruthy();
  });
});

import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import { execSync } from 'child_process';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { clearAllComments, loadPage, goSection, addComment } from './helpers';
import { stateFilePath } from './state-file';

// Story mode fixtures live on top of the shared git-mode fixture repo (server.go,
// routes.go, plan.md, handler.js, legacy.go, utils.go — see setup-fixtures.sh).
// A story references real hunks in that diff via (file_path, old_start) — these
// were confirmed empirically against a running daemon's /api/file/diff response
// (hunk fields are the bare Go struct field names: OldStart, not old_start; the
// story JSON schema itself uses the snake_case old_start).
//
// This spec ingests a story, drives the renderer, then MUST leave the shared
// server story-less (via DELETE /api/story) so other spec files in this project
// aren't affected — tests within a project share one server process.

function readFixtureState(): { critBin: string; fixtureDir: string; fakeHome: string } {
  const raw = fs.readFileSync(stateFilePath(process.env.CRIT_TEST_PORT || '3123'), 'utf8');
  const env: Record<string, string> = {};
  for (const line of raw.trim().split('\n')) {
    const eq = line.indexOf('=');
    if (eq >= 0) env[line.slice(0, eq)] = line.slice(eq + 1);
  }
  if (!env['CRIT_BIN'] || !env['CRIT_FIXTURE_DIR'] || !env['FAKE_HOME']) {
    throw new Error('CRIT_BIN, CRIT_FIXTURE_DIR, or FAKE_HOME not set in state file');
  }
  return { critBin: env['CRIT_BIN'], fixtureDir: env['CRIT_FIXTURE_DIR'], fakeHome: env['FAKE_HOME'] };
}

// The shared git-mode fixture repo (setup-fixtures.sh) has 11 hunks across 8
// changed files. These old_start values were confirmed empirically against a
// running daemon's /api/file/diff response (hunks use the bare Go struct
// field name OldStart there; the story JSON schema field is old_start):
//   server.go: 2, 17, 39   routes.go: 1, 48   plan.md: 0   legacy.go: 5
//   utils.go: 8   handler.js: 0   login.feature: 0   config.yaml: 0
//
// routes.go's two hunks are split across ch1/ch2 to exercise the "elsewhere"
// hint (a file with hunks owned by more than one page) — NOT server.go: its
// three hunks sit close enough together (new-line gaps of 8 and 6) that the
// client's autoExpandSmallGaps() merges them into one hunk object in-place
// (gap <= 8 lines), which collapses their distinct old_start identities and
// would misattribute line-based chapter lookups. routes.go's hunks are ~38
// lines apart, well outside that merge threshold, so they stay separate.
// The story below places 6 of 11 hunks (>50% floor) across two chapters, with
// the rest (including all of server.go) routed to support.
const STORY = {
  version: 1,
  agent: 'e2e-test',
  prologue: {
    title: 'Route registration story',
    overview: 'Reworks route registration and documents the plan.',
    key_changes: [
      'routes.go adds the health-check route and grouped imports.',
      'Dashboard logging and plan.md document the route behavior.',
    ],
    risks: [
      'Auth middleware is support plumbing, so routing review should still scan the support page.',
    ],
  },
  chapters: [
    {
      id: 'ch1',
      title: 'Route imports + health check',
      summary: 'routes.go gains a health-check route and a grouped import block.',
      hunk_refs: [{ file_path: 'routes.go', old_start: 1 }],
    },
    {
      id: 'ch2',
      title: 'Dashboard logging + docs',
      summary: "routes.go's dashboard handler logs hits; plan.md documents the approach.",
      hunk_refs: [
        { file_path: 'routes.go', old_start: 48 },
        { file_path: 'plan.md', old_start: 0 },
      ],
    },
  ],
  support: [
    {
      hunk_refs: [
        { file_path: 'handler.js', old_start: 0 },
        { file_path: 'server.go', old_start: 2 },
        { file_path: 'server.go', old_start: 17 },
        { file_path: 'server.go', old_start: 39 },
      ],
      reason: 'New file / auth middleware plumbing — mechanical additions, not part of the routing story.',
    },
  ],
};

function writeStoryFixtureFile(): string {
  const file = path.join(os.tmpdir(), `crit-e2e-story-${Date.now()}-${Math.random().toString(36).slice(2)}.json`);
  fs.writeFileSync(file, JSON.stringify(STORY));
  return file;
}

function execOptsFor(fixtureDir: string, fakeHome: string) {
  return { shell: true, timeout: 10_000, cwd: fixtureDir, env: { ...process.env, HOME: fakeHome, USERPROFILE: fakeHome } } as const;
}

function storyRail(page: Page) {
  return page.locator('#storyRail');
}

function storyPane(page: Page) {
  return page.locator('#storyPane');
}

function storyOverview(page: Page) {
  return page.locator('#crit-story-view-overview');
}

function railRow(page: Page, target: string) {
  return storyRail(page).locator(`.crit-story-row[data-story-target="${target}"]`);
}

function tocItem(page: Page, target: string) {
  return storyOverview(page).locator(`.crit-story-toc__item[data-story-target="${target}"]`);
}

function storyView(page: Page, pageId: string) {
  return page.locator(`#crit-story-view-${pageId}`);
}

async function ingestStory(critBin: string, fixtureDir: string, fakeHome: string, opts: { refresh?: boolean } = {}) {
  const storyFile = writeStoryFixtureFile();
  // --refresh forces a re-ingest (and a story-updated SSE to a running daemon)
  // even when a story is already present, where a bare `crit story` would
  // instead resume the existing one.
  const refreshFlag = opts.refresh ? ' --refresh' : '';
  try {
    execSync(`"${critBin}" story --story-file "${storyFile}"${refreshFlag}`, execOptsFor(fixtureDir, fakeHome));
  } finally {
    fs.rmSync(storyFile, { force: true });
  }
}

async function clearStory(request: APIRequestContext) {
  await request.delete('/api/story');
}

test.describe('Story mode', () => {
  let critBin: string;
  let fixtureDir: string;
  let fakeHome: string;

  test.beforeAll(() => {
    ({ critBin, fixtureDir, fakeHome } = readFixtureState());
  });

  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
    await clearStory(request);
  });

  test.afterEach(async ({ request }) => {
    // Leave the shared server story-less for every other spec in this project.
    await clearStory(request);
  });

  test('a review with no story renders the flat layout', async ({ page }) => {
    await loadPage(page);
    await expect(page.locator('body')).not.toHaveClass(/crit-story-active/);
    await expect(page.locator('#storyRoot')).toBeHidden();
    await expect(goSection(page)).toBeVisible();
  });

  test('ingesting a story activates the overview with prologue and chapter TOC', async ({ page, request }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    await page.route('**/api/config', async route => {
      const res = await route.fetch();
      const body = await res.json();
      await route.fulfill({
        response: res,
        json: {
          ...body,
          pr_number: 735,
          pr_url: 'https://github.com/tomasz-tomczyk/crit/pull/735',
          pr_title: 'feat: add story review mode',
          pr_body: 'PR description from GitHub.',
          pr_head_ref: 'story-mode',
          pr_base_ref: 'main',
        },
      });
    });
    await loadPage(page);

    await expect(page.locator('body')).toHaveClass(/crit-story-active/);
    await expect(storyOverview(page)).toBeVisible();
    const tabs = storyOverview(page).locator('.crit-story-pr__tabs');
    await expect(tabs.locator('.crit-story-pr__tab', { hasText: 'Story' })).toHaveClass(/active/);
    await expect(storyOverview(page).locator('.crit-story-prologue')).toContainText('Reworks route registration');
    await expect(storyOverview(page).locator('.crit-story-prologue')).toContainText('Key changes');
    await expect(storyOverview(page).locator('.crit-story-prologue')).toContainText('Dashboard logging and plan.md');
    await expect(storyOverview(page).locator('.crit-story-prologue')).toContainText('Risks');
    await tabs.locator('.crit-story-pr__tab', { hasText: 'PR' }).click();
    await expect(storyOverview(page).locator('.crit-story-pr__panel.active')).toContainText('feat: add story review mode');
    await expect(storyOverview(page).locator('.crit-story-pr__panel.active')).toContainText('#735');
    await expect(storyOverview(page).locator('.crit-story-pr__panel.active')).toContainText('story-mode -> main');
    await expect(storyOverview(page).locator('.crit-story-pr__panel.active')).toContainText('PR description from GitHub.');

    await expect(tocItem(page, 'ch1')).toContainText('Route imports + health check');
    await expect(tocItem(page, 'ch2')).toContainText('Dashboard logging + docs');
    await expect(tocItem(page, 'support')).toBeVisible();

    await clearStory(request);
  });

  test('navigating into a chapter shows only that chapter\'s file groups', async ({ page }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    await loadPage(page);

    await tocItem(page, 'ch1').scrollIntoViewIfNeeded();
    await tocItem(page, 'ch1').click();

    await expect(page).toHaveURL(/#story\/ch1$/);
    const ch1View = storyView(page, 'ch1');
    await expect(ch1View).toBeVisible();
    await expect(ch1View.locator('.crit-story-file-group[data-story-file="routes.go"]')).toBeVisible();
    // Only ch1's hunk group renders — plan.md and handler.js belong to other pages.
    await expect(ch1View.locator('.crit-story-file-group')).toHaveCount(1);
    await expect(ch1View.locator('.crit-story-chapter__top .crit-story-chapter__mark')).toHaveCount(0);
    await expect(ch1View.locator('.crit-story-chapter__mark-row .crit-story-chapter__mark')).toBeVisible();
    await expect.poll(async () => ch1View.evaluate((el) => {
      const fileGroup = el.querySelector('.crit-story-file-group');
      const markRow = el.querySelector('.crit-story-chapter__mark-row');
      const footer = el.querySelector('.crit-story-footer');
      if (!fileGroup || !markRow || !footer) return false;
      return !!(fileGroup.compareDocumentPosition(markRow) & Node.DOCUMENT_POSITION_FOLLOWING) &&
        !!(markRow.compareDocumentPosition(footer) & Node.DOCUMENT_POSITION_FOLLOWING);
    })).toBe(true);

    // Rail highlights the active chapter.
    await expect(railRow(page, 'ch1')).toHaveClass(/active/);
  });

  test('adding a comment on a diff line inside a chapter surfaces it in the comments panel and nav returns to the owning chapter', async ({ page, request }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);

    // ch2 owns routes.go's old_start=48 hunk (new-side lines 52-58) — comment
    // on a line in that range so the comment is scoped to ch2, not ch1.
    const comment = await addComment(request, 'routes.go', 55, 'Story E2E: dashboard logging comment');
    await loadPage(page);

    // Start on the overview (not ch2) so the click has to navigate cross-chapter.
    // Initial load doesn't rewrite the URL hash (only in-app navigation does),
    // so assert on the rendered view rather than the URL.
    await expect(storyOverview(page)).toBeVisible();

    await page.keyboard.press('Shift+C');
    const panel = page.locator('#commentsPanel');
    await expect(panel).not.toHaveClass(/comments-panel-hidden/);

    const panelCard = panel.locator('.comment-card', { hasText: 'Story E2E: dashboard logging comment' });
    await expect(panelCard).toBeVisible();
    await panelCard.scrollIntoViewIfNeeded();
    await panelCard.click();

    // Chapter-aware navigation: clicking the panel card activates ch2.
    await expect(page).toHaveURL(/#story\/ch2$/);
    const ch2Card = storyPane(page).locator(`.comment-card[data-comment-id="${comment.id}"]`);
    await expect(ch2Card).toBeVisible();
    await expect(ch2Card).toContainText('Story E2E: dashboard logging comment');
  });

  test('viewed state on a file group persists across reload and updates rail/TOC progress', async ({ page }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    await loadPage(page);

    await tocItem(page, 'ch1').scrollIntoViewIfNeeded();
    await tocItem(page, 'ch1').click();
    await expect(storyView(page, 'ch1')).toBeVisible();

    const group = storyView(page, 'ch1').locator('.crit-story-file-group[data-story-file="routes.go"]');
    const viewedLabel = group.locator('.file-header-viewed');
    await viewedLabel.scrollIntoViewIfNeeded();
    // Click the label, not .check() on the hidden-pattern checkbox — repo
    // convention for toggle-style controls (see comments-panel-switch).
    await viewedLabel.click();

    await expect(group).toHaveClass(/viewed/);
    await expect(railRow(page, 'ch1').locator('.crit-story-status')).toHaveClass(/done|partial/);

    // Reload preserves the URL, and showStory() wrote #story/ch1 to the hash
    // when we navigated there — so the reload lands directly back on ch1
    // (hash-based routing survives reload), not the overview.
    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(storyView(page, 'ch1')).toBeVisible();
    await expect(storyView(page, 'ch1').locator('.crit-story-file-group[data-story-file="routes.go"]')).toHaveClass(/viewed/);
  });

  test('chapter file headers reuse collapse and selected-text comment affordances', async ({ page }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    await loadPage(page);

    await tocItem(page, 'ch1').scrollIntoViewIfNeeded();
    await tocItem(page, 'ch1').click();
    const group = storyView(page, 'ch1').locator('.crit-story-file-group[data-story-file="routes.go"]');
    await expect(group).toBeVisible();
    await expect(group).toHaveAttribute('open', '');
    await expect(group.locator('.diff-container.split')).toBeVisible();
    await expect(group.locator('.file-header-toggle')).toHaveCount(0);

    const firstBtn = group.locator('.diff-split-side.right .diff-comment-btn').first();
    const secondBtn = group.locator('.diff-split-side.right .diff-comment-btn').nth(1);
    const firstBox = await firstBtn.boundingBox();
    const secondBox = await secondBtn.boundingBox();
    if (!firstBox || !secondBox) throw new Error('expected diff comment gutter buttons');
    await firstBtn.dispatchEvent('mousedown', {
      button: 0,
      bubbles: true,
      cancelable: true,
      clientX: firstBox.x + firstBox.width / 2,
      clientY: firstBox.y + firstBox.height / 2,
    });
    await page.mouse.move(secondBox.x + secondBox.width / 2, secondBox.y + secondBox.height / 2);
    await expect.poll(async () => group.locator('.diff-comment-gutter.drag-range').count()).toBeGreaterThan(0);
    await page.mouse.up();
    await expect(group.locator('.comment-form textarea')).toBeVisible();
    await page.keyboard.press('Escape');

    await page.locator('#diffModeToggle .toggle-btn[data-mode="unified"]').click();
    await expect(group.locator('.diff-container.unified')).toBeVisible();

    const firstContent = group.locator('.diff-content').first();
    await firstContent.evaluate((el) => {
      const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
      const text = walker.nextNode();
      if (!text) throw new Error('expected text in diff content');
      const range = document.createRange();
      range.setStart(text, 0);
      range.setEnd(text, Math.min(5, text.textContent?.length || 0));
      const selection = window.getSelection();
      selection?.removeAllRanges();
      selection?.addRange(range);
    });
    await page.keyboard.press('Shift+C');
    await expect(group.locator('.comment-form textarea')).toBeVisible();

    await page.keyboard.press('Escape');
    await group.locator('.file-header').click();
    await expect(group).not.toHaveAttribute('open', '');
  });

  test('chapter diff gap expand controls reveal more context', async ({ page }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    await loadPage(page);

    await tocItem(page, 'ch1').click();
    const group = storyView(page, 'ch1').locator('.crit-story-file-group[data-story-file="routes.go"]');
    await expect(group).toBeVisible();

    const rowsBefore = await group.locator('.diff-split-row').count();
    const trailingSpacer = group.locator('.diff-spacer-trailing').first();
    await expect(trailingSpacer).toBeVisible();
    await trailingSpacer.locator('[aria-label^="Expand "]').click();

    await expect(async () => {
      const rowsAfter = await group.locator('.diff-split-row').count();
      expect(rowsAfter).toBeGreaterThan(rowsBefore);
    }).toPass();
  });

  test('story chapter rail fills the viewport and uses the shared resize handle', async ({ page }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    await loadPage(page);

    const rail = page.locator('#storyRail');
    const resizer = page.locator('#storyRailResizer');
    await expect(rail).toBeVisible();
    await expect(resizer).toBeVisible();

    const railBox = await rail.boundingBox();
    const viewport = page.viewportSize();
    if (!railBox || !viewport) throw new Error('expected visible story rail and viewport');
    expect(Math.abs((railBox.y + railBox.height) - viewport.height)).toBeLessThanOrEqual(2);

    const widthBefore = railBox.width;
    await resizer.focus();
    await page.keyboard.press('ArrowRight');
    await expect.poll(async () => {
      const box = await rail.boundingBox();
      return box ? Math.round(box.width) : 0;
    }).toBe(Math.round(widthBefore + 16));
  });

  test('chapter navigation resets scroll to the top of the story page', async ({ page }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    await loadPage(page);

    await tocItem(page, 'ch2').scrollIntoViewIfNeeded();
    await tocItem(page, 'ch2').click();
    await expect(storyView(page, 'ch2')).toBeVisible();

    await page.evaluate(() => {
      window.scrollTo(0, 500);
      const pane = document.getElementById('storyPane');
      if (pane) pane.scrollTop = 500;
    });
    await railRow(page, 'ch1').click();
    await expect(storyView(page, 'ch1')).toBeVisible();
    await expect(storyView(page, 'ch1').locator('.crit-story-next.prev')).toContainText('Prologue');
    await expect.poll(async () => page.evaluate(() => {
      const pane = document.getElementById('storyPane');
      return {
        windowTop: window.scrollY || document.documentElement.scrollTop || 0,
        paneTop: pane ? pane.scrollTop : 0,
      };
    })).toEqual({ windowTop: 0, paneTop: 0 });

    await railRow(page, 'ch2').click();
    await expect(storyView(page, 'ch2')).toBeVisible();
    await storyView(page, 'ch2').locator('.crit-story-next.prev').click();
    await expect(storyView(page, 'ch1')).toBeVisible();

    await storyView(page, 'ch1').locator('.crit-story-next.prev').click();
    await expect(storyOverview(page)).toBeVisible();
  });

  test('support page renders with reasons', async ({ page }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    await loadPage(page);

    await tocItem(page, 'support').scrollIntoViewIfNeeded();
    await tocItem(page, 'support').click();

    await expect(page).toHaveURL(/#story\/support$/);
    const supportView = storyView(page, 'support');
    await expect(supportView).toBeVisible();
    await expect(supportView.locator('.crit-story-file-group[data-story-file="handler.js"]')).toContainText(
      'New file / auth middleware plumbing — mechanical additions, not part of the routing story.'
    );
  });

  test('Story/Diff toggle is non-destructive: diff layout + story restore without re-ingest', async ({ page }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    await loadPage(page);
    await expect(page.locator('body')).toHaveClass(/crit-story-active/);

    // Diff: flat layout, story root gone, navbar toggle remains. This
    // must NOT delete the story (that's DELETE /api/story / --clear), so no
    // network round-trip is needed — the story stays in session.story.
    await page.locator('#storyViewToggle .toggle-btn[data-story-view="diff"]').click();
    await expect(page.locator('body')).not.toHaveClass(/crit-story-active/);
    await expect(page.locator('body')).toHaveClass(/crit-story-hidden/);
    await expect(page.locator('#storyRoot')).toBeHidden();
    await expect(goSection(page)).toBeVisible();
    await expect(page.locator('.tree-file .crit-story-chip')).toHaveCount(0);
    await expect(page.locator('#storyViewToggle .toggle-btn[data-story-view="diff"]')).toHaveClass(/active/);

    // Show: story view returns with the SAME story (no re-ingest happened).
    await page.locator('#storyViewToggle .toggle-btn[data-story-view="story"]').click();
    await expect(page.locator('body')).toHaveClass(/crit-story-active/);
    await expect(page.locator('body')).not.toHaveClass(/crit-story-hidden/);
    await expect(storyOverview(page)).toBeVisible();
    await expect(page.locator('#storyViewToggle .toggle-btn[data-story-view="story"]')).toHaveClass(/active/);
  });

  test('Story/Diff toggle survives switching diff scope while in Diff view', async ({ page }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    await loadPage(page);
    await expect(page.locator('#storyViewToggle')).toBeVisible();

    await page.locator('#storyViewToggle .toggle-btn[data-story-view="diff"]').click();
    await expect(page.locator('body')).toHaveClass(/crit-story-hidden/);
    await expect(page.locator('#scopeToggle')).toBeVisible();

    await page.locator('#scopeToggle .toggle-btn[data-scope="unstaged"]').click();
    await expect(page.locator('#scopeToggle .toggle-btn[data-scope="unstaged"]')).toHaveClass(/active/);
    await expect(page.locator('#storyViewToggle')).toBeVisible();
    await expect(page.locator('#storyViewToggle .toggle-btn[data-story-view="diff"]')).toHaveClass(/active/);

    await page.locator('#storyViewToggle .toggle-btn[data-story-view="story"]').click();
    await expect(page.locator('body')).toHaveClass(/crit-story-active/);
    await expect(page.locator('#storyViewToggle .toggle-btn[data-story-view="story"]')).toHaveClass(/active/);
    await expect(storyOverview(page)).toBeVisible();

    await tocItem(page, 'ch1').click();
    const group = storyView(page, 'ch1').locator('.crit-story-file-group[data-story-file="routes.go"]');
    await expect(group).toBeVisible();
    await expect(group.locator('.diff-container')).toBeVisible();
    await expect(group).not.toContainText('File not loaded.');
  });

  test('Story/Diff toggle survives initial load from a persisted scoped diff view', async ({ page }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    await page.context().addCookies([{
      name: 'crit-settings',
      value: encodeURIComponent(JSON.stringify({ diffScope: 'unstaged' })),
      domain: 'localhost',
      path: '/',
    }]);

    let storySideFetch = 0;
    let fullSessionFetch = 0;
    await page.route('**/api/session*', async route => {
      const response = await route.fetch();
      const json = await response.json();
      const scope = new URL(route.request().url()).searchParams.get('scope');
      if (scope === 'all') fullSessionFetch++;
      if (scope === 'unstaged') delete json.story;
      await route.fulfill({ json });
    });
    await page.route('**/api/story', async route => {
      storySideFetch++;
      await route.continue();
    });

    await loadPage(page);

    expect(storySideFetch).toBeGreaterThan(0);
    expect(fullSessionFetch).toBeGreaterThan(0);
    await expect(page.locator('#storyViewToggle')).toBeVisible();
    await expect(page.locator('#storyViewToggle .toggle-btn[data-story-view="story"]')).toHaveClass(/active/);
    await expect(page.locator('body')).toHaveClass(/crit-story-active/);
    await expect(storyOverview(page)).toBeVisible();

    await tocItem(page, 'ch1').click();
    const group = storyView(page, 'ch1').locator('.crit-story-file-group[data-story-file="routes.go"]');
    await expect(group).toBeVisible();
    await expect(group.locator('.diff-container')).toBeVisible();
    await expect(group).not.toContainText('File not loaded.');
  });

  test('#story hash un-hides a hidden story', async ({ page }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    await loadPage(page);
    await page.locator('#storyViewToggle .toggle-btn[data-story-view="diff"]').click();
    await expect(page.locator('body')).toHaveClass(/crit-story-hidden/);

    // Navigating to the #story hash restores the story view (same affordance a
    // deep link into a chapter would hit).
    await page.evaluate(() => { window.location.hash = '#story'; });
    await expect(page.locator('body')).toHaveClass(/crit-story-active/);
    await expect(storyOverview(page)).toBeVisible();
  });

  test('DELETE /api/story fully removes the story: no re-entry affordance remains', async ({ page, request }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    await loadPage(page);
    await page.locator('#storyViewToggle .toggle-btn[data-story-view="diff"]').click();
    await expect(page.locator('body')).toHaveClass(/crit-story-hidden/);
    await expect(page.locator('#storyViewToggle')).toBeVisible();

    // Real removal via the API (the --clear path). The story-updated SSE with a
    // null story must tear everything down, including the re-entry affordance.
    const resp = await request.delete('/api/story');
    expect(resp.ok()).toBeTruthy();
    await expect(page.locator('body')).not.toHaveClass(/crit-story-hidden/);
    await expect(page.locator('body')).not.toHaveClass(/crit-story-active/);
    await expect(page.locator('#storyViewToggle')).toBeHidden();
    await expect(goSection(page)).toBeVisible();
  });

  test('a fresh story via SSE brings the story back live and un-hides after Hide', async ({ page }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    await loadPage(page);
    await page.locator('#storyViewToggle .toggle-btn[data-story-view="diff"]').click();
    await expect(page.locator('body')).toHaveClass(/crit-story-hidden/);

    // Re-ingest (with --refresh, since a story is present) while the page stays
    // open — the story-updated SSE must resurface the story view, clearing the
    // hidden state without a reload.
    await ingestStory(critBin, fixtureDir, fakeHome, { refresh: true });
    await expect(page.locator('body')).toHaveClass(/crit-story-active/, { timeout: 5000 });
    await expect(page.locator('body')).not.toHaveClass(/crit-story-hidden/);
    await expect(storyOverview(page)).toBeVisible();
  });

  test('comments-changed SSE re-renders the story pane live while a chapter is open', async ({ page }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    await loadPage(page);

    await tocItem(page, 'ch2').scrollIntoViewIfNeeded();
    await tocItem(page, 'ch2').click();
    await expect(storyView(page, 'ch2')).toBeVisible();

    // Add the comment via the CLI (not the review-panel UI, and not the plain
    // POST /api/file/comments path used by the addComment() helper elsewhere
    // in this suite — that path mutates in-memory state directly and never
    // notifies comments-changed; only external review-file writes such as
    // `crit comment` do, via the git-mode watcher's mergeExternalCritJSON).
    // This exercises the real live-agent-edit path the story-aware
    // comments-changed re-render (web/app.js) is meant to serve.
    execSync(`"${critBin}" comment routes.go:55 "Live SSE comment while ch2 open"`, execOptsFor(fixtureDir, fakeHome));

    await expect(storyView(page, 'ch2').locator('.comment-card', { hasText: 'Live SSE comment while ch2 open' })).toBeVisible({ timeout: 5000 });
  });

  test('clicking a comment on a line outside any story hunk is a graceful no-op', async ({ page, request }) => {
    await ingestStory(critBin, fixtureDir, fakeHome);
    // Auto-repair guarantees every indexed hunk lands somewhere (chapter or
    // synthesized support), so no *changed* file is left fully uncovered by
    // the story. deleted.txt is the one file in the fixture with zero diff
    // hunks (a pure deletion) — genuinely outside any story page's reach.
    const comment = await addComment(request, 'deleted.txt', 1, 'Comment outside any story hunk');
    await loadPage(page);

    await page.keyboard.press('Shift+C');
    const panel = page.locator('#commentsPanel');
    const panelCard = panel.locator('.comment-card', { hasText: 'Comment outside any story hunk' });
    await expect(panelCard).toBeVisible();
    await panelCard.scrollIntoViewIfNeeded();

    const urlBefore = page.url();
    await panelCard.click();

    // No navigation, no crash — story view stays on whatever it was (overview),
    // the click doesn't throw and the panel remains usable.
    await expect(page).toHaveURL(urlBefore);
    await expect(page.locator('body')).toHaveClass(/crit-story-active/);
    await expect(panel).not.toHaveClass(/comments-panel-hidden/);

    void comment;
  });
});

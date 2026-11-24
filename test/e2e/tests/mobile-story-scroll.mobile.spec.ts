import { test, expect, type Page } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

// On a phone a story chapter scrolls inside #storyPane. #storyRoot is locked
// to the viewport height with overflow: hidden, so the pane must stay a flex
// child of it: as a block it grows to its content, the root clips it, and the
// rest of the chapter can't be reached.
//
// Hunk refs come from the shared git-mode fixture (see story.spec.ts).
const STORY = {
  version: 1,
  agent: 'e2e-test',
  prologue: { title: 'Story', overview: 'Overview.', key_changes: ['Routes change.'], risks: ['None.'] },
  chapters: [
    {
      id: 'ch1',
      title: 'Routes and docs',
      summary: 'routes.go and plan.md.',
      hunk_refs: [
        { file_path: 'routes.go', old_start: 1 },
        { file_path: 'routes.go', old_start: 48 },
        { file_path: 'plan.md', old_start: 0 },
      ],
    },
  ],
  support: [{
    hunk_refs: [
      { file_path: 'handler.js', old_start: 0 },
      { file_path: 'server.go', old_start: 2 },
      { file_path: 'server.go', old_start: 17 },
      { file_path: 'server.go', old_start: 39 },
    ],
    reason: 'Plumbing.',
  }],
};

function paneMetrics(page: Page) {
  return page.evaluate(() => {
    const pane = document.getElementById('storyPane')!;
    const root = document.getElementById('storyRoot')!;
    return { pane: pane.clientHeight, content: pane.scrollHeight, root: root.clientHeight };
  });
}

test.describe('Story mode on a phone', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
    await request.delete('/api/story');
  });

  test.afterEach(async ({ request }) => {
    // Leave the shared server story-less for every other spec in this project.
    await request.delete('/api/story');
  });

  test('a chapter scrolls inside the pane to its last line', async ({ page, request }) => {
    await expect(await request.post('/api/story', { data: { story: STORY } })).toBeOK();
    await loadPage(page);

    const item = page.locator('#crit-story-view-overview .crit-story-toc__item[data-story-target="ch1"]');
    await item.scrollIntoViewIfNeeded();
    await item.click();
    await expect(page.locator('#crit-story-view-ch1')).toBeVisible();

    // The pane fits inside the root and holds more than one screen of chapter.
    await expect.poll(async () => {
      const m = await paneMetrics(page);
      return m.pane <= m.root && m.content > m.pane;
    }).toBe(true);

    // Scrolling the pane reaches the end of the chapter.
    await expect.poll(() => page.evaluate(() => {
      const pane = document.getElementById('storyPane')!;
      pane.scrollTop = pane.scrollHeight;
      return pane.scrollHeight - pane.clientHeight - pane.scrollTop;
    })).toBeLessThanOrEqual(1);
  });
});

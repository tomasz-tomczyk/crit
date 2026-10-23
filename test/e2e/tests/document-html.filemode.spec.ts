import { test, expect, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { clearAllComments, loadPage, mdSection, roundComplete, sessionCwd } from './helpers';

// Raw HTML in a reviewed document, loaded through the real app: the server
// serves plan.md and app.js renders it. document-html.spec.ts covers the
// sanitizer on its own.

const INLINE_V1 = 'Payload alpha <img src=x onerror="window.__xss=1"> <span class="share-overlay" data-comment-id="fake-inline">beta</span> gamma.';
const INLINE_V2 = INLINE_V1.replace('gamma', 'delta');
const BLOCK = '<div class="share-overlay" id="takeover" data-comment-id="fake-block" data-start-line="1">block payload</div>';
// A label forwards its clicks to the control its `for` names: here crit's own
// comments-panel button.
const LABEL = '<label for="commentCount">Label payload</label>';

let planPath: string;
let originalPlan: string;

test.beforeAll(async ({ request }) => {
  planPath = path.join(await sessionCwd(request), 'plan.md');
  originalPlan = fs.readFileSync(planPath, 'utf-8');
});

test.afterAll(async ({ request }) => {
  fs.writeFileSync(planPath, originalPlan);
  await roundComplete(request);
});

async function writePlan(request: APIRequestContext, inline: string) {
  const content = originalPlan + '\n\n## Payload\n\n' + inline + '\n\n' + BLOCK + '\n\n' + LABEL + '\n';
  fs.writeFileSync(planPath, content);
  // The file watcher must see the edit before round-complete snapshots it.
  await expect.poll(async () => {
    const file = await (await request.get('/api/file?path=plan.md')).json();
    return file.content as string;
  }, { timeout: 5000 }).toContain(inline);
}

// Neither the handler ran nor the author's names reached crit's CSS/JS.
async function expectInert(page: import('@playwright/test').Page, section: import('@playwright/test').Locator) {
  // The image stays (crit points its src at /files/), only the handler goes.
  const img = section.locator('img[src$="/x"]').first();
  await expect(img).toBeAttached();
  // Wait for the load to fail, so a surviving onerror would have run.
  await expect.poll(() => img.evaluate(el => (el as HTMLImageElement).complete)).toBe(true);
  expect(await page.evaluate(() => (window as any).__xss)).toBeUndefined();
  await expect(section.locator('[onerror]')).toHaveCount(0);
  await expect(page.locator('.share-overlay')).toHaveCount(0);
  await expect(page.locator('[data-comment-id^="fake-"]')).toHaveCount(0);
  await expect(page.locator('#takeover')).toHaveCount(0);
  await expect(section.locator('.user-content-share-overlay').first()).toBeAttached();
  await expect(section.locator('#user-content-takeover').first()).toBeAttached();
}

test.describe('Document HTML — real render path', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('a document with handler, overlay class and fake data attributes stays inert', async ({ page, request }) => {
    await writePlan(request, INLINE_V1);
    await roundComplete(request);
    await loadPage(page);
    const section = await mdSection(page);
    await expect(section.getByText('block payload')).toBeVisible();
    await expectInert(page, section);

    const label = section.getByText('Label payload');
    await expect(label).toHaveAttribute('for', 'user-content-commentCount');
    await expect(page.locator('#commentsPanel')).toHaveClass(/comments-panel-hidden/);
    await label.click();
    await expect(page.locator('#commentsPanel')).toHaveClass(/comments-panel-hidden/);
  });

  test('a #hash link finds the author\'s prefixed id and name', async ({ page, request }) => {
    const content = '[to id](#spot) and [to name](#mark)\n\n' + originalPlan +
      '\n\n<a id="spot">Spot target</a>\n\n' + 'filler\n\n'.repeat(40) + '<a name="mark">Mark target</a>\n';
    fs.writeFileSync(planPath, content);
    await expect.poll(async () => {
      const file = await (await request.get('/api/file?path=plan.md')).json();
      return file.content as string;
    }, { timeout: 5000 }).toContain('Mark target');
    await roundComplete(request);

    await loadPage(page);
    const section = await mdSection(page);
    const spot = section.locator('#user-content-spot');
    const mark = section.locator('[name="user-content-mark"]');
    await expect(spot).toBeAttached();
    await expect(spot).not.toBeInViewport();
    await section.getByRole('link', { name: 'to id' }).click();
    await expect(spot).toBeInViewport();

    await page.evaluate(() => { document.getElementById('filesContainer')!.scrollTop = 0; });
    await section.getByRole('link', { name: 'to name' }).click();
    await expect(mark).toBeInViewport();
  });

  test('the word-diff path between rounds stays inert too', async ({ page, request }) => {
    await writePlan(request, INLINE_V1);
    await roundComplete(request);
    await writePlan(request, INLINE_V2);
    await roundComplete(request);
    await expect(async () => {
      const diff = await (await request.get('/api/file/diff?path=plan.md')).json();
      expect(diff.hunks?.length).toBeGreaterThan(0);
    }).toPass({ timeout: 5000 });

    await loadPage(page);
    await page.locator('#diffToggle').click();
    const section = await mdSection(page);
    await expect(section.locator('.diff-view')).toBeVisible();
    // The changed paragraph went through the word-diff pass (gamma -> delta
    // marks the changed characters).
    await expect(section.locator('.line-content:has(.user-content-share-overlay) .diff-word-add')).toHaveText('delt');
    await expectInert(page, section);
  });
});

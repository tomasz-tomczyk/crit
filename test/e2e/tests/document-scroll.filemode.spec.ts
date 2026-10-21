import { test, expect, type APIRequestContext, type Locator } from '@playwright/test';
import { addComment, clearAllComments, getMdPath, loadPage, mdSection, reviewScroller, waitForScrollStable } from './helpers';

// The "Open Questions" blockquote, near the end of plan.md.
const OPEN_QUESTION_LINE = 47;

// Pierre and crit each round the scroll position, so on some layouts (CI's
// Linux fonts, a palette change) the thread ends up 1px off. The bug this
// guards against moves it by the reply's height (62px).
const MAX_MOVE_PX = 2;

test.describe('Document view keeps the reader in place — File Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('a reply to a thread on screen leaves the thread where it was', async ({ page, request }) => {
    // GIVEN - a thread near the end of the plan, read with the start of the document above the screen
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, OPEN_QUESTION_LINE, 'Rate-limit by key or by IP?');
    await loadPage(page);
    const section = await mdSection(page);
    const card = section.locator(`.comment-card[data-comment-id="${comment.id}"]`);
    await card.evaluate((el) => el.scrollIntoView({ block: 'center' }));
    await waitForScrollStable(page);
    await expectDocumentStartsAboveScreen(section, reviewScroller(page));
    await startTrackingScreenTop(card);

    // WHEN - the agent replies to the thread
    await addReply(request, mdPath, comment.id, 'By key, per tenant.');
    await expect(card.locator('.comment-reply')).toHaveCount(1);
    await waitForScrollStable(page);

    // THEN - the thread stayed where it was in every frame, not only at the end
    expect(await largestMoveSinceTrackingStarted(card)).toBeLessThan(MAX_MOVE_PX);
  });

  test('a reply still leaves the thread in place after the review view is rebuilt', async ({ page, request }) => {
    // GIVEN - the review view was rebuilt (a palette change reloads it), then the reader scrolls to a thread
    const mdPath = await getMdPath(request);
    const comment = await addComment(request, mdPath, OPEN_QUESTION_LINE, 'Rate-limit by key or by IP?');
    await loadPage(page);
    const firstSection = await (await mdSection(page)).elementHandle();
    await page.getByRole('button', { name: 'Settings', exact: true }).click();
    const light = page.locator('#lightPaletteSelect');
    await light.selectOption('catppuccin-latte');
    await expect(light).toBeEnabled();
    await page.keyboard.press('Escape');
    await expect.poll(() => firstSection!.evaluate((el) => el.isConnected)).toBe(false);
    const section = await mdSection(page);
    const card = section.locator(`.comment-card[data-comment-id="${comment.id}"]`);
    await card.evaluate((el) => el.scrollIntoView({ block: 'center' }));
    await waitForScrollStable(page);
    await expectDocumentStartsAboveScreen(section, reviewScroller(page));
    await startTrackingScreenTop(card);

    // WHEN - the agent replies to the thread
    await addReply(request, mdPath, comment.id, 'By key, per tenant.');
    await expect(card.locator('.comment-reply')).toHaveCount(1);
    await waitForScrollStable(page);

    // THEN - the thread stayed where it was in every frame
    expect(await largestMoveSinceTrackingStarted(card)).toBeLessThan(MAX_MOVE_PX);
  });

  // The file header sits above the document in the same Pierre item. Once the
  // header's top leaves the screen, Pierre stops keeping the item in place,
  // even while the document itself still starts on screen.
  for (const [name, place] of [
    ['the top of the file header just above the screen', placeItemTopAboveEdge],
    ['the document starting just below the top of the screen', placeDocumentTopBelowEdge],
  ] as const) {
    test(`a reply leaves the thread in place with ${name}`, async ({ page, request }) => {
      // GIVEN - the top edge of the review cuts through the file header
      const mdPath = await getMdPath(request);
      const comment = await addComment(request, mdPath, OPEN_QUESTION_LINE, 'Rate-limit by key or by IP?');
      await loadPage(page);
      const section = await mdSection(page);
      const card = section.locator(`.comment-card[data-comment-id="${comment.id}"]`);
      await card.waitFor();
      const scroller = reviewScroller(page);
      await place(scroller);
      await waitForScrollStable(page);
      await expectEdgeCutsFileHeader(scroller);
      await startTrackingScreenTop(card);

      // WHEN - the agent replies to the thread
      await addReply(request, mdPath, comment.id, 'By key, per tenant.');
      await expect(card.locator('.comment-reply')).toHaveCount(1);
      await waitForScrollStable(page);

      // THEN - the thread stayed where it was in every frame
      expect(await largestMoveSinceTrackingStarted(card)).toBeLessThan(MAX_MOVE_PX);
    });
  }
});

async function placeItemTopAboveEdge(scroller: Locator) {
  await scroller.evaluate((sc) => {
    const item = sc.querySelector('.pierre-document')!.closest('diffs-container')!;
    sc.scrollTop += item.getBoundingClientRect().top - sc.getBoundingClientRect().top + 5;
  });
}

async function placeDocumentTopBelowEdge(scroller: Locator) {
  await scroller.evaluate((sc) => {
    const section = sc.querySelector('.pierre-document')!;
    sc.scrollTop += section.getBoundingClientRect().top - sc.getBoundingClientRect().top - 4;
  });
}

async function expectEdgeCutsFileHeader(scroller: Locator) {
  const { itemTop, sectionTop, edge } = await scroller.evaluate((sc) => {
    const section = sc.querySelector('.pierre-document')!;
    return {
      itemTop: section.closest('diffs-container')!.getBoundingClientRect().top,
      sectionTop: section.getBoundingClientRect().top,
      edge: sc.getBoundingClientRect().top,
    };
  });
  expect(itemTop, 'file header must start above the visible part of the review').toBeLessThan(edge);
  expect(sectionTop, 'document must start on screen').toBeGreaterThanOrEqual(edge);
}

async function addReply(request: APIRequestContext, path: string, commentId: string, body: string) {
  const resp = await request.post(`/api/comment/${commentId}/replies?path=${encodeURIComponent(path)}`, {
    data: { body, author: 'agent' },
  });
  expect(resp.ok()).toBeTruthy();
}

async function screenTop(target: Locator): Promise<number> {
  return target.evaluate((el) => el.getBoundingClientRect().top);
}

// The reader sees every painted frame, so a jump that is put back a frame
// later still counts. The card is looked up again each frame: a re-render may
// replace its element.
async function startTrackingScreenTop(card: Locator) {
  await card.evaluate((el) => {
    const id = (el as HTMLElement).dataset.commentId;
    if (document.querySelector(`.comment-card[data-comment-id="${id}"]`) !== el) throw new Error('card is not reachable from document');
    const startTop = el.getBoundingClientRect().top;
    const tracker = { largestMove: 0, running: true };
    (window as unknown as { __screenTopTracker: typeof tracker }).__screenTopTracker = tracker;
    const tick = () => {
      const current = document.querySelector(`.comment-card[data-comment-id="${id}"]`);
      if (current) tracker.largestMove = Math.max(tracker.largestMove, Math.abs(current.getBoundingClientRect().top - startTop));
      if (tracker.running) requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });
}

async function largestMoveSinceTrackingStarted(card: Locator): Promise<number> {
  return card.evaluate(() => {
    const tracker = (window as unknown as { __screenTopTracker: { largestMove: number; running: boolean } }).__screenTopTracker;
    tracker.running = false;
    return tracker.largestMove;
  });
}

// Without it the scenario would not exercise the shift: a document that
// starts on screen has nothing above the viewport to anchor on.
async function expectDocumentStartsAboveScreen(section: Locator, scroller: Locator) {
  const sectionTop = await screenTop(section);
  const scrollerTop = await screenTop(scroller);
  expect(sectionTop, 'document must start above the visible part of the review').toBeLessThan(scrollerTop);
}

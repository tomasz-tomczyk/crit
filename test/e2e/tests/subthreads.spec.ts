import { test, expect, type APIRequestContext, type Locator, type Page } from '@playwright/test';
import {
  clearAllComments, loadPage, mdSection, goSection, getMdPath, addComment, addReply, addSubThread, resolveComment,
  selectInMessageAndPressC, messageQuoteHighlights, expectPaintedMessageQuote, roundComplete, switchToDocumentView,
  nextFrames, waitForScrollStable, answerResolvesLate,
} from './helpers';

// ============================================================
// Sub-threads — comments on a passage of a comment or a reply (#1057)
// ============================================================
//
// Selecting a passage inside one message body and pressing c opens a
// composer under that message; the sub-thread then nests there, its quote on
// top and the passage highlighted in the message. Sub-threads are full
// threads: replies, resolve, delete, and their own sub-threads at any depth.

type ApiComment = { id: string; parent_id?: string; quote?: string; quote_offset?: number; body: string; resolved?: boolean };

async function fileComments(request: APIRequestContext, path: string): Promise<ApiComment[]> {
  const resp = await request.get(`/api/file/comments?path=${encodeURIComponent(path)}`);
  await expect(resp).toBeOK();
  return resp.json();
}

// Inline cards and replies in the review list (not their comments-panel copies).
function card(page: Page, id: string): Locator {
  return page.locator(`#filesContainer .comment-card[data-comment-id="${id}"]`);
}

function reply(page: Page, id: string): Locator {
  return page.locator(`#filesContainer .comment-reply[data-reply-id="${id}"]`);
}

// The sub-thread cards directly under a card's body or a reply.
function nestedCards(message: Locator): Locator {
  return message.locator(':scope > .subthreads > .comment-block > .comment-card');
}

function composerUnder(message: Locator): Locator {
  return message.locator(':scope > .subthreads > .subthread-composer');
}

test.describe('Sub-threads', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('c on a passage of a reply opens a composer under it; the sub-thread nests there, quote shown and highlighted', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const root = await addComment(request, mdPath, 1, 'Which option?');
    const answer = await addReply(request, mdPath, root.id, 'Option A is faster. Option B is safer.');
    await loadPage(page);
    await mdSection(page);

    const replyEl = reply(page, answer.id);
    await selectInMessageAndPressC(page, replyEl.locator(':scope > .reply-body'), 'Option B is safer');

    const composer = composerUnder(replyEl);
    await expect(composer).toBeVisible();
    await expect(composer.locator('.subthread-quote')).toHaveText('Option B is safer');
    const textarea = composer.locator('textarea');
    await expect(textarea).toBeFocused();
    // The passage is highlighted while the comment is written.
    await expect.poll(() => messageQuoteHighlights(page)).toEqual(['Option B is safer']);

    await textarea.fill('Why is B safer?');
    await composer.locator('.comment-form-actions .btn-primary').click();
    await expect(composer).toHaveCount(0);

    await expect(replyEl.locator(':scope > .subthreads')).toHaveCount(1);
    const sub = nestedCards(replyEl);
    await expect(sub).toHaveCount(1);
    await expect(sub.locator(':scope > .subthread-quote')).toHaveText('Option B is safer');
    await expect(sub.locator(':scope > .comment-body')).toHaveText('Why is B safer?');
    await expect.poll(() => messageQuoteHighlights(page)).toEqual(['Option B is safer']);
    await expectPaintedMessageQuote(page);

    // Stored as a sub-thread of the reply, with the passage's offset in the
    // reply's rendered text.
    const stored = (await fileComments(request, mdPath)).find(c => c.parent_id === answer.id);
    expect(stored).toMatchObject({ body: 'Why is B safer?', quote: 'Option B is safer', quote_offset: 20 });

    // The comments panel mirrors it read-only under the reply, counts it as
    // an open comment and highlights the passage in its copy too.
    await page.keyboard.press('Shift+C');
    const panelSub = page.locator(`#commentsPanelBody .comment-reply[data-reply-id="${answer.id}"] > .subthreads .comment-card`);
    await expect(panelSub).toContainText('Why is B safer?');
    await expect(panelSub.locator('.reply-form')).toHaveCount(0);
    await expect(page.locator('#commentsFilterPill .toggle-btn[data-filter="open"] .filter-count')).toHaveText('2');
    await expect.poll(() => messageQuoteHighlights(page)).toEqual(['Option B is safer', 'Option B is safer']);
  });

  test('a passage of the root comment nests before the replies; Escape closes an empty composer; the sub-thread takes replies', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const root = await addComment(request, mdPath, 1, 'Rename the helper and add a test.');
    await addReply(request, mdPath, root.id, 'Done.');
    await loadPage(page);
    await mdSection(page);

    const rootCard = card(page, root.id);
    const rootBody = rootCard.locator(':scope > .comment-body');
    const composer = composerUnder(rootCard);

    await selectInMessageAndPressC(page, rootBody, 'Rename the helper');
    await expect(composer.locator('textarea')).toBeFocused();
    await composer.locator('textarea').press('Escape');
    await expect(composer).toHaveCount(0);
    await expect.poll(() => messageQuoteHighlights(page)).toEqual([]);

    await selectInMessageAndPressC(page, rootBody, 'add a test');
    await expect(composer.locator('.subthread-quote')).toHaveText('add a test');
    await composer.locator('textarea').fill('Which test?');
    await composer.locator('textarea').press('Control+Enter');

    const sub = nestedCards(rootCard);
    await expect(sub).toContainText('Which test?');
    // Under the root's body, before its replies.
    expect(await rootCard.evaluate(el => {
      const kids = Array.from(el.children).map(child => child.className);
      return kids.indexOf('subthreads') === kids.indexOf('comment-body') + 1 &&
        kids.indexOf('subthreads') < kids.indexOf('comment-replies');
    })).toBe(true);

    // A reply in the sub-thread stays in the sub-thread.
    await sub.locator(':scope > .reply-form .reply-input').click();
    const replyBox = sub.locator(':scope > .reply-form .reply-textarea');
    await expect(replyBox).toBeFocused();
    await replyBox.fill('The helper unit test.');
    await sub.locator(':scope > .reply-form .reply-form-buttons .btn-primary').click();
    await expect(sub.locator(':scope > .comment-replies > .comment-reply')).toHaveCount(1);
    await expect(sub.locator(':scope > .comment-replies .reply-body')).toHaveText('The helper unit test.');
    await expect(rootCard.locator(':scope > .comment-replies > .comment-reply')).toHaveCount(1);
  });

  test('a message of a sub-thread takes its own sub-thread, at any depth, with the same indent at every level', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const root = await addComment(request, mdPath, 1, 'Root comment body.');
    const answer = await addReply(request, mdPath, root.id, 'First level text here.');
    const first = await addSubThread(request, mdPath, answer.id, 'Second level text here.', 'First level', 0);
    const second = await addSubThread(request, mdPath, first.id, 'Third level text here.', 'Second level', 0);
    await loadPage(page);
    await mdSection(page);

    await selectInMessageAndPressC(page, card(page, second.id).locator(':scope > .comment-body'), 'Third level');
    const composer = composerUnder(card(page, second.id));
    await expect(composer.locator('textarea')).toBeFocused();
    await composer.locator('textarea').fill('Fourth level text here.');
    await composer.locator('textarea').press('Control+Enter');
    const third = nestedCards(card(page, second.id));
    await expect(third).toContainText('Fourth level text here.');

    await expect(page.locator('#filesContainer .subthreads')).toHaveCount(3);
    await expect(page.locator('#filesContainer .subthreads .subthreads .subthreads')).toHaveCount(1);

    const box = async (loc: Locator) => (await loc.boundingBox())!;
    const b0 = await box(card(page, root.id));
    const b1 = await box(card(page, first.id));
    const b2 = await box(card(page, second.id));
    const b3 = await box(third);
    expect(b1.x - b0.x, 'first level is indented').toBeGreaterThan(10);
    // The second and third levels both nest under a comment body: same
    // indent, same right margin, no special case by depth.
    expect(b2.x - b1.x, 'second level is indented').toBeGreaterThan(10);
    expect(Math.abs((b3.x - b2.x) - (b2.x - b1.x)), 'third level takes the same indent').toBeLessThanOrEqual(1);
    const rightGap = (outer: { x: number; width: number }, inner: { x: number; width: number }) =>
      (outer.x + outer.width) - (inner.x + inner.width);
    expect(rightGap(b1, b2), 'second level keeps a right margin').toBeGreaterThan(0);
    expect(Math.abs(rightGap(b2, b3) - rightGap(b1, b2)), 'third level keeps the same right margin').toBeLessThanOrEqual(1);
    await expect(page.locator('#filesContainer .subthreads .subthreads .subthreads'), 'third level keeps its thread line')
      .toHaveCSS('border-left-style', 'solid');

    expect((await messageQuoteHighlights(page)).sort()).toEqual(['First level', 'Second level', 'Third level']);
  });

  test('a sub-thread resolves alone; while open it blocks Approve and keeps its resolved parent shown, hide-resolved included', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const root = await addComment(request, mdPath, 1, 'Root to resolve.');
    const open = await addSubThread(request, mdPath, root.id, 'Still open here.', 'Root', 0);
    await resolveComment(request, mdPath, root.id);
    await loadPage(page);
    await mdSection(page);

    const rootCard = card(page, root.id);
    const subCard = card(page, open.id);
    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');
    await expect(rootCard.locator('xpath=..')).toHaveClass(/has-open-subthreads/);
    await expect(rootCard).not.toHaveClass(/collapsed/);
    await expect(subCard).toBeVisible();

    await page.keyboard.press('h');
    await expect(rootCard).toBeVisible();
    await expect(subCard).toBeVisible();

    // Resolving the sub-thread: nothing open is left, so Approve, and the
    // resolved parent hides with the rest.
    await subCard.hover();
    await subCard.locator(':scope > .comment-header .resolve-btn').click();
    await expect(page.locator('#finishBtn')).toHaveText('Approve');
    await expect(rootCard).toBeHidden();

    await page.keyboard.press('h');
    await expect(rootCard).toBeVisible();
    await expect(rootCard).toHaveClass(/collapsed/);

    // Unresolving the parent leaves the sub-thread resolved.
    await rootCard.locator(':scope > .comment-header .comment-collapse-btn').click();
    await rootCard.hover();
    await rootCard.locator(':scope > .comment-header .comment-actions button[title="Unresolve"]').click();
    await expect(page.locator('#finishBtn')).toHaveText('Finish Review');
    const stored = await fileComments(request, mdPath);
    expect(stored.find(c => c.id === root.id)?.resolved ?? false).toBe(false);
    expect(stored.find(c => c.id === open.id)?.resolved).toBe(true);
  });

  test('deleting a message with sub-threads asks first, with their number; without any it stays immediate', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const root = await addComment(request, mdPath, 1, 'Root with replies.');
    const answer = await addReply(request, mdPath, root.id, 'Alpha beta gamma delta.');
    const onAlpha = await addSubThread(request, mdPath, answer.id, 'On alpha.', 'Alpha', 0);
    await addSubThread(request, mdPath, answer.id, 'On gamma.', 'gamma', 11);
    await addSubThread(request, mdPath, onAlpha.id, 'Deeper.', 'On alpha', 0);
    const lone = await addReply(request, mdPath, root.id, 'A reply with nothing under it.');
    await loadPage(page);
    await mdSection(page);

    const replyEl = reply(page, answer.id);
    const deleteReply = async (el: Locator) => {
      await el.hover();
      await el.locator(':scope > .reply-header .delete-btn').click();
    };
    await expect(replyEl.locator('.subthreads .comment-card')).toHaveCount(3);

    // Dismissed: nothing is deleted.
    const messages: string[] = [];
    page.once('dialog', d => { messages.push(d.message()); void d.dismiss(); });
    await deleteReply(replyEl);
    await expect.poll(() => messages.length).toBe(1);
    expect(messages[0]).toContain('3 sub-threads');
    await expect(replyEl.locator('.subthreads .comment-card')).toHaveCount(3);

    // Accepted: the reply goes, its sub-threads with it.
    page.once('dialog', d => { messages.push(d.message()); void d.accept(); });
    await deleteReply(replyEl);
    await expect(replyEl).toHaveCount(0);
    await expect(page.locator('#filesContainer .subthreads')).toHaveCount(0);
    await expect.poll(async () => (await fileComments(request, mdPath)).filter(c => c.parent_id).length).toBe(0);
    expect(messages).toHaveLength(2);

    // No sub-thread under it: no question asked.
    await deleteReply(reply(page, lone.id));
    await expect(reply(page, lone.id)).toHaveCount(0);
    expect(messages).toHaveLength(2);
  });

  test('sub-threads and their highlighted passages survive a reload and a new round', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const root = await addComment(request, mdPath, 1, 'Keep the cache small.');
    const sub = await addSubThread(request, mdPath, root.id, 'How small?', 'the cache', 5);
    await loadPage(page);
    await mdSection(page);
    await expect(nestedCards(card(page, root.id))).toContainText('How small?');
    await expect.poll(() => messageQuoteHighlights(page)).toEqual(['the cache']);

    await loadPage(page);
    await mdSection(page);
    await expect(card(page, sub.id)).toBeVisible();
    await expect.poll(() => messageQuoteHighlights(page)).toEqual(['the cache']);

    await roundComplete(request);
    await loadPage(page);
    await mdSection(page);
    await expect(nestedCards(card(page, root.id))).toContainText('How small?');
    await expect(card(page, sub.id).locator(':scope > .subthread-quote')).toHaveText('the cache');
  });

  test('a passage of a review-level comment opens a sub-thread in the review conversation', async ({ page, request }) => {
    const created = await request.post('/api/comments', { data: { body: 'Ship it after the docs pass.' } });
    await expect(created).toBeOK();
    const root = await created.json();
    await loadPage(page);

    const rootCard = page.locator(`#reviewConversation .comment-card[data-comment-id="${root.id}"]`);
    await selectInMessageAndPressC(page, rootCard.locator(':scope > .comment-body'), 'the docs pass');
    const composer = composerUnder(rootCard);
    await expect(composer.locator('textarea')).toBeFocused();
    await composer.locator('textarea').fill('Which docs?');
    await composer.locator('textarea').press('Control+Enter');
    await expect(nestedCards(rootCard)).toContainText('Which docs?');
    await expect(page.locator('#reviewConversation .review-conversation-header .count')).toHaveText('2');

    const listed = await (await request.get('/api/comments')).json() as ApiComment[];
    expect(listed.find(c => c.parent_id === root.id)).toMatchObject({ body: 'Which docs?', quote: 'the docs pass' });

    // Deleting the comment takes its sub-thread, after saying so.
    const messages: string[] = [];
    page.once('dialog', d => { messages.push(d.message()); void d.accept(); });
    await rootCard.hover();
    await rootCard.locator(':scope > .comment-header .delete-btn').click();
    await expect(rootCard).toHaveCount(0);
    expect(messages[0]).toContain('1 sub-thread');
    await expect.poll(async () => (await (await request.get('/api/comments')).json() as ApiComment[]).length).toBe(0);
  });

  test('a sub-thread on a reply of a code diff thread', async ({ page, request }) => {
    const root = await addComment(request, 'server.go', 5, 'Why this import?');
    const answer = await addReply(request, 'server.go', root.id, 'It formats the JSON body for respondJSON.');
    await loadPage(page);
    await goSection(page);

    const replyEl = reply(page, answer.id);
    await selectInMessageAndPressC(page, replyEl.locator(':scope > .reply-body'), 'the JSON body');
    const composer = composerUnder(replyEl);
    await expect(composer.locator('textarea')).toBeFocused();
    await composer.locator('textarea').fill('Only the body?');
    await composer.locator('.comment-form-actions .btn-primary').click();

    const sub = nestedCards(replyEl);
    await expect(sub).toContainText('Only the body?');
    await expect(sub.locator(':scope > .subthread-quote')).toHaveText('the JSON body');
    await expect.poll(() => messageQuoteHighlights(page)).toEqual(['the JSON body']);
    const stored = (await fileComments(request, 'server.go')).find(c => c.parent_id === answer.id);
    expect(stored).toMatchObject({ quote: 'the JSON body', quote_offset: 11 });
  });

  test('the composer and its text survive a refresh from outside (a reply added meanwhile)', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const root = await addComment(request, mdPath, 1, 'Keep typing through refreshes.');
    await loadPage(page);
    await mdSection(page);

    const rootCard = card(page, root.id);
    await selectInMessageAndPressC(page, rootCard.locator(':scope > .comment-body'), 'through refreshes');
    const textarea = composerUnder(rootCard).locator('textarea');
    await textarea.pressSequentially('Draft in progress');

    // Another writer (the agent, crit comment) adds a reply: comments-changed
    // rebuilds the card.
    await addReply(request, mdPath, root.id, 'A reply from elsewhere.');
    await expect(rootCard.locator(':scope > .comment-replies > .comment-reply')).toHaveCount(1);
    await expect(textarea).toHaveValue('Draft in progress');
    await expect(textarea).toBeFocused();
    await expect(composerUnder(rootCard).locator('.subthread-quote')).toHaveText('through refreshes');

    await textarea.pressSequentially(', done.');
    await textarea.press('Control+Enter');
    await expect(nestedCards(rootCard)).toContainText('Draft in progress, done.');
  });

  test('a selection running from a message onto the document keeps commenting on the document', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const root = await addComment(request, mdPath, 1, 'Overflowing selection.');
    await loadPage(page);
    const doc = await switchToDocumentView(page);

    const rootCard = doc.locator(`.comment-card[data-comment-id="${root.id}"]`);
    await expect(rootCard).toBeVisible();
    await rootCard.evaluate(cardEl => {
      // First text node of `el` holding some text.
      const firstText = (el: Element) => {
        const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
        let t: Node | null;
        while ((t = walker.nextNode()) && (t as Text).data.trim().length < 3);
        return t as Text;
      };
      const body = cardEl.querySelector(':scope > .comment-body')!;
      // The next document line with text, below the card.
      const next = Array.from(document.querySelectorAll('.line-block .line-content'))
        .find(el => !!(cardEl.compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING) && !!el.textContent!.trim())!;
      const range = document.createRange();
      const end = firstText(next);
      range.setStart(firstText(body), 0);
      range.setEnd(end, end.data.search(/\S/) + 3);
      const selection = window.getSelection()!;
      selection.removeAllRanges();
      selection.addRange(range);
    });
    await page.keyboard.press('c');

    await expect(doc.locator(`.comment-form[data-form-key^="${mdPath}:"] textarea`)).toBeVisible();
    await expect(page.locator('.subthread-composer')).toHaveCount(0);
  });

  test('a sub-thread body can be edited in place', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const root = await addComment(request, mdPath, 1, 'Edit the nested one.');
    const sub = await addSubThread(request, mdPath, root.id, 'Original sub-thread body', 'nested one', 9);
    await loadPage(page);
    await mdSection(page);

    const subCard = card(page, sub.id);
    await subCard.hover();
    await subCard.locator(':scope > .comment-header .comment-actions button[title="Edit"]').click();
    const editor = card(page, root.id).locator(':scope > .subthreads .comment-form');
    await expect(editor.locator('.comment-form-header')).toHaveText('Editing comment');
    await expect(editor.locator('textarea')).toHaveValue('Original sub-thread body');
    await editor.locator('textarea').fill('Edited sub-thread body');
    await editor.getByRole('button', { name: 'Update Comment' }).click();

    await expect(editor).toHaveCount(0);
    await expect(nestedCards(card(page, root.id)).locator(':scope > .comment-body')).toHaveText('Edited sub-thread body');
    await expect(card(page, sub.id).locator(':scope > .subthread-quote')).toHaveText('nested one');
    expect((await fileComments(request, mdPath)).find(c => c.id === sub.id)?.body).toBe('Edited sub-thread body');
  });
});

// Story chapters render threads through their own Pierre FileDiffs. The
// story must be cleared after the test: specs of this project share one
// server.
const SUBTHREAD_STORY = {
  version: 1,
  agent: 'e2e-test',
  prologue: {
    title: 'Sub-thread story',
    overview: 'One chapter on the server.go auth cluster.',
    key_changes: ['server.go gains auth middleware.'],
    risks: ['None for this fixture.'],
  },
  chapters: [
    {
      id: 'ch1',
      title: 'Server auth cluster',
      summary: 'The first server.go hunk.',
      hunk_refs: [{ file_path: 'server.go', old_start: 2 }],
    },
  ],
  support: [
    {
      hunk_refs: [
        { file_path: 'server.go', old_start: 17 },
        { file_path: 'server.go', old_start: 39 },
        { file_path: 'routes.go', old_start: 1 },
        { file_path: 'routes.go', old_start: 48 },
        { file_path: 'plan.md', old_start: 0 },
      ],
      reason: 'Additional fixture hunks keep the story above ingestion coverage requirements.',
    },
  ],
};

test.describe('Sub-threads in story mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
    await request.delete('/api/story');
  });

  test.afterEach(async ({ request }) => {
    await request.delete('/api/story');
  });

  test('a passage of a reply in a story chapter opens a sub-thread there', async ({ page, request }) => {
    const root = await addComment(request, 'server.go', 24, 'Is the middleware ordered?');
    const answer = await addReply(request, 'server.go', root.id, 'Auth runs before logging.');
    await expect(await request.post('/api/story', { data: { story: SUBTHREAD_STORY } })).toBeOK();
    await loadPage(page);
    await page.locator('#crit-story-view-overview .crit-story-toc__item[data-story-target="ch1"]').click();

    const chapter = page.locator('#crit-story-view-ch1');
    const replyEl = chapter.locator(`.comment-reply[data-reply-id="${answer.id}"]`);
    await selectInMessageAndPressC(page, replyEl.locator(':scope > .reply-body'), 'before logging');
    const composer = composerUnder(replyEl);
    await expect(composer.locator('textarea')).toBeFocused();
    await composer.locator('textarea').fill('Checked in the router too?');
    await composer.locator('textarea').press('Control+Enter');

    const sub = nestedCards(replyEl);
    await expect(sub).toContainText('Checked in the router too?');
    await expect(sub.locator(':scope > .subthread-quote')).toHaveText('before logging');
    await expect.poll(() => messageQuoteHighlights(page)).toEqual(['before logging']);
  });
});

// ============================================================
// Collapse and resolve at every depth
// ============================================================
//
// A resolved card collapses, unless an open sub-thread sits under it (then it
// stays expanded); unresolving expands it again. The chevron collapses and
// re-expands any card, at any depth, resolved or not, and that choice holds
// until the card changes state: then the new state's default applies.

type Chain = { R: string; S1: string; S2: string; S3: string };

// A root thread and three nested sub-threads, each on the reply of the one
// above it (depths 0 to 3).
async function seedChain(request: APIRequestContext, filePath: string): Promise<Chain> {
  const root = await addComment(request, filePath, 1, 'Root thread body.');
  const chain: Partial<Chain> = { R: root.id };
  let parent = root.id;
  for (const level of ['S1', 'S2', 'S3'] as const) {
    const answer = await addReply(request, filePath, parent, `Answer above ${level}.`);
    const sub = await addSubThread(request, filePath, answer.id, `${level} body.`, 'Answer above', 0);
    chain[level] = sub.id;
    parent = sub.id;
  }
  return chain as Chain;
}

async function exerciseCollapse(scope: Locator, chain: Chain) {
  const { R, S1, S2, S3 } = chain;
  const card = (id: string) => scope.locator(`.comment-card[data-comment-id="${id}"]`);
  const chevron = (id: string) => card(id).locator(':scope > .comment-header .comment-collapse-btn').click();
  const expectCollapsed = (id: string) => expect(card(id)).toHaveClass(/(^|\s)collapsed(\s|$)/);
  const expectExpanded = (id: string) => expect(card(id)).not.toHaveClass(/(^|\s)collapsed(\s|$)/);
  // Resolve or Unresolve: the card's own button, then the rebuilt card.
  const toggleResolved = async (id: string, resolved: boolean) => {
    await card(id).locator(':scope > .comment-header .resolve-btn').click();
    if (resolved) await expect(card(id)).toHaveClass(/resolved-card/);
    else await expect(card(id)).not.toHaveClass(/resolved-card/);
  };

  // Resolving the root with sub-threads open under it: it stays expanded,
  // and the chevrons still collapse and re-expand the root and its child.
  await toggleResolved(R, true);
  await expect(card(R).locator('xpath=..')).toHaveClass(/has-open-subthreads/);
  await expectExpanded(R);
  await chevron(R);
  await expectCollapsed(R);
  await expect(card(S1)).toBeHidden();
  await chevron(R);
  await expectExpanded(R);
  await expect(card(S1)).toBeVisible();
  await chevron(S1);
  await expectCollapsed(S1);
  await expect(card(S2)).toBeHidden();
  await chevron(S1);
  await expectExpanded(S1);

  // Depth 1, resolved with open sub-threads under it: same as the root.
  await toggleResolved(S1, true);
  await expectExpanded(S1);
  await chevron(S1);
  await expectCollapsed(S1);
  await chevron(S1);
  await expectExpanded(S1);

  // Depth 3, nothing under it: resolving collapses it; the chevron re-expands
  // and collapses it again.
  await toggleResolved(S3, true);
  await expectCollapsed(S3);
  await chevron(S3);
  await expectExpanded(S3);
  await chevron(S3);
  await expectCollapsed(S3);

  // Depth 2, toggled by hand while open: resolving still collapses it. It was
  // the last open sub-thread under the resolved S1 and root: they collapse
  // too, although the user had re-expanded both with the chevron.
  await chevron(S2);
  await chevron(S2);
  await expectExpanded(S2);
  await toggleResolved(S2, true);
  await expectCollapsed(R);
  await expect(card(R).locator('xpath=..')).not.toHaveClass(/has-open-subthreads/);
  await chevron(R);
  await expectExpanded(R);
  await expectCollapsed(S1);
  await chevron(S1);
  await expectExpanded(S1);
  await expectCollapsed(S2);
  await chevron(S2);
  await expectExpanded(S2);

  // Unresolving the root, collapsed by hand just before, expands it.
  await chevron(R);
  await expectCollapsed(R);
  await toggleResolved(R, false);
  await expectExpanded(R);

  // Unresolving depth 2 expands it, and S1 above it, resolved with an open
  // sub-thread again, takes that state's default: expanded.
  await toggleResolved(S2, false);
  await expectExpanded(S2);
  await expectExpanded(S1);
  await expect(card(S1).locator('xpath=..')).toHaveClass(/has-open-subthreads/);
}

test.describe('Sub-thread collapse and resolve', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('in a rendered document: resolve, unresolve and chevrons at depths 0 to 3', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const chain = await seedChain(request, mdPath);
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await exerciseCollapse(doc, chain);
  });
});

// ============================================================
// Keeping the reader's place
// ============================================================
//
// Collapsing, expanding, resolving or unresolving a card changes its height.
// The review pane must not move under the reader: the clicked card keeps its
// place in the viewport (the list's own scroll anchor sits below it).

const LONG_TEXT = 'Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt. '.repeat(4);

// A card's top, from the top of the review pane.
function paneTop(card: Locator): Promise<number> {
  return card.evaluate(el => el.getBoundingClientRect().top - document.getElementById('filesContainer')!.getBoundingClientRect().top);
}

// Put a card's top 200px below the top of the review pane (once the pane has
// settled after the previous action).
async function placeCard(page: Page, card: Locator) {
  await expect(card).toBeVisible();
  await nextFrames(page);
  await waitForScrollStable(page);
  await card.evaluate(el => {
    const pane = document.getElementById('filesContainer')!;
    pane.scrollBy(0, el.getBoundingClientRect().top - pane.getBoundingClientRect().top - 200);
  });
  await nextFrames(page);
  await waitForScrollStable(page);
  expect(await page.locator('#filesContainer').evaluate(el => el.scrollTop), 'the pane is scrolled').toBeGreaterThan(0);
}

// Run `action` on a card, wait for its effect (`done`), then check the card
// sits where it was, within 3px, once the pane has settled.
async function expectCardHeld(page: Page, card: Locator, action: () => Promise<void>, done: () => Promise<void>) {
  const before = await paneTop(card);
  await action();
  await done();
  for (let i = 0; i < 3; i++) await nextFrames(page);
  await waitForScrollStable(page);
  expect(Math.abs((await paneTop(card)) - before), 'the clicked card did not move').toBeLessThanOrEqual(3);
}

async function exerciseHold(page: Page, scope: Locator, chain: Pick<Chain, 'R' | 'S1' | 'S2'>) {
  const card = (id: string) => scope.locator(`.comment-card[data-comment-id="${id}"]`);
  const chevron = (id: string) => () => card(id).locator(':scope > .comment-header .comment-collapse-btn').click();
  const resolveButton = (id: string) => () => card(id).locator(':scope > .comment-header .resolve-btn').click();
  const collapsed = (id: string, yes: boolean) => async () => {
    if (yes) await expect(card(id)).toHaveClass(/(^|\s)collapsed(\s|$)/);
    else await expect(card(id)).not.toHaveClass(/(^|\s)collapsed(\s|$)/);
  };
  const resolved = (id: string, yes: boolean) => async () => {
    if (yes) await expect(card(id)).toHaveClass(/resolved-card/);
    else await expect(card(id)).not.toHaveClass(/resolved-card/);
  };

  // Depth 0: the root, with its (resolved) sub-threads below it.
  await placeCard(page, card(chain.R));
  await expectCardHeld(page, card(chain.R), chevron(chain.R), collapsed(chain.R, true));
  await expectCardHeld(page, card(chain.R), chevron(chain.R), collapsed(chain.R, false));
  await expectCardHeld(page, card(chain.R), resolveButton(chain.R), resolved(chain.R, true));
  await expect(card(chain.R)).toHaveClass(/(^|\s)collapsed(\s|$)/);
  await expectCardHeld(page, card(chain.R), resolveButton(chain.R), resolved(chain.R, false));
  await expect(card(chain.R)).not.toHaveClass(/(^|\s)collapsed(\s|$)/);

  // Depth 2: open S1 (resolved, collapsed) to reach S2, then the same.
  await card(chain.S1).locator(':scope > .comment-header .comment-collapse-btn').click();
  await collapsed(chain.S1, false)();
  await placeCard(page, card(chain.S2));
  await expectCardHeld(page, card(chain.S2), chevron(chain.S2), collapsed(chain.S2, false));
  await expectCardHeld(page, card(chain.S2), chevron(chain.S2), collapsed(chain.S2, true));
  await expectCardHeld(page, card(chain.S2), resolveButton(chain.S2), resolved(chain.S2, false));
  await expect(card(chain.S2)).not.toHaveClass(/(^|\s)collapsed(\s|$)/);
  // Resolving it again closes S1 too (resolved, nothing open under it any
  // more): S2 is hidden in it, and S1 keeps its own place, as its chevron
  // would, or comes back to the pane top if its header was above it.
  const s1At = Math.max(0, await paneTop(card(chain.S1)));
  await resolveButton(chain.S2)();
  await collapsed(chain.S1, true)();
  for (let i = 0; i < 3; i++) await nextFrames(page);
  await waitForScrollStable(page);
  await expect(card(chain.S2)).toHaveClass(/resolved-card/);
  expect(Math.abs((await paneTop(card(chain.S1))) - s1At), 'S1 kept its place').toBeLessThanOrEqual(3);
}

// A root with long messages and two nested sub-threads, both resolved, so
// the root starts as a plain open thread.
async function seedTallChain(request: APIRequestContext, filePath: string): Promise<Pick<Chain, 'R' | 'S1' | 'S2'>> {
  const post = async (url: string, data: Record<string, unknown>) => {
    const resp = await request.post(url, { data });
    await expect(resp).toBeOK();
    return (await resp.json()) as { id: string };
  };
  const replyTo = (id: string, body: string) => filePath
    ? addReply(request, filePath, id, body)
    : post(`/api/review-comment/${id}/replies`, { body, author: 'agent' });
  const resolve = async (id: string) => {
    const url = filePath ? `/api/comment/${id}/resolve?path=${encodeURIComponent(filePath)}` : `/api/review-comment/${id}/resolve`;
    await expect(await request.put(url, { data: { resolved: true } })).toBeOK();
  };
  const root = filePath
    ? await addComment(request, filePath, 20, 'Root. ' + LONG_TEXT)
    : await post('/api/comments', { body: 'Root. ' + LONG_TEXT });
  const chain: Partial<Chain> = { R: root.id };
  let parent = root.id;
  for (const level of ['S1', 'S2'] as const) {
    const answer = await replyTo(parent, 'Answer. ' + LONG_TEXT);
    const sub = await addSubThread(request, filePath, answer.id, `${level}. ` + LONG_TEXT, 'Answer', 0);
    await replyTo(sub.id, 'Sub-thread reply. ' + LONG_TEXT);
    chain[level] = sub.id;
    parent = sub.id;
  }
  await resolve(chain.S1!);
  await resolve(chain.S2!);
  return chain as Pick<Chain, 'R' | 'S1' | 'S2'>;
}

test.describe('Sub-threads keep the reader in place', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('in a rendered document, collapsing, expanding and resolving a card at depth 0 and 2 does not move it', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const chain = await seedTallChain(request, mdPath);
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await exerciseHold(page, doc, chain);
  });

  test('in the review conversation, collapsing, expanding and resolving a card at depth 0 and 2 does not move it', async ({ page, request }) => {
    // Threads above the target, so it can sit mid-pane with the pane scrolled.
    for (let i = 0; i < 3; i++) {
      await expect(await request.post('/api/comments', { data: { body: `Earlier thread ${i}. ` + LONG_TEXT } })).toBeOK();
    }
    const chain = await seedTallChain(request, '');
    await loadPage(page);
    await exerciseHold(page, page.locator('#reviewConversation'), chain);
  });
});

// ============================================================
// Resolving the last open sub-thread under resolved cards
// ============================================================
//
// The resolved cards around it collapse too and the clicked card disappears
// inside the outermost one. That card keeps its own place, as its chevron
// would, or comes back to the pane top when its header was above it, also
// when the server answers late and the collapse is rendered many frames
// after the click.

// R (resolved) → S1 (resolved) → S2 (open, the last open sub-thread under
// them), with long messages, in a file's list.
async function seedResolvedAncestors(request: APIRequestContext, filePath: string, line = 20, rootResolved = true) {
  const replyTo = (id: string, body: string) => addReply(request, filePath, id, body);
  const resolve = async (id: string) => {
    await expect(await request.put(`/api/comment/${id}/resolve?path=${encodeURIComponent(filePath)}`, { data: { resolved: true } })).toBeOK();
  };
  const root = await addComment(request, filePath, line, 'Root. ' + LONG_TEXT);
  const answer = await replyTo(root.id, 'Answer. ' + LONG_TEXT);
  const s1 = await addSubThread(request, filePath, answer.id, 'S1. ' + LONG_TEXT, 'Answer', 0);
  const s1Answer = await replyTo(s1.id, 'S1 answer. ' + LONG_TEXT);
  const s2 = await addSubThread(request, filePath, s1Answer.id, 'S2. ' + LONG_TEXT, 'S1 answer', 0);
  await replyTo(s2.id, 'S2 reply. ' + LONG_TEXT + LONG_TEXT);
  await resolve(s1.id);
  if (rootResolved) await resolve(root.id);
  return { R: root.id, S1: s1.id, S2: s2.id };
}

// Put a card's top `at` px below the top of the review pane.
async function placeCardAt(page: Page, card: Locator, at: number) {
  await expect(card).toBeVisible();
  await nextFrames(page);
  await waitForScrollStable(page);
  await card.evaluate((el, y) => {
    const pane = document.getElementById('filesContainer')!;
    pane.scrollBy(0, el.getBoundingClientRect().top - pane.getBoundingClientRect().top - y);
  }, at);
  await nextFrames(page);
  await waitForScrollStable(page);
}

async function resolveAndSettle(page: Page, card: Locator, settled: () => Promise<void>) {
  await card.locator(':scope > .comment-header .resolve-btn').click();
  await settled();
  for (let i = 0; i < 3; i++) await nextFrames(page);
  await waitForScrollStable(page);
}

test.describe('Resolving the last open sub-thread under resolved cards', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('in a document, the collapsed root comes back to the pane top from above it, with a late server answer', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const chain = await seedResolvedAncestors(request, mdPath);
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await answerResolvesLate(page);
    const card = (id: string) => doc.locator(`.comment-card[data-comment-id="${id}"]`);
    await placeCardAt(page, card(chain.S2), 300);
    // The root's header is above the pane: only the clicked card is in view.
    const rootAt = await paneTop(card(chain.R));
    expect(rootAt).toBeLessThan(0);
    await resolveAndSettle(page, card(chain.S2), async () => {
      await expect(card(chain.R)).toHaveClass(/(^|\s)collapsed(\s|$)/);
    });
    await expect(card(chain.S2)).toBeHidden();
    expect(Math.abs(await paneTop(card(chain.R))), 'the collapsed root is at the pane top').toBeLessThanOrEqual(3);
  });

  test('in a document, under an open root, the collapsed sub-thread keeps its own place', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    // An open root: only S1 (resolved, header on screen) collapses.
    const chain = await seedResolvedAncestors(request, mdPath, 20, false);
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    const card = (id: string) => doc.locator(`.comment-card[data-comment-id="${id}"]`);
    await placeCardAt(page, card(chain.S1), 120);
    const s1At = await paneTop(card(chain.S1));
    await resolveAndSettle(page, card(chain.S2), async () => {
      await expect(card(chain.S1)).toHaveClass(/(^|\s)collapsed(\s|$)/);
    });
    await expect(card(chain.R)).not.toHaveClass(/(^|\s)collapsed(\s|$)/);
    expect(Math.abs((await paneTop(card(chain.S1))) - s1At), 'S1 kept its place').toBeLessThanOrEqual(3);
  });

  test('in a document with hide-resolved on, what follows the thread takes the thread\'s place', async ({ page, request }) => {
    const mdPath = await getMdPath(request);
    const chain = await seedResolvedAncestors(request, mdPath);
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await page.keyboard.press('h');
    await expect(page.locator('body')).toHaveClass(/hide-resolved/);
    const card = (id: string) => doc.locator(`.comment-card[data-comment-id="${id}"]`);
    // The resolved root stays shown while S2 is open.
    await placeCardAt(page, card(chain.S2), 300);
    const rootAt = await paneTop(card(chain.R));
    // The first document line after the thread.
    const nextStart = await card(chain.R).evaluate(el => {
      let next = el.parentElement!.nextElementSibling;
      while (next && !next.matches('.line-block')) next = next.nextElementSibling;
      return (next as HTMLElement).dataset.startLine!;
    });
    const next = doc.locator(`.line-block[data-start-line="${nextStart}"]`);
    await resolveAndSettle(page, card(chain.S2), async () => {
      await expect(card(chain.R)).toBeHidden();
    });
    expect(Math.abs((await paneTop(next)) - rootAt), 'the next line sits where the thread began').toBeLessThanOrEqual(3);
  });
});

import { test, expect, type Locator, type Page } from '@playwright/test';
import {
  addComment, clearAllComments, getMdPath, goSection, loadPage, switchToDocumentView,
  recordClassAdditions, expectClassAddedTo,
} from './helpers';

// How a comment body's markdown renders: in the rendered document, in a diff
// annotation, and in the comments panel. Every case is seeded through the API
// and checked on one page load (the form that submits them is covered by
// comments.spec.ts).

// A markdown table reaches a comment body through the sanitizer with its
// classes removed, so only element rules can draw it. Without them the browser
// default applies and the cells run together with no border at all.
const TABLE_MD = [
  '| Field | Type | Notes |',
  '| --- | --- | --- |',
  '| id | string | primary key |',
  '| name | string | display name |',
].join('\n');

async function expectAllCellBorders(table: Locator) {
  const cells = table.locator('th, td');
  await expect(cells).not.toHaveCount(0);
  for (const cell of await cells.all()) {
    for (const side of ['top', 'right', 'bottom', 'left']) {
      await expect(cell).toHaveCSS(`border-${side}-style`, 'solid');
      await expect(cell).toHaveCSS(`border-${side}-width`, '1px');
    }
  }
}

async function expectAccentLink(link: Locator, href: string) {
  await expect(link).toBeVisible();
  await expect(link).toHaveAttribute('href', href);
  // Accent colour, not the browser's default link blue.
  expect(await link.evaluate(el => getComputedStyle(el).color)).not.toBe('rgb(0, 0, 238)');
}

async function expectHighlightedCode(body: Locator) {
  const codeBlock = body.locator('pre code');
  await expect(codeBlock).toBeVisible();
  // Shiki upgrades the block asynchronously; tokens carry theme colors.
  await expect(codeBlock).toHaveAttribute('data-crit-code', 'highlighted');
  await expect(codeBlock.locator('span[style*="--diffs-token"]').first()).toBeVisible();
}

function card(scope: Locator | Page, id: string): Locator {
  return scope.locator(`.comment-card[data-comment-id="${id}"]`);
}

test.describe('Comment markdown rendering', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('document comments render markdown, tables, ref chips, and the panel copies them', async ({ page, request }) => {
    const path = await getMdPath(request);
    const quote = await addComment(request, path, 1, [
      'Intro.', '', '> proper blockquote', '', '**> bold-wrapped quoted question?**', '', 'Answer.',
    ].join('\n'));
    const hr = await addComment(request, path, 3, 'Above\n\n---\n\nBelow');
    const inline = await addComment(request, path, 5, '**bold text** and `inline code` and [a link](https://example.com)');
    const url = await addComment(request, path, 7, 'See https://example.com for details');
    const code = await addComment(request, path, 9, 'Check this:\n```go\nfunc main() {\n\tfmt.Println("hello")\n}\n```');
    const bareRef = await addComment(request, path, 16, `See also ${quote.id} for context`);
    const tickRef = await addComment(request, path, 18, `Related to \`${quote.id}\``);
    const missingRef = await addComment(request, path, 19, 'See c_000000 for details');
    const fencedId = await addComment(request, path, 20, '```\nc_aabbcc\n```');
    const tableReply = await addComment(request, path, 21, 'Which fields does this cover?');
    const reply = await request.post(`/api/comment/${tableReply.id}/replies?path=${encodeURIComponent(path)}`, {
      data: { body: TABLE_MD, author: 'agent' },
    });
    expect(reply.status()).toBe(201);

    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await recordClassAdditions(page, 'comment-ref-flash');

    // Blockquotes, including the agent-style **> question** line.
    const quotes = card(doc, quote.id).locator('.comment-body blockquote');
    await expect(quotes).toHaveCount(2);
    await expect(quotes.first()).toHaveText('proper blockquote');
    await expect(quotes.nth(1)).toHaveText('bold-wrapped quoted question?');
    await expect(card(doc, quote.id).locator('.comment-body strong')).toHaveCount(0);

    // Horizontal rule styling.
    const rule = card(doc, hr.id).locator('.comment-body hr');
    await expect(rule).toHaveCount(1);
    await expect(rule).toHaveCSS('height', '1px');
    await expect(rule).toHaveCSS('border-top-width', '0px');

    // Bold, inline code, links (written and bare URLs).
    const inlineBody = card(doc, inline.id).locator('.comment-body');
    await expect(inlineBody.locator('strong')).toHaveText('bold text');
    await expect(inlineBody.locator('code')).toHaveText('inline code');
    await expect(inlineBody.locator('a')).toHaveText('a link');
    await expect(inlineBody.locator('a')).toHaveAttribute('href', 'https://example.com');
    await expectAccentLink(card(doc, url.id).locator('.comment-body a'), 'https://example.com');

    // Fenced code is syntax highlighted.
    await expectHighlightedCode(card(doc, code.id).locator('.comment-body'));

    // A table in a reply draws a border on every cell.
    const replyTable = card(doc, tableReply.id).locator('.reply-body table');
    await expect(replyTable).toBeVisible();
    await expect(replyTable.locator('th')).toHaveCount(3);
    await expectAllCellBorders(replyTable);

    // Comment IDs become .comment-ref chips — bare or backtick-wrapped, even
    // when the ID is not on the page — but not inside code blocks.
    for (const ref of [bareRef, tickRef]) {
      const chips = card(doc, ref.id).locator('.comment-ref');
      await expect(chips).toHaveCount(1);
      await expect(chips).toContainText(quote.id);
    }
    await expect(card(doc, missingRef.id).locator('.comment-ref')).toHaveCount(1);
    await expect(card(doc, fencedId.id).locator('.comment-body')).toContainText('c_aabbcc');
    await expect(card(doc, fencedId.id).locator('.comment-ref')).toHaveCount(0);

    // Clicking a chip scrolls to and flashes the referenced card.
    await expectClassAddedTo(page, 'comment-ref-flash',
      () => card(doc, bareRef.id).locator('.comment-ref').click(), quote.id);
    await expect(card(doc, quote.id)).toBeInViewport();

    // A chip whose ID is absent from the page is a no-op, not an error.
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));
    const missingChip = card(doc, missingRef.id).locator('.comment-ref');
    await missingChip.scrollIntoViewIfNeeded();
    await missingChip.click();
    // A later, observable action proves the click was handled before checking.
    await expectClassAddedTo(page, 'comment-ref-flash',
      () => card(doc, tickRef.id).locator('.comment-ref').click(), quote.id);
    expect(errors).toEqual([]);

    // The comments panel renders the same bodies: styled links, highlighted code.
    await page.keyboard.press('Shift+C');
    const panel = page.locator('.panel-comment-block');
    await expectAccentLink(card(panel, url.id).locator('.comment-body a'), 'https://example.com');
    await expectHighlightedCode(card(panel, code.id).locator('.comment-body'));
  });

  test('a ref chip to a card already on the page scrolls straight to it', async ({ page, request }) => {
    const path = await getMdPath(request);
    const target = await addComment(request, path, 16, 'Target comment');
    const ref = await addComment(request, path, 18, `See ${target.id}`);

    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await recordClassAdditions(page, 'comment-ref-flash');

    // Put the chip near the top of the view; the target card sits just above.
    const chip = card(doc, ref.id).locator('.comment-ref');
    await expect(chip).toBeVisible();
    await page.evaluate(() => {
      const root = document.getElementById('filesContainer')!;
      const chipEl = document.querySelector('.comment-ref')!;
      root.scrollTop += chipEl.getBoundingClientRect().top - root.getBoundingClientRect().top - 40;
      const w = window as unknown as { __minScrollTop: number };
      w.__minScrollTop = root.scrollTop;
      root.addEventListener('scroll', () => { w.__minScrollTop = Math.min(w.__minScrollTop, root.scrollTop); });
    });
    await expect(card(doc, target.id)).toBeAttached();

    await expectClassAddedTo(page, 'comment-ref-flash', () => chip.click(), target.id);
    await expect(card(doc, target.id)).toBeInViewport();

    // The scroll never passed above where it ended: no detour to the top of the file.
    await expect.poll(() => page.evaluate(() => {
      const root = document.getElementById('filesContainer')!;
      const w = window as unknown as { __minScrollTop: number };
      return root.scrollTop - w.__minScrollTop;
    })).toBeLessThanOrEqual(2);
  });

  test('a ref chip jumps the document even when the comments panel holds a copy of the card', async ({ page, request }) => {
    // The target is in another file, so its card is not mounted in the list.
    // The panel keeps its cards after it closes; the click must not stop there.
    const path = await getMdPath(request);
    const target = await addComment(request, 'server.go', 5, 'Target comment');
    const ref = await addComment(request, path, 18, `See ${target.id}`);

    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await page.locator('#commentCount').click();
    await expect(card(page.locator('#commentsPanelBody'), target.id)).toBeVisible();
    await page.locator('#commentCount').click();
    await expect(page.locator('#commentsPanel')).toHaveClass(/comments-panel-hidden/);

    const chip = card(doc, ref.id).locator('.comment-ref');
    await chip.scrollIntoViewIfNeeded();
    await chip.click();
    await expect(card(page.locator('#filesContainer'), target.id)).toBeInViewport();
  });

  test('a ref chip to a review comment opens the collapsed review conversation', async ({ page, request }) => {
    const res = await request.post('/api/comments', { data: { body: 'Review-level target' } });
    await expect(res).toBeOK();
    const target = await res.json();
    const path = await getMdPath(request);
    const ref = await addComment(request, path, 18, `See ${target.id}`);

    await loadPage(page);
    const conversation = page.locator('#reviewConversation');
    await conversation.locator('.review-conversation-toggle').click();
    await expect(conversation).toHaveClass(/collapsed/);
    await expect(card(conversation, target.id)).toHaveCount(0);
    const doc = await switchToDocumentView(page);
    // Same flash as a ref to a card that is already shown.
    await recordClassAdditions(page, 'comment-ref-flash');

    const chip = card(doc, ref.id).locator('.comment-ref');
    await expect(chip).toBeVisible();
    await expectClassAddedTo(page, 'comment-ref-flash', () => chip.click(), target.id);
    await expect(conversation).not.toHaveClass(/collapsed/);
    await expect(card(conversation, target.id)).toBeInViewport();
  });

  test('diff comments render markdown and tables', async ({ page, request }) => {
    // server.go new line 5 (`"log"`) is an addition.
    const table = await addComment(request, 'server.go', 5, TABLE_MD);
    const inline = await addComment(request, 'server.go', 5, 'This has **bold**, `code`, and a [link](https://test.com)');

    await loadPage(page);
    const section = await goSection(page);

    const tableEl = card(section, table.id).locator('.comment-body table');
    await expect(tableEl).toBeVisible();
    await expect(tableEl).toHaveCSS('border-collapse', 'collapse');
    await expect(tableEl.locator('th')).toHaveCount(3);
    await expect(tableEl.locator('tbody tr')).toHaveCount(2);
    await expectAllCellBorders(tableEl);

    const body = card(section, inline.id).locator('.comment-body');
    await expect(body.locator('strong')).toHaveText('bold');
    await expect(body.locator('code')).toHaveText('code');
    await expect(body.locator('a')).toHaveText('link');
    await expect(body.locator('a')).toHaveAttribute('href', 'https://test.com');
  });
});

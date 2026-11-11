import { test, expect, type Locator } from '@playwright/test';
import { addComment, clearAllComments, loadPage } from './helpers';

async function textPosition(element: Locator) {
  return element.evaluate(el => {
    const range = document.createRange();
    range.selectNodeContents(el);
    const text = range.getBoundingClientRect();
    const container = el.closest('.line-content, .comment-body, .reply-body')!.getBoundingClientRect();
    return { left: text.left, right: text.right, midpoint: (container.left + container.right) / 2 };
  });
}

test.describe('RTL text — Single File Mode', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('rendered Hebrew and English blocks align by their own direction', async ({ page }) => {
    await loadPage(page);
    const doc = page.locator('#filesContainer .document-wrapper');
    const hebrew = doc.locator('.line-content p', { hasText: 'שלום, Hello world.' });
    const english = doc.locator('.line-content p', { hasText: 'Hello, שלום עולם.' });

    await expect(hebrew).toHaveAttribute('dir', 'auto');
    await expect(english).toHaveAttribute('dir', 'auto');
    expect(await hebrew.evaluate(el => getComputedStyle(el).direction)).toBe('rtl');
    expect(await english.evaluate(el => getComputedStyle(el).direction)).toBe('ltr');
    const hebrewPosition = await textPosition(hebrew);
    const englishPosition = await textPosition(english);
    expect(hebrewPosition.right).toBeGreaterThan(hebrewPosition.midpoint);
    expect(englishPosition.left).toBeLessThan(englishPosition.midpoint);

    const listItem = doc.locator('.line-content li', { hasText: 'שלום, list item' }).last();
    const quote = doc.locator('.line-content blockquote', { hasText: 'שלום, quoted text' });
    const header = doc.locator('.line-content', { hasText: 'שלום כותרת' }).last();
    expect(await listItem.evaluate(el => getComputedStyle(el).direction)).toBe('rtl');
    expect(await quote.evaluate(el => getComputedStyle(el).borderRightWidth)).toBe('3px');
    expect(await header.evaluate(el => getComputedStyle(el).direction)).toBe('rtl');

    const code = doc.locator('.line-content.code-line').first();
    await expect(code).toHaveAttribute('dir', 'ltr');
    expect(await code.evaluate(el => getComputedStyle(el).direction)).toBe('ltr');
  });

  test('comment paragraphs and replies choose direction independently', async ({ page, request }) => {
    const comment = await addComment(request, 'plan.md', 1, 'שלום comment\n\nHello comment');
    const reply = await request.post(`/api/comment/${comment.id}/replies?path=plan.md`, {
      data: { body: 'שלום reply', author: 'Agent' },
    });
    await expect(reply).toBeOK();
    await loadPage(page);

    const card = page.locator(`.comment-card[data-comment-id="${comment.id}"]`);
    const paragraphs = card.locator('.comment-body p');
    expect(await paragraphs.nth(0).evaluate(el => getComputedStyle(el).direction)).toBe('rtl');
    expect(await paragraphs.nth(1).evaluate(el => getComputedStyle(el).direction)).toBe('ltr');
    const position = await textPosition(paragraphs.nth(0));
    expect(position.right).toBeGreaterThan(position.midpoint);
    expect(await card.locator('.reply-body p').evaluate(el => getComputedStyle(el).direction)).toBe('rtl');
    await expect(card.locator('.reply-input')).toHaveAttribute('dir', 'auto');
  });
});

import { test, expect, type Page, type APIRequestContext } from '@playwright/test';
import {
  clearAllComments, loadPage, addComment, switchToDocumentView, clearFocus,
  clickWhenHittable, openDocLineComment, recordClassAdditions, expectClassAddedTo,
} from './helpers';

// ============================================================
// Comment Navigation — ] / [ shortcuts and prev/next buttons
//
// The landed card carries .comment-nav-highlight for only one second, which a
// slow runner can miss when polling the class. Each step records the cards
// that gained the class (recordClassAdditions) and expects exactly the one it
// should have landed on.
//
// Also covered elsewhere: ] / [ typed into a comment textarea do not navigate
// (keyboard.spec.ts, "shortcuts type into a focused textarea"); the shortcuts
// pane lists ] and [ (web/__tests__/crit-settings-panes.test.js,
// renderShortcutsPane code-review mode).
// ============================================================

const NAV = 'comment-nav-highlight';

// Two comments on plan.md, opened in Document view with the recorder running.
async function openWithTwoComments(page: Page, request: APIRequestContext) {
  const first = await addComment(request, 'plan.md', 1, 'First comment');
  const second = await addComment(request, 'plan.md', 5, 'Second comment');
  await loadPage(page);
  await switchToDocumentView(page);
  await expect(page.locator('.comment-card')).toHaveCount(2);
  await clearFocus(page);
  await recordClassAdditions(page, NAV);
  return { first: first.id as string, second: second.id as string };
}

const press = (page: Page, key: string) => () => page.keyboard.press(key);
const click = (page: Page, id: string) => () => clickWhenHittable(page, page.locator(id));

test.describe('Comment Navigation', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('nav group and buttons appear once a comment exists', async ({ page }) => {
    await loadPage(page);
    const doc = await switchToDocumentView(page);
    await expect(page.locator('.comment-card')).toHaveCount(0);
    await expect(page.locator('#commentNavGroup')).not.toHaveClass(/has-comments/);

    await (await openDocLineComment(page, doc)).fill('Navigation test comment');
    await doc.locator('.comment-form .btn-primary').click();
    await expect(doc.locator('.comment-card')).toHaveCount(1);

    await expect(page.locator('#commentNavGroup')).toHaveClass(/has-comments/);
    await expect(page.locator('#commentNavPrev')).toBeVisible();
    await expect(page.locator('#commentNavNext')).toBeVisible();
  });

  test('] starts at the first comment, advances and wraps; [ goes back and wraps', async ({ page, request }) => {
    const { first, second } = await openWithTwoComments(page, request);

    await expectClassAddedTo(page, NAV, press(page, ']'), first);
    await expectClassAddedTo(page, NAV, press(page, ']'), second);
    await expectClassAddedTo(page, NAV, press(page, ']'), first); // wraps last → first
    await expectClassAddedTo(page, NAV, press(page, '['), second); // wraps first → last
    await expectClassAddedTo(page, NAV, press(page, '['), first); // previous
  });

  test('[ jumps to the last comment on initial press', async ({ page, request }) => {
    const { second } = await openWithTwoComments(page, request);
    await expectClassAddedTo(page, NAV, press(page, '['), second);
  });

  test('next button starts at the first comment; prev wraps to the last', async ({ page, request }) => {
    const { first, second } = await openWithTwoComments(page, request);
    await expectClassAddedTo(page, NAV, click(page, '#commentNavNext'), first);
    await expectClassAddedTo(page, NAV, click(page, '#commentNavPrev'), second);
  });

  test('prev button jumps to the last comment on initial click', async ({ page, request }) => {
    const { second } = await openWithTwoComments(page, request);
    await expectClassAddedTo(page, NAV, click(page, '#commentNavPrev'), second);
  });
});

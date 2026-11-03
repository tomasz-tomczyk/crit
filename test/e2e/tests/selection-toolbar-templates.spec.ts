import { test, expect } from '@playwright/test';
import type { Page, Locator } from '@playwright/test';
import {
  clearAllComments, getMdPath, loadPage, switchToDocumentView, selectInBlock, nextFrames,
} from './helpers';

// Pinned comment templates sit in the selection toolbar, before Comment: one
// click comments the selection with the template text. Settings place the
// toolbar or turn it off, and say whether buttons show their shortcut.

function toolbar(page: Page) {
  return page.locator('.crit-selection-toolbar');
}

function partialBlock(section: Locator) {
  return section.locator('.line-block', { hasText: 'API key authentication' });
}

async function setSetting(page: Page, key: string, value: unknown) {
  await page.evaluate(([k, v]) => window.crit.shared.setSetting(k as string, v), [key, value] as const);
}

async function savedComments(page: Page, request: import('@playwright/test').APIRequestContext) {
  const mdPath = await getMdPath(request);
  return await (await request.get('/api/file/comments?path=' + encodeURIComponent(mdPath))).json() as Array<{ body: string; quote?: string }>;
}

test.describe('Selection toolbar templates (git mode)', () => {
  let section: Locator;

  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
    section = await switchToDocumentView(page);
  });

  test('pinned 👍 and 👎 come before Comment, and one click comments the selection', async ({ page, request }) => {
    const selected = await selectInBlock(page, partialBlock(section));
    const buttons = toolbar(page).locator('button');
    await expect(buttons).toHaveCount(3);
    await expect(buttons.nth(0)).toHaveAccessibleName('Comment "Accepted"');
    await expect(buttons.nth(1)).toHaveAccessibleName('Comment "Rejected"');
    await expect(buttons.nth(2)).toHaveAccessibleName('Comment on selection');

    await buttons.nth(0).click();
    await expect(section.locator('.comment-card', { hasText: 'Accepted' })).toBeVisible();
    await expect(section.locator('.comment-form')).toHaveCount(0);
    const saved = (await savedComments(page, request)).find(c => c.body === 'Accepted');
    expect(saved?.quote?.replace(/\s+/g, ' ')).toBe(selected.replace(/\s+/g, ' '));
  });

  test('each template shows its emoji, its title or text, or both', async ({ page }) => {
    await page.evaluate(() => {
      const t = window.crit.commentTemplates;
      t.getTemplateItems();
      t.updateTemplate(1, { display: 'text', title: 'No' });
      t.addTemplate('Please add a unit test for this branch.', { title: 'Ask for tests', emoji: '🧪', display: 'both', pinned: true });
      t.addTemplate('Not pinned', { emoji: '🙈' });
    });
    await selectInBlock(page, partialBlock(section));
    await expect(toolbar(page).locator('.crit-selection-toolbar-pin')).toHaveText(['👍', 'No', '🧪Ask for tests']);
  });

  test('a template shortcut comments the selection; without a selection it does nothing', async ({ page, request }) => {
    await page.evaluate(() => {
      const t = window.crit.commentTemplates;
      t.getTemplateItems();
      t.updateTemplate(0, { shortcut: 'y' });
    });
    await selectInBlock(page, partialBlock(section));
    await expect(toolbar(page).locator('.crit-selection-toolbar-pin').first().locator('kbd')).toHaveText('y');
    await page.keyboard.press('y');
    await expect(section.locator('.comment-card', { hasText: 'Accepted' })).toBeVisible();
    expect((await savedComments(page, request)).filter(c => c.body === 'Accepted')).toHaveLength(1);

    await page.evaluate(() => window.getSelection()!.removeAllRanges());
    await page.keyboard.press('y');
    await nextFrames(page);
    expect((await savedComments(page, request)).filter(c => c.body === 'Accepted')).toHaveLength(1);
  });

  test('Toolbar shortcuts off hides every key, Comment\'s too', async ({ page }) => {
    await setSetting(page, 'selectionToolbarShortcuts', false);
    await selectInBlock(page, partialBlock(section));
    await expect(toolbar(page)).toBeVisible();
    await expect(toolbar(page).locator('kbd:visible')).toHaveCount(0);
  });

  test('top-left aligns on the selection start, bottom-right sits under its end, off hides the toolbar', async ({ page }) => {
    const selectionEdges = () => page.evaluate(() => {
      const rects = Array.from(window.getSelection()!.getRangeAt(0).getClientRects()).filter(r => r.width > 0);
      return { left: rects[0].left, right: rects[rects.length - 1].right, bottom: rects[rects.length - 1].bottom };
    });

    await setSetting(page, 'selectionToolbar', 'top-left');
    await selectInBlock(page, partialBlock(section));
    await expect(toolbar(page)).toBeVisible();
    let box = (await toolbar(page).boundingBox())!;
    let edges = await selectionEdges();
    expect(Math.abs(box.x - edges.left)).toBeLessThan(2);
    await page.keyboard.press('Escape');
    await page.evaluate(() => window.getSelection()!.removeAllRanges());

    await setSetting(page, 'selectionToolbar', 'bottom-right');
    await selectInBlock(page, partialBlock(section));
    await expect(toolbar(page)).toBeVisible();
    box = (await toolbar(page).boundingBox())!;
    edges = await selectionEdges();
    await expect(toolbar(page)).toHaveAttribute('data-placement', 'below');
    expect(Math.abs(box.x - edges.right)).toBeLessThan(2);
    expect(box.y).toBeGreaterThanOrEqual(edges.bottom);
    await page.keyboard.press('Escape');
    await page.evaluate(() => window.getSelection()!.removeAllRanges());

    await setSetting(page, 'selectionToolbar', 'off');
    await selectInBlock(page, partialBlock(section));
    await nextFrames(page);
    await expect(toolbar(page)).toBeHidden();
    // `c` still comments the selection.
    await page.keyboard.press('c');
    await expect(section.locator('.comment-form textarea')).toBeVisible();
  });

  test('"closest to the cursor" follows the direction the selection was made in', async ({ page }) => {
    // Drag across the paragraph forwards (left to right) or backwards.
    const drag = async (forwards: boolean) => {
      await page.evaluate(() => window.getSelection()!.removeAllRanges());
      const content = partialBlock(section).locator('.line-content').first();
      const box = (await content.boundingBox())!;
      const y = box.y + box.height / 2;
      const [from, to] = forwards ? [box.x + 80, box.x + 250] : [box.x + 250, box.x + 80];
      await page.mouse.move(from, y);
      await page.mouse.down();
      await page.mouse.move(to, y, { steps: 5 });
      await page.mouse.up();
      await expect(toolbar(page)).toBeVisible();
      return page.evaluate(() => {
        const rects = Array.from(window.getSelection()!.getRangeAt(0).getClientRects()).filter(r => r.width > 0);
        return { left: rects[0].left, right: rects[rects.length - 1].right };
      });
    };

    await setSetting(page, 'selectionToolbar', 'top-cursor');
    let edges = await drag(true);
    expect(Math.abs((await toolbar(page).boundingBox())!.x - edges.right)).toBeLessThan(2);
    edges = await drag(false);
    expect(Math.abs((await toolbar(page).boundingBox())!.x - edges.left)).toBeLessThan(2);

    await setSetting(page, 'selectionToolbar', 'bottom-cursor');
    edges = await drag(false);
    expect(Math.abs((await toolbar(page).boundingBox())!.x - edges.left)).toBeLessThan(2);
    await expect(toolbar(page)).toHaveAttribute('data-placement', 'below');
  });

  test('Settings set the placement and the shortcut display', async ({ page }) => {
    await page.locator('#settingsToggle').click();
    const placement = page.locator('#selectionToolbarSelect');
    await expect(placement).toHaveValue('top-center');
    await placement.selectOption('bottom-right');
    // The switch input is visually hidden: click its label.
    await page.locator('label.comments-panel-switch:has(#toolbarShortcutsToggle)').click();
    await expect(page.locator('#toolbarShortcutsToggle')).not.toBeChecked();
    await page.locator('#settingsClose').click();
    const settings = await page.evaluate(() => ({
      placement: window.crit.shared.getSetting('selectionToolbar', null),
      shortcuts: window.crit.shared.getSetting('selectionToolbarShortcuts', null),
    }));
    expect(settings).toEqual({ placement: 'bottom-right', shortcuts: false });
  });
});

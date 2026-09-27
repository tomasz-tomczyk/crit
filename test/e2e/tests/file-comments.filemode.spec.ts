import { test, expect, type Locator } from '@playwright/test';
import {
  clearAllComments, loadPage, mdSection, goSection, jsSection, fileHeader, mdDocument,
  submitFileLevelComment, revealFile,
} from './helpers';

// File-level threads and the file compose form are annotations on line 0 of
// the file's Pierre item (above its first line / rendered document).
function fileLevel(item: Locator): Locator {
  return item.locator('[slot="annotation-0"]');
}

test.describe('File-level comments — File Mode', () => {
  test.beforeEach(async ({ request, page }) => {
    await clearAllComments(request);
    await loadPage(page);
  });

  test('source, prose and comments share centered reading width across settings and viewports', async ({ page, request }) => {
    for (const path of ['plan.md', 'server.go']) {
      for (const scope of ['file', 'line']) {
        const response = await request.post(`/api/file/comments?path=${path}`, {
          data: { scope, start_line: scope === 'file' ? 0 : 1, end_line: scope === 'file' ? 0 : 1, body: `${path} ${scope} width` },
        });
        expect(response.ok()).toBeTruthy();
      }
    }
    await loadPage(page);
    for (const viewport of [1600, 1000, 390]) {
      await page.setViewportSize({ width: viewport, height: 1000 });
      for (const choice of ['compact', 'default', 'wide']) {
        await page.getByRole('button', { name: 'Settings', exact: true }).click();
        await page.locator(`[data-settings-width="${choice}"]`).click();
        await page.keyboard.press('Escape');
        let documentWidth = 0;
        for (const path of ['plan.md', 'server.go']) {
          const item = await revealFile(page, path);
          await expect(async () => {
            const boxes = await item.locator('.comment-card').evaluateAll(cards => cards.map(card => {
              const r = card.getBoundingClientRect();
              return { width: r.width, x: r.x };
            }));
            expect(boxes).toHaveLength(2);
            expect(Math.abs(boxes[0].width - boxes[1].width)).toBeLessThan(2);
            expect(Math.abs(boxes[0].x - boxes[1].x)).toBeLessThan(2);
            if (path === 'plan.md') documentWidth = boxes[0].width;
            else {
              expect(Math.abs(boxes[0].width - documentWidth)).toBeLessThan(2);
              const source = await item.locator('pre[data-file]').boundingBox();
              const file = await item.boundingBox();
              expect(Math.abs(source!.width - documentWidth)).toBeLessThan(2);
              expect(Math.abs(source!.x + source!.width / 2 - file!.x - file!.width / 2)).toBeLessThan(2);
              const selectedWidth = { compact: 840, default: 1040, wide: 1280 }[choice]!;
              expect(Math.abs(source!.width - Math.min(selectedWidth, file!.width - 32))).toBeLessThan(2);
            }
          }).toPass();
          if (viewport === 390 && choice === 'wide') {
            const card = item.locator('.comment-card').filter({ hasText: `${path} line width` });
            // The sticky number gutter must not cover the card's left edge.
            await card.locator('.comment-collapse-btn').click();
            await expect(card).toHaveClass(/collapsed/);
            await card.locator('.comment-collapse-btn').click();
            await expect(card).not.toHaveClass(/collapsed/);
          }
        }
        const pane = await page.locator('#filesContainer').boundingBox();
        expect(pane!.x + pane!.width).toBeLessThanOrEqual(viewport);
      }
    }
  });

  // Rendered markdown: the card must land above the document, not below it
  // (Pierre appends a new annotation after the document in the same slot).
  test('file-level comments on a rendered document show above it, twice in a row', async ({ page, request }) => {
    const section = await mdSection(page);
    const opts = { header: fileHeader(page, 'plan.md'), scope: fileLevel(section), content: mdDocument(page) };
    await submitFileLevelComment(page, { ...opts, body: 'File-mode file comment' });
    await submitFileLevelComment(page, { ...opts, body: 'Second file-mode comment' });
    await expect(fileLevel(section).locator('.comment-card')).toHaveCount(2);

    await expect.poll(async () => {
      const comments = await (await request.get('/api/file/comments?path=plan.md')).json();
      return comments.map((c: { scope: string; body: string }) => `${c.scope}:${c.body}`);
    }).toEqual(['file:File-mode file comment', 'file:Second file-mode comment']);
  });

  test('cancel then reopen gives a fresh file composer', async ({ page }) => {
    const section = await mdSection(page);
    const textarea = fileLevel(section).locator('.comment-form textarea');
    await fileHeader(page, 'plan.md').locator('.file-comment-btn').click();
    await textarea.fill('Discarded draft');
    page.once('dialog', d => d.accept());
    await fileLevel(section).getByRole('button', { name: 'Cancel' }).click();
    await expect(section.locator('.comment-form')).toHaveCount(0);

    await submitFileLevelComment(page, {
      header: fileHeader(page, 'plan.md'), scope: fileLevel(section), content: mdDocument(page), body: 'Kept comment',
    });
    await expect(fileLevel(section).locator('.comment-card')).toHaveCount(1);
  });

  // server.go sits below the rendered plan.md: this also catches a wrong
  // height for the document item, which put later files at wrong offsets.
  for (const [name, open] of [['server.go', goSection], ['handler.js', jsSection]] as const) {
    test(`file-level comments on code file ${name} show above line 1, twice in a row`, async ({ page }) => {
      const section = await open(page);
      const opts = { header: fileHeader(page, name), scope: fileLevel(section), content: section.locator('[data-line="1"]').first() };
      await submitFileLevelComment(page, { ...opts, body: 'First on ' + name });
      await submitFileLevelComment(page, { ...opts, body: 'Second on ' + name });
    });
  }
});

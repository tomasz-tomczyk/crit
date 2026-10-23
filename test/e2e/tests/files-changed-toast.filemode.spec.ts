import { test, expect, type APIRequestContext } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { execFileSync } from 'child_process';
import { randomUUID } from 'crypto';
import { addComment, clearAllComments, jsSection, loadPageWithEvents, openLineComment, sessionCwd } from './helpers';

// Mid-round edits are picked up by the server's file watcher, but the page's
// file content is only refreshed by round-complete. The toast's Reload
// re-fetches the files in place, keeping the reader's place, views and open
// forms.

const TOAST = '#toast-files-changed';

test.describe('Files changed mid-round — File Mode', () => {
  let fixtureDir = '';
  const originals: Record<string, string> = {};

  function write(file: string, content: string) {
    fs.writeFileSync(path.join(fixtureDir, file), content);
  }

  // Wait until the server has picked up the file as it is on disk, so no
  // edit-detected from this spec reaches the next one.
  async function waitForServerCopy(request: APIRequestContext, file: string) {
    await expect.poll(async () => {
      const res = await request.get(`/api/file?path=${encodeURIComponent(file)}`);
      return (await res.json()).content;
    }, { timeout: 10_000 }).toBe(fs.readFileSync(path.join(fixtureDir, file), 'utf-8'));
  }

  test.beforeAll(async ({ request }) => {
    fixtureDir = await sessionCwd(request);
    for (const file of ['plan.md', 'handler.js']) {
      originals[file] = execFileSync('git', ['show', `HEAD:${file}`], { cwd: fixtureDir, encoding: 'utf-8' });
    }
  });

  test.afterAll(async ({ request }) => {
    for (const file of Object.keys(originals)) {
      write(file, originals[file]);
      await waitForServerCopy(request, file);
    }
  });

  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  test('toast shows, dismisses and returns; Reload keeps the reader\'s line and each file\'s view', async ({ page }) => {
    await loadPageWithEvents(page);
    const toast = page.locator(TOAST);
    await expect(toast).toHaveCount(0);

    // An edit shows the toast; Dismiss hides it; the next edit brings it back.
    write('handler.js', originals['handler.js'] + '\n// ' + randomUUID() + '\n');
    await expect(toast).toBeVisible({ timeout: 10_000 });
    await expect(toast).toHaveAttribute('role', 'status');
    await expect(page.locator('#waitingOverlay')).not.toHaveClass(/active/);
    await toast.getByRole('button', { name: 'Dismiss' }).click();
    await expect(toast).toHaveCount(0);

    // Collapse server.go: an unchanged file keeps its view across Reload.
    await page.locator('.tree-file', { hasText: 'server.go' }).first().click();
    const serverToggle = page.getByRole('button', { name: 'Toggle server.go' }).first();
    const serverCollapsed = () => serverToggle.evaluate(el => {
      const details = el.closest('details');
      return details ? !details.open : el.getAttribute('aria-expanded') === 'false';
    });
    await serverToggle.click();
    await expect.poll(serverCollapsed).toBe(true);

    // Read a plan.md block mid-screen, then edit plan.md ABOVE it: its start
    // line shifts, so the restore has to find it by its text.
    const blocks = page.locator('#filesContainer .line-block.kb-nav[data-file-path="plan.md"][data-start-line]');
    const index = Math.min(12, (await blocks.count()) - 1);
    await blocks.nth(index).evaluate(el => el.scrollIntoView({ block: 'center' }));
    // A plan.md block's own text (without its gutters), position and line:
    // by index before the edit, by text after it.
    const readBlock = (by: { index?: number; text?: string }) => page.evaluate((q) => {
      const own = (el: Element) => {
        const copy = el.cloneNode(true) as Element;
        copy.querySelectorAll('.line-gutter, .line-comment-gutter').forEach(g => g.remove());
        return (copy.textContent || '').trim();
      };
      const els = Array.from(document.querySelectorAll('#filesContainer .line-block.kb-nav[data-file-path="plan.md"][data-start-line]'));
      const el = q.text !== undefined ? els.find(e => own(e) === q.text) : els[q.index!];
      return el ? { text: own(el), y: el.getBoundingClientRect().top, line: (el as HTMLElement).dataset.startLine } : null;
    }, by);
    const findBlock = (text: string) => readBlock({ text });
    const start = (await readBlock({ index }))!;
    expect(start.text.length).toBeGreaterThan(5);
    expect(start.y).toBeGreaterThan(150);

    const lines = originals['plan.md'].split('\n');
    lines.splice(1, 0, '', 'Inserted above the reader.', '', 'And another paragraph.', '');
    write('plan.md', lines.join('\n'));
    await expect(toast).toBeVisible({ timeout: 10_000 });
    await toast.getByRole('button', { name: 'Reload' }).click();

    await expect(page.getByText('Inserted above the reader.')).toBeAttached({ timeout: 10_000 });
    await expect.poll(async () => {
      const now = await findBlock(start.text);
      return now ? Math.abs(now.y - start.y) : Infinity;
    }, { timeout: 10_000 }).toBeLessThan(4);
    expect((await findBlock(start.text))!.line).not.toBe(start.line);
    // Off-screen files aren't mounted. Scroll down to server.go (a tree click
    // would expand it).
    await expect.poll(() => page.evaluate(() => {
      if (document.querySelector('[aria-label="Toggle server.go"]')) return true;
      const list = document.getElementById('filesContainer')!;
      const scroller = list.scrollHeight > list.clientHeight ? list : document.scrollingElement!;
      scroller.scrollTop += 300;
      return false;
    }), { timeout: 10_000 }).toBe(true);
    await expect.poll(serverCollapsed).toBe(true);
    await expect(toast).toHaveCount(0);
  });

  test('Reload keeps a comment and a reply that are being written', async ({ page, request }) => {
    // In the file that changes, so its view is rebuilt by the refresh.
    await addComment(request, 'handler.js', 12, 'Existing comment');
    await loadPageWithEvents(page);
    const item = await jsSection(page);
    const form = await openLineComment(page, item, 5);
    await form.locator('textarea').fill('Half-written comment');
    const card = item.locator('.comment-card', { hasText: 'Existing comment' }).first();
    await card.locator('.reply-input').click();
    await card.locator('.reply-textarea').fill('Half-written reply');

    const marker = 'edited-' + randomUUID().slice(0, 8);
    write('handler.js', originals['handler.js'] + '\n// ' + marker + '\n');
    const toast = page.locator(TOAST);
    await expect(toast).toBeVisible({ timeout: 10_000 });
    await toast.getByRole('button', { name: 'Reload' }).click();
    await expect(toast).toHaveCount(0);

    await expect(item.getByText(marker).first()).toBeAttached({ timeout: 10_000 });
    await expect(item.locator('.comment-form textarea').first()).toHaveValue('Half-written comment');
    await expect(card.locator('.reply-textarea')).toHaveValue('Half-written reply');
  });

  test('Finish closes the toast, so Reload never runs over the waiting dialog', async ({ page }) => {
    await loadPageWithEvents(page);
    write('handler.js', originals['handler.js'] + '\n// ' + randomUUID() + '\n');
    const toast = page.locator(TOAST);
    await expect(toast).toBeVisible({ timeout: 10_000 });

    await page.locator('#finishBtn').click();
    await expect(page.locator('#waitingOverlay')).toHaveClass(/active/);
    await expect(toast).toHaveCount(0);
  });
});

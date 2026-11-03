import { test, expect, type Page } from '@playwright/test';
import { loadPage } from './helpers';

// Read aloud speaks through window.speechSynthesis. Headless Chromium has no
// voices, so a fake engine records each utterance and lets the test finish
// the current one (window.__finishUtterance) to advance the reader, or fire a
// word boundary in it (window.__boundary) as a real voice does while speaking.
async function installFakeSpeech(page: Page) {
  await page.addInitScript(() => {
    type FakeUtterance = {
      text: string; rate: number; volume: number;
      onend: (() => void) | null; onerror: (() => void) | null;
      onboundary: ((e: { name: string; charIndex: number }) => void) | null;
    };
    const w = window as unknown as {
      __spoken: FakeUtterance[];
      __finishUtterance: () => void;
      __boundary: (charIndex: number) => void;
    };
    w.__spoken = [];
    let current: FakeUtterance | null = null;
    class Utterance {
      text: string; rate = 1; volume = 1;
      onend: (() => void) | null = null; onerror: (() => void) | null = null;
      onboundary: ((e: { name: string; charIndex: number }) => void) | null = null;
      constructor(text: string) { this.text = text; }
    }
    const synth = {
      speak(u: FakeUtterance) { current = u; w.__spoken.push(u); },
      cancel() { current = null; },
      pause() {}, resume() {},
      speaking: false, pending: false, paused: false,
      getVoices() { return []; },
      addEventListener() {}, removeEventListener() {},
    };
    w.__finishUtterance = () => {
      const u = current;
      current = null;
      if (u && u.onend) u.onend();
    };
    w.__boundary = (charIndex: number) => {
      if (current && current.onboundary) current.onboundary({ name: 'word', charIndex });
    };
    Object.defineProperty(window, 'speechSynthesis', { value: synth, configurable: true });
    Object.defineProperty(window, 'SpeechSynthesisUtterance', { value: Utterance, configurable: true });
  });
}

type Spoken = { text: string; rate: number; volume: number };

function spoken(page: Page): Promise<Spoken[]> {
  return page.evaluate(() => (window as unknown as { __spoken: Spoken[] }).__spoken
    .map(u => ({ text: u.text, rate: u.rate, volume: u.volume })));
}

async function lastSpoken(page: Page): Promise<Spoken | undefined> {
  const all = await spoken(page);
  return all[all.length - 1];
}

function finishUtterance(page: Page) {
  return page.evaluate(() => (window as unknown as { __finishUtterance: () => void }).__finishUtterance());
}

function boundary(page: Page, charIndex: number) {
  return page.evaluate((i) => (window as unknown as { __boundary: (i: number) => void }).__boundary(i), charIndex);
}

// Click the middle of the first occurrence of `word` in the document text.
async function clickWord(page: Page, word: string) {
  const point = await page.evaluate((w) => {
    const root = document.getElementById('filesContainer')!;
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const at = (node.textContent || '').search(new RegExp('\\b' + w + '\\b'));
      if (at === -1) continue;
      const range = document.createRange();
      range.setStart(node, at);
      range.setEnd(node, at + w.length);
      const rect = range.getBoundingClientRect();
      return { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 };
    }
    return null;
  }, word);
  expect(point, word).not.toBeNull();
  await page.mouse.click(point!.x, point!.y);
}

// The fixture's "We're" may render with a typographic apostrophe.
const FIRST = /^We.re adding API key authentication to the server\.$/;
const SECOND = 'This is phase 1 of the auth system.';

// The document block whose text contains `text`.
function block(page: Page, text: string) {
  return page.locator('#filesContainer .line-block[data-start-line]').filter({ hasText: text }).first();
}

// A block is highlighted by a style rule, so check its accent shadow.
async function expectHighlighted(page: Page, text: string) {
  await expect.poll(() => block(page, text).evaluate(el => getComputedStyle(el).boxShadow), { message: text })
    .not.toBe('none');
}

test.describe('Read aloud — Single File Mode', () => {
  test.beforeEach(async ({ page }) => {
    await installFakeSpeech(page);
    await loadPage(page);
  });

  test('reads from the top, highlights each block and advances when a block ends', async ({ page }) => {
    const toggle = page.locator('#readAloudToggle');
    const bar = page.locator('#readAloudBar');
    await expect(toggle).toBeVisible();
    await expect(bar).toBeHidden();

    await toggle.click();
    await expect(toggle).toHaveAttribute('aria-pressed', 'true');
    await expect(bar).toBeVisible();
    await expect.poll(async () => (await lastSpoken(page))?.text).toBe('Authentication Plan');
    await expectHighlighted(page, 'Authentication Plan');
    await expect(page.locator('#readAloudPos')).toHaveText(/^1 \/ \d+$/);

    await finishUtterance(page);
    await expect.poll(async () => (await lastSpoken(page))?.text).toBe('Overview');
    await expect(page.locator('#readAloudPos')).toHaveText(/^2 \/ \d+$/);
    await expectHighlighted(page, 'Overview');
  });

  test('announces code blocks and reads table rows cell by cell', async ({ page }) => {
    await page.locator('#readAloudToggle').click();
    // Walk the whole document by finishing each utterance.
    await expect(page.locator('#readAloudBar')).toBeVisible();
    for (let i = 0; i < 60; i++) {
      if (await page.locator('#readAloudBar').isHidden()) break;
      await finishUtterance(page);
    }
    await expect(page.locator('#readAloudBar')).toBeHidden();
    const texts = (await spoken(page)).map(s => s.text);
    expect(texts).toContain('Code block.');
    expect(texts.join('\n')).not.toContain('func authMiddleware');
    const row = texts.indexOf('Auth method');
    expect(texts.slice(row, row + 4)).toEqual(['Auth method', 'OAuth, API keys, JWT', 'API keys', 'Simplest for M2M']);
    expect(texts[texts.length - 1]).toContain('Week 3');
  });

  test('previous, next and the seek slider move by sentence', async ({ page }) => {
    await page.locator('#readAloudToggle').click();
    await expect.poll(async () => (await lastSpoken(page))?.text).toBe('Authentication Plan');

    await page.locator('#readAloudNext').click();
    await page.locator('#readAloudNext').click();
    await expect(page.locator('#readAloudPos')).toHaveText(/^3 \/ \d+$/);
    await page.locator('#readAloudPrev').click();
    await expect(page.locator('#readAloudPos')).toHaveText(/^2 \/ \d+$/);
    expect((await lastSpoken(page))?.text).toBe('Overview');

    await page.locator('#readAloudSeek').fill('0');
    await expect(page.locator('#readAloudPos')).toHaveText(/^1 \/ \d+$/);
    expect((await lastSpoken(page))?.text).toBe('Authentication Plan');
  });

  test('clicking text seeks there, and only moves the position while paused', async ({ page }) => {
    // Clicks do nothing while stopped.
    await block(page, 'Open Questions').click();
    expect(await spoken(page)).toHaveLength(0);

    await page.locator('#readAloudToggle').click();
    await block(page, 'Open Questions').click();
    await expect.poll(async () => (await lastSpoken(page))?.text).toBe('Open Questions');
    await expectHighlighted(page, 'Open Questions');

    // A click on a line inside the code block lands on its announcement.
    await block(page, 'return func(w http.ResponseWriter').click();
    await expect.poll(async () => (await lastSpoken(page))?.text).toBe('Code block.');

    await page.locator('#readAloudPlayPause').click();
    await expect(page.locator('#readAloudBar')).toHaveClass(/paused/);
    await expect(page.locator('#readAloudPlayPause')).toHaveAttribute('aria-label', 'Resume');
    const countWhilePaused = (await spoken(page)).length;
    await block(page, 'Timeline').click();
    await expectHighlighted(page, 'Timeline');
    expect(await spoken(page)).toHaveLength(countWhilePaused);

    await page.locator('#readAloudPlayPause').click();
    await expect.poll(async () => (await lastSpoken(page))?.text).toBe('Timeline');
  });

  test('speed and volume apply to the current block and persist across reloads', async ({ page }) => {
    await page.locator('#readAloudToggle').click();
    await page.locator('#readAloudRate').selectOption('1.5');
    await expect.poll(async () => (await lastSpoken(page))?.rate).toBe(1.5);
    await page.locator('#readAloudVolume').fill('0.4');
    await expect.poll(async () => (await lastSpoken(page))?.volume).toBe(0.4);
    expect((await lastSpoken(page))?.text).toBe('Authentication Plan');

    await page.reload();
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(page.locator('#readAloudRate')).toHaveValue('1.5');
    await expect(page.locator('#readAloudVolume')).toHaveValue('0.4');
    await page.locator('#readAloudToggle').click();
    await expect.poll(async () => lastSpoken(page)).toMatchObject({ rate: 1.5, volume: 0.4 });
  });

  test('stop and Shift+L end the reading and clear the highlight', async ({ page }) => {
    const toggle = page.locator('#readAloudToggle');
    await page.keyboard.press('Shift+L');
    await expect(toggle).toHaveAttribute('aria-pressed', 'true');
    await expectHighlighted(page, 'Authentication Plan');

    await page.locator('#readAloudStop').click();
    await expect(page.locator('#readAloudBar')).toBeHidden();
    await expect(toggle).toHaveAttribute('aria-pressed', 'false');
    await expect.poll(() => block(page, 'Authentication Plan').evaluate(el => getComputedStyle(el).boxShadow)).toBe('none');

    await page.keyboard.press('Shift+L');
    await expect(page.locator('#readAloudBar')).toBeVisible();
    await page.keyboard.press('Shift+L');
    await expect(page.locator('#readAloudBar')).toBeHidden();
  });

  test('a paragraph is read sentence by sentence', async ({ page }) => {
    await page.locator('#readAloudToggle').click();
    await clickWord(page, 'We');
    await expect.poll(async () => (await lastSpoken(page))?.text).toMatch(FIRST);
    await finishUtterance(page);
    await expect.poll(async () => (await lastSpoken(page))?.text).toBe(SECOND);
    await expectHighlighted(page, 'adding API key authentication');
    await page.locator('#readAloudPrev').click();
    await expect.poll(async () => (await lastSpoken(page))?.text).toMatch(FIRST);
  });

  test('pause and resume continue from the word being spoken', async ({ page }) => {
    await page.locator('#readAloudToggle').click();
    await clickWord(page, 'We');
    await expect.poll(async () => (await lastSpoken(page))?.text).toMatch(FIRST);
    await boundary(page, (await lastSpoken(page))!.text.indexOf('authentication'));
    await page.locator('#readAloudPlayPause').click();
    await expect(page.locator('#readAloudBar')).toHaveClass(/paused/);
    await page.locator('#readAloudPlayPause').click();
    await expect.poll(async () => (await lastSpoken(page))?.text).toBe('authentication to the server.');
    // A speed change mid-sentence keeps the place too.
    await boundary(page, 'authentication to the '.length);
    await page.locator('#readAloudRate').selectOption('1.25');
    await expect.poll(async () => lastSpoken(page)).toMatchObject({ text: 'server.', rate: 1.25 });
  });

  test('clicking a word starts reading at that word', async ({ page }) => {
    await page.locator('#readAloudToggle').click();
    await expect.poll(async () => (await lastSpoken(page))?.text).toBe('Authentication Plan');
    await clickWord(page, 'phase');
    await expect.poll(async () => (await lastSpoken(page))?.text).toBe('phase 1 of the auth system.');
    await clickWord(page, 'server');
    await expect.poll(async () => (await lastSpoken(page))?.text).toBe('server.');
  });

  test('table cells are sought, highlighted and stepped through one by one', async ({ page }) => {
    // The cell of the row containing `text` at `column` (0-based content cell).
    const cell = (text: string, column: number) =>
      block(page, text).locator(':scope > .line-content').nth(column);
    const shadow = (text: string, column: number) =>
      cell(text, column).evaluate(el => getComputedStyle(el).boxShadow);

    await page.locator('#readAloudToggle').click();
    await expect.poll(async () => (await lastSpoken(page))?.text).toBe('Authentication Plan');
    await clickWord(page, 'Database');
    await expect.poll(async () => (await lastSpoken(page))?.text).toBe('Database');
    await expect.poll(() => shadow('Key storage', 2)).not.toBe('none');
    expect(await shadow('Key storage', 0)).toBe('none');

    await clickWord(page, 'rotation');
    await expect.poll(async () => (await lastSpoken(page))?.text).toBe('rotation');
    await page.locator('#readAloudNext').click();
    await expect.poll(async () => (await lastSpoken(page))?.text).toBe('Header format');
    await expect.poll(() => shadow('Header format', 0)).not.toBe('none');
  });
});

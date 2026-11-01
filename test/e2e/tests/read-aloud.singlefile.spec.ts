import { test, expect, type Page } from '@playwright/test';
import { loadPage } from './helpers';

// Read aloud speaks through window.speechSynthesis. Headless Chromium has no
// voices, so a fake engine records each utterance and lets the test finish
// the current one (window.__finishUtterance) to advance the reader.
async function installFakeSpeech(page: Page) {
  await page.addInitScript(() => {
    type FakeUtterance = { text: string; rate: number; volume: number; onend: (() => void) | null; onerror: (() => void) | null };
    const w = window as unknown as {
      __spoken: FakeUtterance[];
      __finishUtterance: () => void;
    };
    w.__spoken = [];
    let current: FakeUtterance | null = null;
    class Utterance {
      text: string; rate = 1; volume = 1;
      onend: (() => void) | null = null; onerror: (() => void) | null = null;
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
    expect(texts).toContain('Auth method OAuth, API keys, JWT API keys Simplest for M2M');
    expect(texts[texts.length - 1]).toContain('Week 3');
  });

  test('previous, next and the seek slider move by block', async ({ page }) => {
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
});

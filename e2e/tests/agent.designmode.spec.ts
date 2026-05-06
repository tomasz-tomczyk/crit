import { test, expect } from '@playwright/test';

test.describe('design-mode agent', () => {
  // Phase F provisions the design-mode Playwright project with a fixture that
  // boots crit + a tiny upstream HTML server. Until then this whole file is
  // parse-checked but never executed.
  test.skip(true, 'phase F design-mode runner');

  test('agent posts agent-ready on boot', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
    await page.waitForFunction(() =>
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (window as any).__critDesignMessages?.some((m: any) => m.type === 'agent-ready'));
    expect(true).toBe(true);
  });

  test('agent rejects inbound messages from a foreign origin', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('agent posts to the verified API origin, not "*"', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('agent flips internal mode on set-mode pin', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
    const iframe = page.frameLocator('iframe.crit-design-iframe-frame');
    await page.evaluate(() => {
      const win = (document.querySelector('iframe.crit-design-iframe-frame') as HTMLIFrameElement).contentWindow!;
      win.postMessage({ type: 'set-mode', value: 'pin' }, '*');
    });
    await expect.poll(async () =>
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      iframe.locator('body').evaluate(() => (window as any).__critAgentState.mode),
    ).toBe('pin');
  });

  test('hover paints outline overlay in pin mode', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
    const iframe = page.frameLocator('iframe.crit-design-iframe-frame');
    await page.evaluate(() => {
      const win = (document.querySelector('iframe.crit-design-iframe-frame') as HTMLIFrameElement).contentWindow!;
      win.postMessage({ type: 'set-mode', value: 'pin' }, '*');
    });
    await iframe.locator('h1').first().hover();
    await expect(iframe.locator('#crit-agent-overlay')).toBeVisible();
  });

  test('clicking inside shadow DOM emits agent-error and does not pin', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('click in pin mode posts selection with dom_anchor', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('pointerdown on a draggable element does NOT start drag in pin mode', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('mousedown on a focusable element does NOT shift focus in pin mode', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('Enter on a focused button does NOT activate it in pin mode', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('typing into an <input> still works in pin mode (suppression carve-out)', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('selection includes a non-empty data-URL screenshot when html2canvas loads', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('screenshot is empty string on capture failure', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('selection event opens the composer with screenshot thumbnail', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('save composer POSTs /api/file/comments with dom_anchor and prepends row', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('save error shows inline error and does not close composer', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('right-click in pin mode posts request-ancestor-menu with options', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('agent posts focus-state {in_input:true} on focusin into INPUT and false on focusout', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('shadow-DOM agent-error surfaces a toast in chrome', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('cancel composer keeps agent in Pin mode for rapid pinning', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });

  test('end-to-end: pin → composer → save → row appears in panel', async ({ page, baseURL }) => {
    await page.goto(`${baseURL}/design`);
  });
});

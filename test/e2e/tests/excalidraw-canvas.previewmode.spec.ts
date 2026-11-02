import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { clearAllLivePins, enterPinMode, getIframe } from './livemode-helpers';

// A lone Excalidraw scene in preview mode (`crit arch.excalidraw`): the
// canvas page in the preview iframe, comments pinned to the scene's
// elements, and the canvas controls. Fixture: setup-fixtures-preview.sh.

type LiveMessage = { type: string; status?: string };
type PreviewComment = { dom_anchor?: { css_selector?: string; accessible_name?: string } };

function liveMessages(page: Page): Promise<LiveMessage[]> {
  return page.evaluate(() =>
    (window as unknown as { __critLiveMessages?: LiveMessage[] }).__critLiveMessages || []);
}

async function openCanvas(page: Page): Promise<void> {
  await page.goto('/preview');
  await expect(page.locator('#critLiveIframe')).toBeVisible({ timeout: 15_000 });
  await expect.poll(
    async () => (await liveMessages(page)).some((m) => m.type === 'agent-ready'),
    { timeout: 15_000 },
  ).toBe(true);
  await expect(getIframe(page).locator('#crit-canvas-islands')).toBeVisible();
}

// The selector and name each stored pin points at. Preview pins are keyed
// by the iframe's path.
async function pinnedElements(request: APIRequestContext): Promise<(string | undefined)[][]> {
  const resp = await request.get(`/api/file/comments?path=${encodeURIComponent('/preview-content/')}`);
  expect(resp.ok()).toBeTruthy();
  const comments = (await resp.json()) as PreviewComment[];
  return comments.map((c) => [c.dom_anchor?.css_selector, c.dom_anchor?.accessible_name]);
}

async function saveComment(page: Page, body: string): Promise<void> {
  await expect(page.locator('.crit-live-composer')).toBeVisible();
  await page.locator('.crit-live-composer-body').fill(body);
  await page.locator('.crit-live-composer-save').click();
  await expect(page.locator('.crit-live-composer')).toHaveCount(0);
}

test.describe('Excalidraw canvas preview', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllLivePins(request);
  });

  test('a lone scene opens as a canvas, each element an anchor named after it', async ({ page }) => {
    await openCanvas(page);
    const canvas = getIframe(page);
    await expect(canvas.locator('#crit-canvas-error')).toBeHidden();
    // The canvas fills the pane, whatever viewport other previews saved.
    await expect(page.locator('#liveViewportToggle [data-viewport="fit"]')).toHaveClass(/active/);

    await expect(canvas.locator('#excalidraw-auth')).toHaveAttribute('aria-label', 'rectangle "Auth service"');
    await expect(canvas.locator('#excalidraw-db')).toHaveAttribute('aria-label', 'ellipse "Orders DB"');
    await expect(canvas.locator('#excalidraw-query')).toHaveAttribute('aria-label', 'arrow "query"');
    await expect(canvas.locator('#excalidraw-reply'))
      .toHaveAttribute('aria-label', 'arrow from "Orders DB" to "Auth service"');
    await expect(canvas.locator('#excalidraw-note'))
      .toHaveAttribute('aria-label', 'text "Pins name the element they are on"');
    // A shape and its label are one element, as in Excalidraw.
    await expect(canvas.locator('#excalidraw-auth-label')).toHaveCount(0);
  });

  test('an arrow\'s label is drawn on its arrow, not at its saved x and y', async ({ page }) => {
    await openCanvas(page);
    const canvas = getIframe(page);
    // The fixture saves the "query" label at (0, 0), far from its arrow.
    const label = await canvas.locator('#excalidraw-query-label').boundingBox();
    const arrow = await canvas.locator('#excalidraw-query').boundingBox();
    expect(label).not.toBeNull();
    expect(arrow).not.toBeNull();
    expect(label!.x + label!.width / 2).toBeCloseTo(arrow!.x + arrow!.width / 2, 0);
    expect(label!.y + label!.height / 2).toBeCloseTo(arrow!.y + arrow!.height / 2, 0);
  });

  test('a click on a shape in Comment mode pins that element', async ({ page, request }) => {
    await openCanvas(page);
    await enterPinMode(page);
    await getIframe(page).locator('#excalidraw-db').click();
    await saveComment(page, 'Name the table');

    // The id and name point an agent at the element in the scene's JSON.
    await expect.poll(() => pinnedElements(request)).toEqual([['#excalidraw-db', 'ellipse "Orders DB"']]);
    await expect(getIframe(page).locator('.crit-live-marker')).toHaveCount(1);
    const statuses = async () => (await liveMessages(page))
      .filter((m) => m.type === 'pin-resolution-result')
      .map((m) => m.status);
    await expect.poll(statuses).toContain('resolved');
    expect(await statuses()).not.toContain('drifted');
  });

  test('a click on a shape\'s label pins the shape', async ({ page, request }) => {
    await openCanvas(page);
    await enterPinMode(page);
    // The label lets the pointer through, so click where it is drawn.
    const label = await getIframe(page).locator('text', { hasText: 'Auth service' }).boundingBox();
    expect(label).not.toBeNull();
    await page.mouse.click(label!.x + label!.width / 2, label!.y + label!.height / 2);
    await saveComment(page, 'Which auth?');

    await expect.poll(() => pinnedElements(request)).toEqual([['#excalidraw-auth', 'rectangle "Auth service"']]);
  });

  test('the zoom island and keys zoom the drawing, and Fit brings it back', async ({ page }) => {
    await openCanvas(page);
    await enterPinMode(page);
    const canvas = getIframe(page);
    const zoom = canvas.locator('#crit-canvas-zoom');
    const width = async () => (await canvas.locator('#excalidraw-auth').boundingBox())?.width ?? 0;

    // A scene smaller than the pane fits at 100%, never enlarged.
    await expect(zoom).toHaveText('100%');
    await expect.poll(width).toBeCloseTo(200, 0);

    const zoomIn = canvas.getByRole('button', { name: 'Zoom in' });
    await zoomIn.click();
    await expect(zoom).toHaveText('125%');
    await expect.poll(width).toBeCloseTo(250, 0);

    await zoomIn.press('-');
    await expect(zoom).toHaveText('100%');
    await zoomIn.press('+');
    await zoomIn.press('+');
    await expect(zoom).toHaveText('156%');

    await canvas.getByRole('button', { name: 'Zoom to fit' }).click();
    await expect(zoom).toHaveText('100%');
    await expect.poll(width).toBeCloseTo(200, 0);

    // The islands handle their own clicks: none of them became a pin.
    await expect(page.locator('.crit-live-composer')).toHaveCount(0);
  });

  test('the theme island switches the canvas to dark and keeps the view', async ({ page }) => {
    await openCanvas(page);
    const canvas = getIframe(page);
    await expect(canvas.locator('html')).toHaveAttribute('data-canvas-theme', 'light');
    await canvas.getByRole('button', { name: 'Zoom in' }).click();
    await expect(canvas.locator('#crit-canvas-zoom')).toHaveText('125%');

    await canvas.getByRole('button', { name: 'Switch light or dark canvas' }).click();
    await expect(canvas.locator('html')).toHaveAttribute('data-canvas-theme', 'dark');
    await expect(canvas.locator('#crit-canvas-zoom')).toHaveText('125%');
  });
});

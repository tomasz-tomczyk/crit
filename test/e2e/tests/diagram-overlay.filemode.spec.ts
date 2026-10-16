import { test, expect, type Page, type Locator } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { clearAllComments, loadPage, mdSection, roundComplete, sessionCwd } from './helpers';

const MERMAID_SNIPPET = `

## Diagram

\`\`\`mermaid
flowchart LR
  A[Start] --> B[End]
\`\`\`
`;

async function expandButton(page: Page): Promise<Locator> {
  const section = await mdSection(page);
  const block = section.locator('.line-content.render-block').first();
  await expect(block).toBeVisible();
  await block.scrollIntoViewIfNeeded();
  // Desktop Expand is opacity:0 / pointer-events:none until hover or focus.
  // Pierre pauses pointer events briefly after a scroll, so a hover that
  // lands during the pause doesn't register; re-hover until it reveals.
  const btn = block.locator('.crit-render-expand');
  await expect(async () => {
    await block.hover();
    await expect(btn).toHaveCSS('pointer-events', 'auto', { timeout: 500 });
  }).toPass({ timeout: 10_000 });
  await expect(btn).toBeVisible();
  return btn;
}

// Click Expand and wait for the overlay. A scroll (Playwright's own
// actionability scroll, or the diagram settling) pauses pointer events and
// drops the hover that reveals the button, so hover + click as one retry.
async function clickExpand(page: Page): Promise<Locator> {
  const btn = await expandButton(page);
  const overlay = page.locator('#diagramOverlay');
  const block = page.locator('.line-content.render-block').first();
  await expect(async () => {
    await block.hover();
    await expect(btn).toHaveCSS('pointer-events', 'auto', { timeout: 500 });
    await btn.click({ timeout: 1000 });
    await expect(overlay).toHaveClass(/active/, { timeout: 1000 });
  }).toPass({ timeout: 15_000 });
  return btn;
}

async function openOverlay(page: Page): Promise<Locator> {
  await clickExpand(page);
  const overlay = page.locator('#diagramOverlay');
  await expect(overlay).toHaveClass(/active/);
  return overlay;
}

test.describe('Mermaid fullscreen overlay — File Mode', () => {
  let planPath: string;
  let originalContent: string;

  // Append a mermaid fence to plan.md once and wait until the server has
  // re-read it; no test here edits the file.
  test.beforeAll(async ({ request }) => {
    planPath = path.join(await sessionCwd(request), 'plan.md');
    originalContent = fs.readFileSync(planPath, 'utf-8');
    fs.writeFileSync(planPath, originalContent + MERMAID_SNIPPET);
    await roundComplete(request);
  });

  test.afterAll(async ({ request }) => {
    fs.writeFileSync(planPath, originalContent);
    await roundComplete(request);
  });

  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
  });

  // Fence comments cover the whole fence (gutter); a node click is inert.
  test('rendered fence exposes Expand and a node click opens no comment form', async ({ page }) => {
    await loadPage(page);
    const btn = await expandButton(page);
    await expect(btn).toHaveAttribute('aria-label', 'Expand diagram fullscreen');
    await expect(btn).toContainText('Expand');

    const section = await mdSection(page);
    const block = section.locator('.line-content.render-block').first();
    await expect(block.locator('.crit-render[data-crit-state="rendered"]')).toBeVisible();
    await expect(block.locator('.crit-render')).not.toHaveAttribute('data-crit-anchorable', /.*/);
    const node = block.locator('.crit-render-output g.node', { hasText: 'Start' });
    await node.scrollIntoViewIfNeeded();
    await node.click();
    await expect(section.locator('.comment-form')).toHaveCount(0);
  });

  test('overlay shows a cloned SVG, zooms, and closes via button, backdrop and Esc', async ({ page }) => {
    await loadPage(page);
    const overlay = await openOverlay(page);

    // Expand opens the overlay with a cloned SVG and focuses Close.
    await expect(page.locator('#diagramOverlayCanvas svg')).toHaveCount(1);
    await expect(page.locator('#diagramOverlayClose')).toBeFocused();

    // Zoom in/out updates the label; Reset restores the fit percent.
    const label = page.locator('#diagramZoomLabel');
    await expect(label).toHaveText(/^\d+%$/);
    const fitText = (await label.textContent())!;
    await page.locator('#diagramZoomIn').click();
    await expect(label).not.toHaveText(fitText);
    const zoomedIn = (await label.textContent())!;
    await page.locator('#diagramZoomOut').click();
    await expect(label).not.toHaveText(zoomedIn);
    await page.locator('#diagramZoomReset').click();
    await expect(label).toHaveText(fitText);

    // Keyboard (focus is on Reset, inside the overlay): + zooms, 0 fits,
    // arrows pan.
    const canvas = page.locator('#diagramOverlayCanvas');
    await page.keyboard.press('+');
    await expect(label).not.toHaveText(fitText);
    await page.keyboard.press('0');
    await expect(label).toHaveText(fitText);
    const before = await canvas.evaluate(el => el.style.transform);
    await page.keyboard.press('ArrowRight');
    await expect.poll(() => canvas.evaluate(el => el.style.transform)).not.toBe(before);

    // A drag on the viewport keeps the keys working (focus stays in the
    // overlay), and arrows do not reach the review list behind it.
    const viewport = page.locator('#diagramOverlayViewport');
    const box = (await viewport.boundingBox())!;
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width / 2 + 40, box.y + box.height / 2 + 20, { steps: 4 });
    await page.mouse.up();
    const listTop = await page.locator('#filesContainer').evaluate(el => el.scrollTop);
    const dragged = await canvas.evaluate(el => el.style.transform);
    await page.keyboard.press('ArrowDown');
    await expect.poll(() => canvas.evaluate(el => el.style.transform)).not.toBe(dragged);
    await page.keyboard.press('+');
    await expect(label).not.toHaveText(fitText);
    await page.keyboard.press('0');
    await expect(label).toHaveText(fitText);
    expect(await page.locator('#filesContainer').evaluate(el => el.scrollTop)).toBe(listTop);
    await expect(overlay).toHaveClass(/active/);

    // Close button dismisses it.
    await page.locator('#diagramOverlayClose').click();
    await expect(overlay).not.toHaveClass(/active/);

    // Backdrop click dismisses it. Hit the overlay padding (16px) so
    // target === overlay, not a child.
    await openOverlay(page);
    await overlay.click({ position: { x: 8, y: 8 } });
    await expect(overlay).not.toHaveClass(/active/);

    // Esc dismisses it and returns focus to Expand.
    const btn = await clickExpand(page);
    await expect(overlay).toHaveClass(/active/);
    await page.keyboard.press('Escape');
    await expect(overlay).not.toHaveClass(/active/);
    await expect(btn).toBeFocused();
  });

  test('theme change and round-complete close the overlay', async ({ page, request }) => {
    await loadPage(page);
    const overlay = await openOverlay(page);

    // Settings sits under the mermaid overlay (z-index), so drive applyTheme
    // directly — same close path as the settings theme buttons.
    const applyTheme = (t: string) => page.evaluate(theme => {
      (window as unknown as { applyTheme: (t: string) => void }).applyTheme(theme);
    }, t);
    try {
      await applyTheme('dark');
      await expect(overlay).not.toHaveClass(/active/);
    } finally {
      await applyTheme('system');
    }

    await openOverlay(page);
    await roundComplete(request);
    await expect(overlay).not.toHaveClass(/active/);
  });
});

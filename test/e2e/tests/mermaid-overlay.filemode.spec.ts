import { test, expect, type Page, type APIRequestContext, type Locator } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { clearAllComments, loadPage, mdSection } from './helpers';

const MERMAID_SNIPPET = `

## Diagram

\`\`\`mermaid
flowchart LR
  A[Start] --> B[End]
\`\`\`
`;

async function getFixtureDir(request: APIRequestContext): Promise<string> {
  const res = await request.get('/api/session');
  const data = await res.json();
  return data.cwd;
}

async function syncPlanWithMermaid(
  request: APIRequestContext,
  fixtureDir: string,
  originalContent: string,
) {
  fs.writeFileSync(path.join(fixtureDir, 'plan.md'), originalContent + MERMAID_SNIPPET);
  await request.post('/api/round-complete');
}

async function expandButton(page: Page): Promise<Locator> {
  const section = mdSection(page);
  const block = section.locator('.line-content.mermaid-block').first();
  await expect(block).toBeVisible();
  // Desktop Expand is opacity:0 / pointer-events:none until hover or focus.
  await block.hover();
  const btn = block.locator('.mermaid-expand');
  await expect(btn).toBeVisible();
  return btn;
}

async function openOverlay(page: Page): Promise<Locator> {
  const btn = await expandButton(page);
  await btn.click();
  const overlay = page.locator('#mermaidOverlay');
  await expect(overlay).toHaveClass(/active/);
  return overlay;
}

test.describe('Mermaid fullscreen overlay — File Mode', () => {
  let fixtureDir: string;
  let originalContent: string;

  test.beforeAll(async ({ request }) => {
    fixtureDir = await getFixtureDir(request);
    originalContent = fs.readFileSync(path.join(fixtureDir, 'plan.md'), 'utf-8');
  });

  test.afterAll(() => {
    if (fixtureDir && originalContent) {
      fs.writeFileSync(path.join(fixtureDir, 'plan.md'), originalContent);
    }
  });

  test.beforeEach(async ({ request }) => {
    await clearAllComments(request);
    await syncPlanWithMermaid(request, fixtureDir, originalContent);
  });

  test('rendered diagram exposes Expand affordance', async ({ page }) => {
    await loadPage(page);
    const btn = await expandButton(page);
    await expect(btn).toHaveAttribute('aria-label', 'Open diagram fullscreen');
    await expect(btn).toContainText('Expand');
  });

  test('Expand opens overlay with a cloned SVG', async ({ page }) => {
    await loadPage(page);
    await openOverlay(page);

    const canvas = page.locator('#mermaidOverlayCanvas');
    await expect(canvas.locator('svg')).toHaveCount(1);
    await expect(page.locator('#mermaidOverlayClose')).toBeFocused();
  });

  test('zoom in/out updates label and Reset restores fit percent', async ({ page }) => {
    await loadPage(page);
    await openOverlay(page);

    const label = page.locator('#mermaidZoomLabel');
    const fitText = await label.textContent();
    expect(fitText).toMatch(/^\d+%$/);

    await page.locator('#mermaidZoomIn').click();
    await expect(label).not.toHaveText(fitText!);

    const zoomedIn = await label.textContent();
    await page.locator('#mermaidZoomOut').click();
    await expect(label).not.toHaveText(zoomedIn!);

    await page.locator('#mermaidZoomReset').click();
    await expect(label).toHaveText(fitText!);
  });

  test('Esc closes overlay and returns focus to Expand', async ({ page }) => {
    await loadPage(page);
    const btn = await expandButton(page);
    await btn.click();
    await expect(page.locator('#mermaidOverlay')).toHaveClass(/active/);

    await page.keyboard.press('Escape');
    await expect(page.locator('#mermaidOverlay')).not.toHaveClass(/active/);
    await expect(btn).toBeFocused();
  });

  test('close button dismisses overlay', async ({ page }) => {
    await loadPage(page);
    await openOverlay(page);

    await page.locator('#mermaidOverlayClose').click();
    await expect(page.locator('#mermaidOverlay')).not.toHaveClass(/active/);
  });

  test('backdrop click dismisses overlay', async ({ page }) => {
    await loadPage(page);
    await openOverlay(page);

    // Hit the overlay padding (16px) so target === overlay, not a child.
    await page.locator('#mermaidOverlay').click({ position: { x: 8, y: 8 } });
    await expect(page.locator('#mermaidOverlay')).not.toHaveClass(/active/);
  });

  test('theme change closes overlay', async ({ page }) => {
    await loadPage(page);
    await openOverlay(page);

    // Settings sits under the mermaid overlay (z-index), so drive applyTheme
    // directly — same close path as the settings theme buttons.
    await page.evaluate(() => {
      (window as unknown as { applyTheme: (t: string) => void }).applyTheme('dark');
    });
    await expect(page.locator('#mermaidOverlay')).not.toHaveClass(/active/);

    await page.evaluate(() => {
      (window as unknown as { applyTheme: (t: string) => void }).applyTheme('system');
    });
  });

  test('round-complete closes overlay', async ({ page, request }) => {
    await loadPage(page);
    await openOverlay(page);

    await request.post('/api/round-complete');
    await expect(page.locator('#mermaidOverlay')).not.toHaveClass(/active/);
  });
});

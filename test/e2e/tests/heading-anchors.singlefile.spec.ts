import { test, expect } from '@playwright/test';
import { loadPage } from './helpers';

// plan.md has 8 headings: h1 Authentication Plan; h2 Overview, Design
// Decisions, Implementation Steps, Open Questions, Timeline; h3 Step 1, Step 2.
const HEADING_COUNT = 8;

test.describe('Heading Anchors — Single File Mode', () => {
  test('a hash URL on first load scrolls to the heading', async ({ page }) => {
    // A fresh navigation (not a same-document hash change) exercises the
    // initial-load scroll after the document renders.
    await page.goto('/#timeline');
    await expect(page.locator('.loading')).toBeHidden({ timeout: 10_000 });
    await expect(page.locator('h2#timeline')).toBeInViewport();
  });

  test('headings get slug ids and anchors that update the hash, copy the link and follow hash changes', async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write']);
    await loadPage(page);

    // Every heading gets a slug id.
    const heading = page.locator('h2#overview');
    await expect(heading).toBeVisible();
    await expect(heading).toContainText('Overview');
    await expect(page.locator('h3#step-1-auth-middleware')).toBeVisible();
    await expect(page.locator('#filesContainer .document-wrapper').locator('h1[id], h2[id], h3[id], h4[id], h5[id], h6[id]')).toHaveCount(HEADING_COUNT);

    // The link icon is hidden until the heading is hovered.
    const anchor = heading.locator('.heading-anchor');
    await expect(anchor).toHaveCSS('opacity', '0');
    await heading.hover();
    await expect(anchor).toHaveCSS('opacity', '1');

    // Clicking it updates the URL hash and copies the heading link.
    await anchor.click();
    await expect(page).toHaveURL(/#overview$/);
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toContain('#overview');

    // A later hash change (in-page anchor link) scrolls to that heading.
    await page.evaluate(() => { window.location.hash = '#open-questions'; });
    await expect(page.locator('h2#open-questions')).toBeInViewport();
  });
});

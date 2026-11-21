import { test, expect } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { clearAllComments, loadPage, mdSection, roundComplete, sessionCwd } from './helpers';

// Issue #1203: under fractional display scaling Mermaid's HTML labels were
// measured a fraction off their wrapping width, never wrapped, and were cut
// off at the box edge. A transform on the page makes the measurement miss the
// same way in headless Chromium, which otherwise always measures whole pixels.
const MERMAID_SNIPPET = `

## Label wrapping

\`\`\`mermaid
flowchart LR
  A[Applied filters] --> B[Generated queries]
  B --> C[Query cache for generation]
  C --> D[Panel, condition tree]
  D --> E[Add or edit condition]
\`\`\`
`;

test.describe('Mermaid labels — File Mode', () => {
  let planPath: string;
  let originalContent: string;

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

  test('flowchart labels stay inside their boxes on a scaled page', async ({ page }) => {
    await page.addInitScript(() => {
      document.addEventListener('DOMContentLoaded', () => {
        document.documentElement.style.transform = 'scale(1.1)';
        document.documentElement.style.transformOrigin = '0 0';
      });
    });
    await loadPage(page);
    const section = await mdSection(page);
    const output = section.locator('.crit-render[data-crit-state="rendered"] .crit-render-output', { hasText: 'Applied filters' });
    await output.scrollIntoViewIfNeeded();
    await expect(output.locator('g.node')).toHaveCount(5);

    // Sizes in the diagram's own units: an HTML label's content box against
    // its foreignObject, an SVG text label's bounding box against the node.
    const clipped = await output.evaluate(el => [...el.querySelectorAll('g.node')].flatMap(node => {
      const div = node.querySelector('foreignObject > div');
      const text = node.querySelector('text');
      const fo = node.querySelector('foreignObject');
      const shape = node.querySelector('rect')!.getBBox();
      const label = div
        ? { width: div.scrollWidth, height: div.scrollHeight }
        : text!.getBBox();
      const room = fo
        ? { width: Number(fo.getAttribute('width')), height: Number(fo.getAttribute('height')) }
        : shape;
      return label.width > room.width + 0.5 || label.height > room.height + 0.5 ? [node.textContent!.trim()] : [];
    }));
    expect(clipped).toEqual([]);
  });
});

import { test, expect } from '@playwright/test';
import { clearAllComments, loadPage } from './helpers';

test.beforeEach(async ({ request }) => { await clearAllComments(request); });

test('unified line and quote lookup resolve old context coordinates and avoid opposite-side collisions', async ({ page }) => {
  await loadPage(page);
  const result = await page.evaluate(async () => {
    const P = await window.critPierreReady;
    if (!P) throw new Error('Pierre did not load');
    const container = document.createElement('div');
    document.body.append(container);
    const patch = 'diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1,4 +1,7 @@\n+insert1\n+insert2\n+insert3\n one\n two\n three\n-four\n+changed\n';
    let instance;
    await new Promise<void>(resolve => {
      instance = new P.FileDiff({ diffStyle: 'unified', theme: { light: 'github-light-default', dark: 'tokyo-night' }, onPostRender: resolve });
      instance.render({ fileDiff: P.processFile(patch), containerWrapper: container });
    });
    const dom = window.crit.pierreDOM;
    const old = dom.pierreLineElement(container, 4, 'old');
    const context = dom.pierreLineElement(container, 2, 'old');
    const newSide = dom.pierreLineElement(container, 4, '');
    const host = container.querySelector('diffs-container');
    if (!host) throw new Error('Pierre host did not mount');
    const decorations = dom.createDecorations();
    decorations.mount(host, 'audit.txt', [{ start: 4, end: 4, side: 'old', quote: 'four', offset: 0 }]);
    // mount publishes CSS Custom Highlight via queueMicrotask so a cached
    // FileDiff can attach its host in the same task first.
    await Promise.resolve();
    const highlights = [...(CSS.highlights.get('crit-quote') || [])];
    const highlighted = highlights.map(range => {
      const el = range.startContainer.parentElement?.closest('[data-line]') as HTMLElement | null;
      return { text: range instanceof Range ? range.toString() : '', type: el?.dataset.lineType };
    });
    decorations.unmount(host);
    instance.cleanUp();
    container.remove();
    return { old: old?.textContent, context: context?.textContent, newSide: newSide?.textContent, highlighted };
  });
  expect(result.old?.trim()).toBe('four');
  expect(result.context?.trim()).toBe('two');
  expect(result.newSide?.trim()).toBe('one');
  expect(result.highlighted).toEqual([{ text: 'four', type: 'change-deletion' }]);
});

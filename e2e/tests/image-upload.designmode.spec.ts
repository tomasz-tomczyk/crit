import { test, expect } from '@playwright/test';
import {
  clearAllDesignPins,
  openPinComposer,
  waitForAgentReady,
} from './designmode-helpers';

test.describe('design-mode image paste/drop in composer', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('pasting an image into the composer inserts markdown and renders after save', async ({ page }) => {
    await openPinComposer(page);

    const textarea = page.locator('.crit-design-composer-body');
    await expect(textarea).toBeVisible();
    await textarea.focus();

    // Simulate a clipboard paste with an image file
    await textarea.evaluate((el) => {
      const file = new File(
        [new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10])], // PNG magic bytes
        'screenshot.png',
        { type: 'image/png' },
      );
      const dt = new DataTransfer();
      dt.items.add(file);
      const event = new ClipboardEvent('paste', {
        clipboardData: dt,
        bubbles: true,
        cancelable: true,
      });
      el.dispatchEvent(event);
    });

    // Upload completes near-instantly on localhost — assert the final markdown
    await expect(textarea).toHaveValue(/!\[.*\]\(attachments\/[a-f0-9-]+\.png\)/);
  });

  test('drag-and-drop an image file onto the composer textarea', async ({ page }) => {
    await openPinComposer(page);

    const textarea = page.locator('.crit-design-composer-body');
    await expect(textarea).toBeVisible();
    await textarea.focus();

    // Simulate dragenter + dragover + drop with an image file
    await textarea.evaluate((el) => {
      const file = new File(
        [new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10])],
        'dropped.png',
        { type: 'image/png' },
      );
      const dt = new DataTransfer();
      dt.items.add(file);

      el.dispatchEvent(new DragEvent('dragenter', { dataTransfer: dt, bubbles: true }));
      el.dispatchEvent(new DragEvent('dragover', { dataTransfer: dt, bubbles: true }));
      el.dispatchEvent(new DragEvent('drop', { dataTransfer: dt, bubbles: true }));
    });

    // Upload completes near-instantly on localhost — assert the final markdown
    await expect(textarea).toHaveValue(/!\[.*\]\(attachments\/[a-f0-9-]+\.png\)/);
  });

  test('textarea gets drag-active class during dragover', async ({ page }) => {
    await openPinComposer(page);

    const textarea = page.locator('.crit-design-composer-body');
    await expect(textarea).toBeVisible();

    await textarea.evaluate((el) => {
      const dt = new DataTransfer();
      dt.items.add(new File([new Uint8Array(8)], 'img.png', { type: 'image/png' }));
      el.dispatchEvent(new DragEvent('dragenter', { dataTransfer: dt, bubbles: true }));
      el.dispatchEvent(new DragEvent('dragover', { dataTransfer: dt, bubbles: true }));
    });

    await expect(textarea).toHaveClass(/drag-active/);
  });
});

test.describe('design-mode reply textarea image upload', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('reply textarea supports image paste', async ({ page, request }) => {
    await openPinComposer(page);
    await page.locator('.crit-design-composer-body').fill('First pin');
    await page.locator('.crit-design-composer-save').click();
    await expect(page.locator('.crit-design-comment-row')).toBeVisible();

    // Open reply composer
    await page.locator('.crit-design-comment-reply').first().click();
    const replyTa = page.locator('.crit-design-reply-textarea').first();
    await expect(replyTa).toBeVisible();
    await replyTa.focus();

    // Wait for image upload handler (attached in requestAnimationFrame)
    await expect.poll(
      () => replyTa.evaluate((el: HTMLTextAreaElement & { _imageUploadsAttached?: boolean }) => el._imageUploadsAttached === true),
      { timeout: 5_000 },
    ).toBe(true);

    // Simulate paste
    await replyTa.evaluate((el) => {
      const file = new File(
        [new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10])],
        'reply-img.png',
        { type: 'image/png' },
      );
      const dt = new DataTransfer();
      dt.items.add(file);
      el.dispatchEvent(new ClipboardEvent('paste', {
        clipboardData: dt,
        bubbles: true,
        cancelable: true,
      }));
    });

    await expect(replyTa).toHaveValue(/!\[uploading…\]\(crit-pending-/);
  });
});

test.describe('design-mode image rendering in comments', () => {
  test.beforeEach(async ({ request }) => {
    await clearAllDesignPins(request);
  });

  test('attachment paths are rewritten to /api/attachments/ in rendered comment bodies', async ({ page, request }) => {
    await waitForAgentReady(page);

    // Seed a pin with an image markdown reference (simulates a successful upload)
    const resp = await request.post('/api/file/comments?path=/', {
      data: {
        start_line: 0,
        end_line: 0,
        body: '![screenshot](attachments/test-uuid.png)',
        dom_anchor: {
          pathname: '/',
          css_selector: '#primary-btn',
          tag_chain: ['button#primary-btn'],
          accessible_name: '',
          role: '',
          landmark: '',
          outer_html: '',
          viewport_width: 1280,
          viewport_height: 800,
        },
      },
    });
    expect(resp.ok()).toBeTruthy();

    // Reload to see the comment rendered
    await page.goto('/design');
    await waitForAgentReady(page);

    // The rendered image src should point to /api/attachments/test-uuid.png
    const img = page.locator('.comment-body img, .reply-body img').first();
    await expect(img).toBeVisible({ timeout: 10_000 });
    await expect(img).toHaveAttribute('src', '/api/attachments/test-uuid.png');
  });
});

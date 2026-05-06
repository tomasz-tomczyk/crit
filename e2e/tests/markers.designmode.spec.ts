import { test } from '@playwright/test';

test.skip(true, 'phase F design-mode runner brings these online');

test.describe('design-mode markers', () => {
  test('renders one marker per pin on current pathname', async () => { /* Task 6 */ });
  test('marker positions update when DOM mutates', async () => { /* Task 16 */ });
  test('200-mutation budget triggers full re-resolve', async () => { /* Task 15 */ });
  test('marker click posts pin-clicked', async () => { /* Task 19 */ });
  test('drifted-recoverable shows in tray with re-anchor button', async () => { /* Task 24 */ });
  test('drifted shows in tray without re-anchor button', async () => { /* Task 24 */ });
  test('re-anchor flow updates pin via PUT', async () => { /* Task 32 */ });
});

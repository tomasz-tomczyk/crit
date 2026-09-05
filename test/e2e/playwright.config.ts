import { defineConfig } from '@playwright/test';
import os from 'os';

const isWindows = os.platform() === 'win32';
const GIT_PORT = process.env.CRIT_TEST_PORT || '3123';
const FILE_PORT = process.env.CRIT_TEST_FILE_PORT || '3124';
const SINGLE_PORT = process.env.CRIT_TEST_SINGLE_PORT || '3125';
const NOGIT_PORT = process.env.CRIT_TEST_NOGIT_PORT || '3126';
const MULTI_PORT = process.env.CRIT_TEST_MULTI_PORT || '3127';
const RANGE_PORT = process.env.CRIT_TEST_RANGE_PORT || '3128';
const LIVE_PORT = process.env.CRIT_TEST_LIVE_PORT || '3129';
const SHARE_PORT = process.env.CRIT_TEST_SHARE_PORT || '3132';
// Stub crit-web backends for the share-transport project. Specs talk to them
// directly for seeding and assertions, so they need the ports too.
const STUB_PORT = process.env.CRIT_TEST_STUB_PORT || '3133';
const STUB2_PORT = process.env.CRIT_TEST_STUB2_PORT || '3135';
// Large-review perf fixture (300 files / ~9k changed lines + big markdown).
const PERF_PORT = process.env.CRIT_TEST_PERF_PORT || '3134';
// Mobile project re-uses the git-mode fixture — no separate server needed.
const MOBILE_PORT = GIT_PORT;
const debug = !!process.env.E2E_DEBUG;

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: process.env.CI
    ? [['github'], ['html', { open: 'never' }], ['list']]
    : [['html', { open: 'never' }], ['list']],

  // Per-test timeout: 60s on CI (some tests write review files and wait for
  // SSE events); 30s locally is fine since slow tests won't hit it in practice.
  timeout: process.env.CI ? 60_000 : 30_000,

  // Hard ceiling so a single hung test can't stall the entire job.
  globalTimeout: process.env.CI ? 20 * 60 * 1000 : undefined,

  use: {
    screenshot: 'only-on-failure',
    trace: debug ? 'retain-on-failure' : 'off',
    video: debug ? 'retain-on-failure' : 'off',
  },

  expect: {
    timeout: 10_000,
  },

  projects: [
    {
      name: 'git-mode',
      testMatch: /^(?!.*\.(filemode|singlefile|multifile|nogit|rangemode|mobile|livemode|sharetransport|perf)\.).*\.spec\.ts$/,
      use: {
        browserName: 'chromium',
        baseURL: `http://localhost:${GIT_PORT}`,
      },
    },
    // Touch emulation is a Chromium feature identical across OS — Linux
    // coverage is sufficient, and Windows headless has reliability issues
    // with touchscreen.tap() that cause 60s-per-test timeouts.
    ...(!isWindows ? [{
      name: 'mobile' as const,
      testMatch: /\.mobile\.spec\.ts$/,
      use: {
        browserName: 'chromium' as const,
        baseURL: `http://localhost:${MOBILE_PORT}`,
        viewport: { width: 375, height: 812 },
        hasTouch: true,
      },
    }] : []),
    {
      name: 'file-mode',
      testMatch: /\.filemode\.spec\.ts$/,
      use: {
        browserName: 'chromium',
        baseURL: `http://localhost:${FILE_PORT}`,
      },
    },
    {
      name: 'single-file-mode',
      testMatch: /\.singlefile\.spec\.ts$/,
      use: {
        browserName: 'chromium',
        baseURL: `http://localhost:${SINGLE_PORT}`,
      },
    },
    {
      // Runs targeted no-git tests against a fixture with NO git repo.
      // Verifies that file-mode works identically outside any git repository.
      name: 'no-git-mode',
      testMatch: /\.nogit\.spec\.ts$/,
      use: {
        browserName: 'chromium',
        baseURL: `http://localhost:${NOGIT_PORT}`,
      },
    },
    {
      name: 'multi-file-mode',
      testMatch: /\.multifile\.spec\.ts$/,
      use: {
        browserName: 'chromium',
        baseURL: `http://localhost:${MULTI_PORT}`,
      },
    },
    {
      name: 'range-mode',
      testMatch: /\.rangemode\.spec\.ts$/,
      use: {
        browserName: 'chromium',
        baseURL: `http://localhost:${RANGE_PORT}`,
      },
    },
    {
      name: 'live-mode',
      testMatch: /\.livemode\.spec\.ts$/,
      use: {
        browserName: 'chromium',
        // NOTE: must be `localhost` (not 127.0.0.1) — the proxy injects the
        // agent <script src="http://localhost:..."> tag, which the agent then
        // uses as its postMessage targetOrigin. If the chrome page is loaded
        // via 127.0.0.1, agent-ready postMessages get dropped (origin mismatch).
        baseURL: `http://localhost:${LIVE_PORT}/live`,
      },
    },
    {
      // Drives the browser Share round trip against a stub crit-web. baseURL
      // must be `localhost` while the stub binds `127.0.0.1` — the two are
      // distinct origins to the browser, which is what lets the spec detect a
      // regression back to cross-origin fetches.
      name: 'share-transport',
      testMatch: /\.sharetransport\.spec\.ts$/,
      use: {
        browserName: 'chromium',
        baseURL: `http://localhost:${SHARE_PORT}`,
      },
    },
    {
      // Perf guardrails against a 300-file / ~9k-line review. Asserts
      // structural budgets (mounted bodies, DOM nodes, longtask TBT), not
      // wall-clock time, so it stays green on slow runners.
      name: 'perf',
      testMatch: /\.perf\.spec\.ts$/,
      use: {
        browserName: 'chromium',
        baseURL: `http://localhost:${PERF_PORT}`,
      },
    },
  ],

  webServer: [
    {
      command: `bash setup-fixtures.sh ${GIT_PORT}`,
      url: `http://localhost:${GIT_PORT}/api/session`,
      reuseExistingServer: true,
      timeout: 30_000,
      stdout: 'pipe',
    },
    {
      command: `bash setup-fixtures-filemode.sh ${FILE_PORT}`,
      url: `http://localhost:${FILE_PORT}/api/session`,
      reuseExistingServer: true,
      timeout: 30_000,
      stdout: 'pipe',
    },
    {
      command: `bash setup-fixtures-singlefile.sh ${SINGLE_PORT}`,
      url: `http://localhost:${SINGLE_PORT}/api/session`,
      reuseExistingServer: true,
      timeout: 30_000,
      stdout: 'pipe',
    },
    {
      command: `bash setup-fixtures-nogit.sh ${NOGIT_PORT}`,
      url: `http://localhost:${NOGIT_PORT}/api/session`,
      reuseExistingServer: true,
      timeout: 30_000,
      stdout: 'pipe',
    },
    {
      command: `bash setup-fixtures-multifile.sh ${MULTI_PORT}`,
      url: `http://localhost:${MULTI_PORT}/api/session`,
      reuseExistingServer: true,
      timeout: 30_000,
      stdout: 'pipe',
    },
    {
      command: `bash setup-fixtures-range-mode.sh ${RANGE_PORT}`,
      url: `http://localhost:${RANGE_PORT}/api/session`,
      reuseExistingServer: true,
      timeout: 30_000,
      stdout: 'pipe',
    },
    {
      command: `bash setup-fixtures-livemode.sh ${LIVE_PORT}`,
      url: `http://127.0.0.1:${LIVE_PORT}/api/session`,
      reuseExistingServer: true,
      timeout: 60_000,
      stdout: 'pipe',
    },
    {
      command: `bash setup-fixtures-sharetransport.sh ${SHARE_PORT} ${STUB_PORT} ${STUB2_PORT}`,
      url: `http://localhost:${SHARE_PORT}/api/session`,
      reuseExistingServer: true,
      timeout: 60_000,
      stdout: 'pipe',
    },
    {
      command: `bash setup-fixtures-perf.sh ${PERF_PORT}`,
      url: `http://localhost:${PERF_PORT}/api/session`,
      reuseExistingServer: true,
      // Fixture generates 300 files (+ optional go build when CRIT_BIN is
      // unset, e.g. cold local runs outside run.sh which prebuilds).
      timeout: 120_000,
      stdout: 'pipe',
    },
  ],
});

import { defineConfig } from '@playwright/test';

const GIT_PORT = process.env.CRIT_TEST_PORT || '3123';
const FILE_PORT = process.env.CRIT_TEST_FILE_PORT || '3124';
const SINGLE_PORT = process.env.CRIT_TEST_SINGLE_PORT || '3125';
const debug = !!process.env.E2E_DEBUG;

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  retries: 0,
  workers: 1,
  reporter: [['html', { open: 'never' }], ['list']],

  use: {
    screenshot: 'only-on-failure',
    trace: debug ? 'retain-on-failure' : 'off',
    video: debug ? 'retain-on-failure' : 'off',
  },

  projects: [
    {
      name: 'git-mode',
      testMatch: /^(?!.*\.(filemode|singlefile)\.).*\.spec\.ts$/,
      use: {
        browserName: 'chromium',
        baseURL: `http://localhost:${GIT_PORT}`,
      },
    },
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
  ],
});

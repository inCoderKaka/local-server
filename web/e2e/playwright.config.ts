import { defineConfig, devices } from '@playwright/test';

/**
 * The browser end-to-end test of the admin UI (req_001 D-152). The server is the Go harness
 * TestE2EServer (cmd/local-server/e2e_harness_test.go): the real `serve --engine fake` with fakes
 * for the keychain, terminal, sleep lock and Tailscale. The fixture in tests/harness.ts starts it
 * and gives its admin URL as baseURL. Run it with `make web-admin-e2e`.
 */

/** Longest one test may run, harness start and shutdown included. */
const TEST_TIMEOUT_MS = 120_000;
/** Longest one auto-retrying expect waits. */
const EXPECT_TIMEOUT_MS = 10_000;
/** Longest one click, fill or navigation waits. */
const ACTION_TIMEOUT_MS = 10_000;

export default defineConfig({
  testDir: './tests',
  // One worker, files and tests one after the other: the tests share one host.
  workers: 1,
  fullyParallel: false,
  retries: 0,
  forbidOnly: !!process.env.CI,
  timeout: TEST_TIMEOUT_MS,
  expect: { timeout: EXPECT_TIMEOUT_MS },
  // Every output stays in this folder (all gitignored).
  outputDir: './test-results',
  reporter: [['list'], ['html', { outputFolder: './playwright-report', open: 'never' }]],
  use: {
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'off',
    actionTimeout: ACTION_TIMEOUT_MS,
    navigationTimeout: ACTION_TIMEOUT_MS,
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});

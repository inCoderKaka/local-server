import { randomBytes } from 'node:crypto';
import type { Page } from '@playwright/test';
import { expect, test } from './harness';

/**
 * Finish-line statements 2 and 5 in a real browser, with no curl (req_001 D-141, D-152): the host
 * sets up the host, sees its resources on every page, sets the share, creates a consumer, a grant
 * with model and context limits and an access key, all in the admin UI; then sees that
 * consumer's usage now and over time once the consumer has called the host with the key.
 */

/** Longest the usage page may take to show the calls: a few rollup passes of the harness
 * (usage.rollup.interval is lowered to 1s there) plus a refresh. */
const USAGE_TIMEOUT_MS = 30_000;
/** How long to wait between two refreshes of the usage page while the rollup catches up. */
const USAGE_POLL_MS = [500, 1_000, 2_000];
/** How long one consumer API call may take. */
const CALL_TIMEOUT_MS = 15_000;

/** The fake engine's chat model. */
const MODEL = 'fake-chat';
/** The grant's limits for it. */
const CONTEXT_LIMIT = '4096';
const OUTPUT_LIMIT = '64';
/** The memory budget the host sets: 8 GiB. */
const MEMORY_BUDGET_BYTES = String(8 * 1024 ** 3);
const MEMORY_BUDGET_SHOWN = '8.0 GiB';
/** How many chat calls the consumer makes. */
const CALLS = 2;
/** The fixed prompt sent to the fake engine. It and the answers are never logged or asserted on. */
const PROMPT = 'say hi';

const CONSUMER_NAME = 'E2E client app';

/** The resource strip in the header of every page (req_001 D-31). */
async function expectResourceStrip(page: Page) {
  const strip = page.getByRole('region', { name: 'Host resources' });
  for (const part of ['Host total', 'Share-budget slice', 'Other apps', 'Unattributed']) {
    await expect(strip.getByRole('term').filter({ hasText: new RegExp(`^${part}$`) })).toBeVisible();
  }
}

async function openPage(page: Page, nav: string, heading: string) {
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: nav, exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: heading })).toBeVisible();
  await expectResourceStrip(page);
}

test('the host sets up sharing in the browser and sees usage per consumer', async ({ page, server }) => {
  // Kept in memory only: the key is shown once and never written anywhere.
  let accessKey = '';
  let consumerPath = '';

  await test.step('first-run setup creates the admin and logs in', async () => {
    await page.goto('/');
    const form = page.getByRole('form', { name: 'Create the first admin' });
    const password = randomBytes(18).toString('base64url');
    await form.getByLabel('Login name').fill('host-admin');
    await form.getByLabel('Password', { exact: true }).fill(password);
    await form.getByLabel('Repeat the password').fill(password);
    await form.getByRole('button', { name: 'Create admin' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible();
    await expectResourceStrip(page);
  });

  await test.step('the dashboard shows host use now and over time', async () => {
    const now = page.getByRole('region', { name: 'Host use now' });
    for (const part of ['Host total', 'Share-budget slice', 'Other apps']) {
      await expect(now.getByRole('rowheader', { name: part, exact: true })).toBeVisible();
    }
    await expect(page.getByRole('region', { name: 'Host use over time' })).toBeVisible();
  });

  await test.step('pausing and resuming the share', async () => {
    const sharing = page.getByRole('region', { name: 'Sharing' });
    const toggle = sharing.getByRole('switch', { name: 'Share this host with consumers' });
    await expect(toggle).toBeChecked();
    await toggle.click();
    await expect(toggle).not.toBeChecked();
    await expect(sharing.getByText('State: Paused')).toBeVisible();
    await toggle.click();
    await expect(toggle).toBeChecked();
    await expect(sharing.getByText('State: Sharing')).toBeVisible();
  });

  await test.step('setting the share budget (memory budget)', async () => {
    await openPage(page, 'Settings', 'Settings');
    const key = 'scheduler.memory_budget_bytes';
    await page.getByLabel(`New value for ${key}`).fill(MEMORY_BUDGET_BYTES);
    await page.getByRole('button', { name: `Save ${key}` }).click();
    const strip = page.getByRole('region', { name: 'Host resources' });
    await expect(strip.getByRole('definition').filter({ hasText: MEMORY_BUDGET_SHOWN })).toBeVisible();
  });

  await test.step('sharing the model', async () => {
    await openPage(page, 'Models', 'Models');
    const share = page.getByRole('checkbox', { name: `Share ${MODEL}` });
    // A new host does not share a model it sees for the first time (admin.model_default_allowed).
    // The box follows the API's answer, so it is clicked, then awaited.
    await expect(share).not.toBeChecked();
    await share.click();
    await expect(share).toBeChecked();
  });

  await test.step('creating a consumer', async () => {
    await openPage(page, 'Consumers', 'Consumers');
    const form = page.getByRole('form', { name: 'New key-only consumer' });
    await form.getByLabel('Kind').selectOption({ label: 'Application' });
    await form.getByLabel('Display name').fill(CONSUMER_NAME);
    await form.getByRole('button', { name: 'Create consumer' }).click();
    await expect(page.getByRole('heading', { level: 1, name: CONSUMER_NAME })).toBeVisible();
    await expectResourceStrip(page);
    consumerPath = new URL(page.url()).hash;
    expect(consumerPath).toMatch(/^#\/consumers\/[^/]+$/);
  });

  await test.step('creating a grant with model and context limits', async () => {
    await page.getByRole('link', { name: /^Grant and rules/ }).click();
    const form = page.getByRole('form', { name: 'Set the grant' });
    await form.getByRole('checkbox', { name: `Allow ${MODEL}` }).check();
    await form.getByLabel(`Context limit for ${MODEL} (tokens)`).fill(CONTEXT_LIMIT);
    await form.getByLabel(`Most output tokens per call for ${MODEL}`).fill(OUTPUT_LIMIT);
    await form.getByRole('button', { name: 'Save grant' }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Saved as version 1.' })).toBeVisible();
    await expectResourceStrip(page);
    const current = page.getByRole('region', { name: 'Current grant' });
    await expect(current.getByText(MODEL).first()).toBeVisible();
    await expect(current.getByText(CONTEXT_LIMIT).first()).toBeVisible();
  });

  await test.step('issuing an access key, shown once', async () => {
    await page.goto(`/${consumerPath}`);
    await expect(page.getByRole('heading', { level: 1, name: CONSUMER_NAME })).toBeVisible();
    const form = page.getByRole('form', { name: 'Issue an access key' });
    await form.getByLabel('Key name (optional)').fill('e2e key');
    await form.getByRole('button', { name: 'Issue key' }).click();
    const panel = page.getByRole('region', { name: 'New access key' });
    accessKey = await panel.getByLabel('Access key secret').inputValue();
    expect(accessKey.length).toBeGreaterThan(0);
    await panel.getByRole('button', { name: 'I have copied it' }).click();
    await expect(panel).toBeHidden();
    await expect(page.getByRole('region', { name: 'Access keys' }).getByText('e2e key')).toBeVisible();
  });

  await test.step('the consumer calls the host with the key (consumer API, not the admin UI)', async () => {
    // Node's fetch, not Playwright's request API, so neither the key nor the prompt is in a trace.
    for (let i = 0; i < CALLS; i++) {
      const res = await fetch(`${server.consumer}/v1/chat/completions`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${accessKey}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ model: MODEL, messages: [{ role: 'user', content: PROMPT }] }),
        signal: AbortSignal.timeout(CALL_TIMEOUT_MS),
      });
      const body = (await res.json()) as { usage?: { total_tokens?: number } };
      expect(res.status, `chat call ${i + 1}`).toBe(200);
      expect(body.usage?.total_tokens ?? 0).toBeGreaterThan(0);
    }
  });

  await test.step('the usage page shows the consumer: totals now and usage over time', async () => {
    await openPage(page, 'Usage', 'Usage');
    const totals = page.getByRole('region', { name: 'Totals per consumer' });
    const row = totals.getByRole('row').filter({ has: page.getByRole('link', { name: CONSUMER_NAME }) });
    // The rollup runs every second in the harness; refresh until the calls are in.
    await expect(async () => {
      await page.getByRole('region', { name: 'What to show' }).getByRole('button', { name: 'Refresh' }).click();
      await expect(row.getByRole('cell').nth(1)).toHaveText(String(CALLS), { timeout: USAGE_POLL_MS[0] });
    }).toPass({ timeout: USAGE_TIMEOUT_MS, intervals: USAGE_POLL_MS });
    await expect(row.getByRole('cell').nth(4)).not.toHaveText('0');

    const overTime = page.getByRole('region', { name: 'Usage over time' });
    await expect(overTime.getByRole('img', { name: 'Calls over time' })).toBeVisible();
    await expect(overTime.getByRole('img', { name: 'Tokens over time' })).toBeVisible();
    await overTime.getByText('Show the data as a table').click();
    await expect(overTime.getByRole('row').filter({ has: page.getByRole('link', { name: CONSUMER_NAME }) }).first()).toBeVisible();

    // Only this consumer.
    await page.getByRole('region', { name: 'What to show' }).getByLabel('Consumer').selectOption({ label: CONSUMER_NAME });
    await expect(row.getByRole('cell').nth(1)).toHaveText(String(CALLS));
    await expect(totals.getByRole('row')).toHaveCount(2); // the header and this consumer
  });

  await test.step('the dashboard after the calls', async () => {
    await openPage(page, 'Dashboard', 'Dashboard');
    // The dashboard shows host use only (Host use now, Host use over time), not usage per consumer.
    await expect(page.getByRole('region', { name: 'Host use now' })).toBeVisible();
  });
});

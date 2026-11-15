import { randomBytes } from 'node:crypto';
import type { Page } from '@playwright/test';
import { expect, test } from './harness';

/**
 * Finish-line statements 2 and 5 in a real browser, with no curl (req_001 D-141, D-152): the host
 * sets up the host, sees its resources on every page, sets the share, creates a consumer, a grant
 * with model and context limits and an access key, all in the admin UI; then sees that
 * consumer's usage now and over time once the consumer has called the host with the key.
 * Then the resource requests flow (req_001 D-39, D-160): the consumer asks for more with its key,
 * the host sees the request and the pending count, approves it with limits (a new grant version),
 * and rejects a second one with a comment.
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
const ADMIN_LOGIN = 'host-admin';

/** The context limit the first resource request asks for (the fake chat model's whole context). */
const ASKED_CONTEXT_LIMIT = '8192';
/** The concurrency the host sets when it approves the first request. */
const APPROVED_CONCURRENCY = '2';
/** The concurrency the second resource request asks for. */
const REJECTED_CONCURRENCY = 4;
/** The consumer's messages and the host's reason; plain request text, not prompts. */
const APPROVE_MESSAGE = 'E2E request: a longer context, please.';
const REJECT_MESSAGE = 'E2E request: a second one, to be rejected.';
const REJECT_REASON = 'E2E reason: not this time.';

// The tests share one host (one worker, see playwright.config.ts) and run in order: the second
// starts from the consumer, grant and key the first set up, so it is skipped when the first fails.
test.describe.configure({ mode: 'serial' });

/** Set by the first test, read by the next. Kept in memory only: the admin password and the
 * access key (shown once) are never written anywhere. */
const shared = { password: '', accessKey: '', consumerPath: '' };

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
  await test.step('first-run setup creates the admin and logs in', async () => {
    await page.goto('/');
    const form = page.getByRole('form', { name: 'Create the first admin' });
    shared.password = randomBytes(18).toString('base64url');
    await form.getByLabel('Login name').fill(ADMIN_LOGIN);
    await form.getByLabel('Password', { exact: true }).fill(shared.password);
    await form.getByLabel('Repeat the password').fill(shared.password);
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
    shared.consumerPath = new URL(page.url()).hash;
    expect(shared.consumerPath).toMatch(/^#\/consumers\/[^/]+$/);
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
    await page.goto(`/${shared.consumerPath}`);
    await expect(page.getByRole('heading', { level: 1, name: CONSUMER_NAME })).toBeVisible();
    const form = page.getByRole('form', { name: 'Issue an access key' });
    await form.getByLabel('Key name (optional)').fill('e2e key');
    await form.getByRole('button', { name: 'Issue key' }).click();
    const panel = page.getByRole('region', { name: 'New access key' });
    shared.accessKey = await panel.getByLabel('Access key secret').inputValue();
    expect(shared.accessKey.length).toBeGreaterThan(0);
    await panel.getByRole('button', { name: 'I have copied it' }).click();
    await expect(panel).toBeHidden();
    await expect(page.getByRole('region', { name: 'Access keys' }).getByText('e2e key')).toBeVisible();
  });

  await test.step('the consumer calls the host with the key (consumer API, not the admin UI)', async () => {
    // Node's fetch, not Playwright's request API, so neither the key nor the prompt is in a trace.
    for (let i = 0; i < CALLS; i++) {
      const res = await fetch(`${server.consumer}/v1/chat/completions`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${shared.accessKey}`, 'Content-Type': 'application/json' },
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

/** The consumer opens a resource request with its access key, on the consumer API. Node's fetch,
 * not Playwright's request API, so the key is not in a trace. */
async function openRequest(server: { consumer: string }, message: string, ask: Record<string, unknown>) {
  const res = await fetch(`${server.consumer}/v1/resource-requests`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${shared.accessKey}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ message, ask }),
    signal: AbortSignal.timeout(CALL_TIMEOUT_MS),
  });
  const body = (await res.json()) as { status?: string };
  expect(res.status, 'create resource request').toBe(201);
  expect(body.status).toBe('open');
}

/** Opens the request of the consumer from the list on the requests page. */
async function showRequest(page: Page) {
  await page
    .getByRole('table', { name: 'Resource requests' })
    .getByRole('button', { name: new RegExp(`^Show the request from ${CONSUMER_NAME}`) })
    .click();
  return page.getByRole('region', { name: 'Request', exact: true });
}

test('the host approves one resource request with limits and rejects another with a comment', async ({ page, server }) => {
  expect(shared.accessKey, 'the first test issued the key').not.toBe('');
  const pendingBadge = (n: number) =>
    page.getByRole('banner').getByRole('link', { name: `${n} pending resource ${n === 1 ? 'request' : 'requests'}` });

  await test.step('the consumer asks for more with its key (consumer API)', async () => {
    await openRequest(server, APPROVE_MESSAGE, { models: [MODEL], context_limit: Number(ASKED_CONTEXT_LIMIT) });
  });

  await test.step('the host logs in and sees the request and the pending count', async () => {
    await page.goto('/');
    const form = page.getByRole('form', { name: 'Log in' });
    await form.getByLabel('Login name').fill(ADMIN_LOGIN);
    await form.getByLabel('Password', { exact: true }).fill(shared.password);
    await form.getByRole('button', { name: 'Log in' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible();
    await expect(pendingBadge(1)).toBeVisible();
    await openPage(page, 'Requests', 'Requests');
    await expect(page.getByText('1 pending', { exact: true })).toBeVisible();
    const rows = page.getByRole('table', { name: 'Resource requests' }).getByRole('row');
    await expect(rows).toHaveCount(2); // the header and the request
    await expect(rows.nth(1).getByRole('link', { name: CONSUMER_NAME })).toBeVisible();
    await expect(rows.nth(1).getByText('Pending', { exact: true })).toBeVisible();
    await expect(rows.nth(1).getByText(`Context limit: ${ASKED_CONTEXT_LIMIT} tokens`)).toBeVisible();
  });

  await test.step('the host approves it with limits: a new grant version', async () => {
    const card = await showRequest(page);
    await expect(card.getByText(APPROVE_MESSAGE)).toBeVisible();
    const form = card.getByRole('form', { name: 'Approve the request' });
    // The form starts from the current grant with the ask applied; the host changes a limit too.
    await expect(form.getByLabel(`Context limit for ${MODEL} (tokens)`)).toHaveValue(ASKED_CONTEXT_LIMIT);
    await form.getByLabel('Requests at once').fill(APPROVED_CONCURRENCY);
    await form.getByRole('button', { name: 'Approve', exact: true }).click();
    await expect(card.getByRole('status').filter({ hasText: 'Approved: the consumer has a new grant version.' })).toBeVisible();
    await expect(card.getByText('Approved', { exact: true })).toBeVisible();
    await expect(page.getByText('0 pending', { exact: true })).toBeVisible();
    await expect(pendingBadge(0)).toBeVisible();
    // The request names the grant version its approval made.
    await expect(card.getByRole('term').filter({ hasText: /^Grant version$/ })).toBeVisible();

    await card.getByRole('link', { name: 'Grant', exact: true }).click();
    await expect(page.getByRole('heading', { level: 1, name: `Grant of ${CONSUMER_NAME}` })).toBeVisible();
    const history = page.getByRole('table', { name: 'Grant versions, newest first' });
    await expect(history.getByRole('row')).toHaveCount(3); // the header, version 2 and version 1
    // Version 2 carries the approved limits. Its "Made by" cell is not asserted: a read leaves
    // resource_request_id null, so it shows "The host" (phase-2-parking.md, build PLT-F-028).
    const newest = history.getByRole('row').nth(1);
    await expect(newest.getByRole('cell').nth(0)).toHaveText('2');
    await expect(newest.getByRole('cell').nth(3)).toContainText(`context ${ASKED_CONTEXT_LIMIT}`);
    await expect(newest.getByRole('cell').nth(5)).toHaveText(APPROVED_CONCURRENCY);
  });

  await test.step('a second request is rejected with a comment', async () => {
    await openRequest(server, REJECT_MESSAGE, { concurrency: REJECTED_CONCURRENCY });
    await openPage(page, 'Requests', 'Requests');
    // The shell polls the count; Refresh reads the list and the count now.
    await page.getByRole('button', { name: 'Refresh', exact: true }).click();
    await expect(page.getByText('1 pending', { exact: true })).toBeVisible();
    await expect(pendingBadge(1)).toBeVisible();

    const card = await showRequest(page);
    await expect(card.getByText(REJECT_MESSAGE)).toBeVisible();
    await card.getByLabel('Reason for the consumer (optional)').fill(REJECT_REASON);
    await card.getByRole('button', { name: 'Reject', exact: true }).click();
    await card.getByRole('group', { name: 'Reject this request?' }).getByRole('button', { name: 'Reject the request' }).click();
    await expect(card.getByRole('status').filter({ hasText: 'The request is now rejected.' })).toBeVisible();
    await expect(card.getByText('Rejected', { exact: true })).toBeVisible();
    await expect(card.getByRole('list', { name: 'Comments, oldest first' }).getByText(REJECT_REASON)).toBeVisible();
    await expect(pendingBadge(0)).toBeVisible();

    // The rejected filter lists it.
    await page.getByLabel('Show', { exact: true }).selectOption({ label: 'Rejected' });
    const rows = page.getByRole('table', { name: 'Resource requests' }).getByRole('row');
    await expect(rows).toHaveCount(2);
    await expect(rows.nth(1).getByText('Rejected', { exact: true })).toBeVisible();
  });
});

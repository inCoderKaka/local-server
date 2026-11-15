import { randomBytes } from 'node:crypto';
import type { Browser, BrowserContext, Page } from '@playwright/test';
import { expect, test, type Server } from './harness';

/**
 * Finish-line statement 4 in a real browser (req_001 D-141, D-152): the host invites a consumer in
 * the admin UI; the consumer signs up from the invite link on the consumer web page with a
 * password and the consent, logs in, sees limits, usage and keys, and files a resource request
 * with a comment; the host approves it in the admin UI; the consumer page then shows the approved
 * request, the host's note and the new limit. The admin UI and the consumer page run in two
 * browser contexts, as two people on two machines would. The consumer page listener serves TLS
 * only, with the harness's self-signed certificate (tests/harness.ts).
 */

/** Longest the usage page may take to show the calls: a few rollup passes of the harness
 * (usage.rollup.interval is lowered to 1s there) plus a refresh. */
const USAGE_TIMEOUT_MS = 30_000;
/** How long to wait between two refreshes of the usage page while the rollup catches up. */
const USAGE_POLL_MS = [500, 1_000, 2_000];
/** How long one consumer API call may take. */
const CALL_TIMEOUT_MS = 15_000;
/** One locale for both contexts, so the numbers on the page read the same on every machine. */
const LOCALE = 'en-US';

/** The fake engine's chat model. */
const MODEL = 'fake-chat';
/** The grant's context limit, and the one the request asks for (the fake model's whole context). */
const CONTEXT_LIMIT = 4096;
const ASKED_CONTEXT_LIMIT = 8192;
/** How many chat calls the consumer's app makes with its key. */
const CALLS = 2;
/** The fixed prompt sent to the fake engine. It and the answers are never logged or asserted on. */
const PROMPT = 'say hi';

const ADMIN_LOGIN = 'host-admin';
const CONSUMER_NAME = 'E2E consumer person';
const CONSUMER_LOGIN = 'e2e-consumer';
const KEY_NAME = 'e2e consumer key';
/** Request text, not prompts. */
const REQUEST_MESSAGE = 'E2E request: a longer context, please.';
const REQUEST_COMMENT = 'E2E comment: it is for long documents.';
const APPROVAL_NOTE = 'E2E note: approved for a month.';

const fmt = (n: number) => n.toLocaleString(LOCALE);

async function newContext(browser: Browser, baseURL: string, https: boolean): Promise<BrowserContext> {
  return browser.newContext({ baseURL, locale: LOCALE, ignoreHTTPSErrors: https });
}

/** Opens a page of the admin UI from its navigation. */
async function adminOpen(admin: Page, nav: string, heading: string) {
  await admin.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: nav, exact: true }).click();
  await expect(admin.getByRole('heading', { level: 1, name: heading })).toBeVisible();
}

/** Opens a page of the consumer web page from its navigation. */
async function consumerOpen(me: Page, nav: string, heading = nav) {
  await me.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: nav, exact: true }).click();
  await expect(me.getByRole('heading', { level: 1, name: heading })).toBeVisible();
}

/** The consumer's app calls the host with its key on the consumer API. Node's fetch, not
 * Playwright's request API, so neither the key nor the prompt is in a trace. */
async function chat(server: Server, key: string) {
  for (let i = 0; i < CALLS; i++) {
    const res = await fetch(`${server.consumer}/v1/chat/completions`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${key}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ model: MODEL, messages: [{ role: 'user', content: PROMPT }] }),
      signal: AbortSignal.timeout(CALL_TIMEOUT_MS),
    });
    await res.json();
    expect(res.status, `chat call ${i + 1}`).toBe(200);
  }
}

test('a consumer signs up from an invite, asks for more and sees the host approve it', async ({ browser, server }) => {
  const hostCtx = await newContext(browser, server.admin, false);
  const meCtx = await newContext(browser, server.page, true);
  const admin = await hostCtx.newPage();
  const me = await meCtx.newPage();
  // Kept in memory only: the passwords, the invite link and the access key are never written anywhere.
  const adminPassword = randomBytes(18).toString('base64url');
  const myPassword = randomBytes(18).toString('base64url');
  let inviteLink = '';
  let accessKey = '';

  try {
    await test.step('the host sets up the host, shares the model and creates an invite', async () => {
      await admin.goto('/');
      const setup = admin.getByRole('form', { name: 'Create the first admin' });
      await setup.getByLabel('Login name').fill(ADMIN_LOGIN);
      await setup.getByLabel('Password', { exact: true }).fill(adminPassword);
      await setup.getByLabel('Repeat the password').fill(adminPassword);
      await setup.getByRole('button', { name: 'Create admin' }).click();
      await expect(admin.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible();

      await adminOpen(admin, 'Models', 'Models');
      const share = admin.getByRole('checkbox', { name: `Share ${MODEL}` });
      await expect(share).not.toBeChecked();
      await share.click();
      await expect(share).toBeChecked();

      await adminOpen(admin, 'Invites', 'Invites');
      const form = admin.getByRole('form', { name: 'New invite' });
      await form.getByLabel('Note (optional)').fill('for the e2e consumer');
      await form.getByRole('button', { name: 'Create invite' }).click();
      const shown = admin.getByRole('region', { name: 'Invite link' });
      inviteLink = await shown.getByLabel('Invite link').inputValue();
      // invites.link_base_url is empty in the harness: the link is relative to the consumer page.
      expect(inviteLink).toMatch(/^\/invite#.+/);
      await shown.getByRole('button', { name: 'I have copied it' }).click();
    });

    await test.step('the consumer signs up from the invite link with a password and the consent', async () => {
      await me.goto(inviteLink);
      const form = me.getByRole('form', { name: 'Sign up' });
      await expect(form.getByLabel('Invite code')).not.toHaveValue('');
      // The token leaves the address at once.
      await expect(me).toHaveURL(/\/#\/signup$/);
      await form.getByLabel('Your name').fill(CONSUMER_NAME);
      await form.getByRole('textbox', { name: 'Login name' }).fill(CONSUMER_LOGIN);
      await form.getByLabel('Password', { exact: true }).fill(myPassword);
      await form.getByLabel('Repeat the password').fill(myPassword);
      // The consent names the versions the page shows; they are read from the page, not written here.
      const terms = form.locator('label', { hasText: 'accept the Terms of Use' });
      const privacy = form.locator('label', { hasText: 'accept the Privacy and consent notice' });
      const shown = { terms_version: (await terms.locator('code').textContent())!, privacy_version: (await privacy.locator('code').textContent())! };
      expect(shown.terms_version).not.toBe('');
      expect(shown.privacy_version).not.toBe('');
      await terms.click();
      await privacy.click();
      const sent = me.waitForRequest((r) => r.method() === 'POST' && new URL(r.url()).pathname === '/v1/invite-redemptions/password');
      await form.getByRole('button', { name: 'Create account' }).click();
      expect(((await sent).postDataJSON() as { consent: unknown }).consent).toEqual(shown);
      await expect(me.getByRole('heading', { level: 1, name: 'Home' })).toBeVisible();
      await expect(me.getByRole('banner').getByText(CONSUMER_NAME)).toBeVisible();
      // No grant yet (req_001 D-150).
      await expect(me.getByRole('region', { name: 'Your limits' }).getByText('The host has not given you access yet.')).toBeVisible();
    });

    await test.step('the consumer logs out and logs in with the password', async () => {
      await me.getByRole('button', { name: 'Log out' }).click();
      const form = me.getByRole('form', { name: 'Log in with a password' });
      await form.getByLabel('Login name').fill(CONSUMER_LOGIN);
      await form.getByLabel('Password').fill(myPassword);
      await form.getByRole('button', { name: 'Log in' }).click();
      await expect(me.getByRole('heading', { level: 1, name: 'Home' })).toBeVisible();
    });

    await test.step('the host gives the new consumer a grant and a key, and its app makes calls', async () => {
      // The invites page is still open from creating the invite; read it again.
      await admin.reload();
      await expect(admin.getByRole('heading', { level: 1, name: 'Invites' })).toBeVisible();
      const invites = admin.getByRole('table', { name: 'Invites, newest first' });
      await expect(invites.getByRole('row').nth(1)).toContainText('Redeemed');
      await invites.getByRole('link', { name: 'Open consumer' }).click();
      await expect(admin.getByRole('heading', { level: 1, name: CONSUMER_NAME })).toBeVisible();
      const consumerUrl = admin.url();

      await admin.getByRole('link', { name: /^Grant and rules/ }).click();
      const grant = admin.getByRole('form', { name: 'Set the grant' });
      await grant.getByRole('checkbox', { name: `Allow ${MODEL}` }).check();
      await grant.getByLabel(`Context limit for ${MODEL} (tokens)`).fill(String(CONTEXT_LIMIT));
      await grant.getByRole('button', { name: 'Save grant' }).click();
      await expect(admin.getByRole('status').filter({ hasText: 'Saved as version 1.' })).toBeVisible();

      await admin.goto(consumerUrl);
      const issue = admin.getByRole('form', { name: 'Issue an access key' });
      await issue.getByLabel('Key name (optional)').fill(KEY_NAME);
      await issue.getByRole('button', { name: 'Issue key' }).click();
      const panel = admin.getByRole('region', { name: 'New access key' });
      accessKey = await panel.getByLabel('Access key secret').inputValue();
      await panel.getByRole('button', { name: 'I have copied it' }).click();
      await chat(server, accessKey);
    });

    await test.step('the consumer sees limits, keys and usage', async () => {
      await consumerOpen(me, 'My limits');
      const models = me.getByRole('table', { name: 'Models and their limits' });
      const row = models.getByRole('row').filter({ hasText: MODEL });
      await expect(row).toContainText(`${fmt(CONTEXT_LIMIT)} tokens`);

      await consumerOpen(me, 'My keys');
      const key = me.getByRole('table', { name: 'Your access keys' }).getByRole('row').filter({ hasText: KEY_NAME });
      await expect(key).toContainText('Active');
      await expect(key).toContainText(`${accessKey.split('_').slice(0, 2).join('_')}_…`);

      await consumerOpen(me, 'My usage');
      const totals = me.getByRole('region', { name: 'Totals per model' });
      const usage = totals.getByRole('row').filter({ hasText: MODEL });
      await expect(async () => {
        await me.getByRole('region', { name: 'Used now' }).getByRole('button', { name: 'Refresh' }).click();
        await expect(usage.getByRole('cell').nth(1)).toHaveText(String(CALLS), { timeout: USAGE_POLL_MS[0] });
      }).toPass({ timeout: USAGE_TIMEOUT_MS, intervals: USAGE_POLL_MS });
      await expect(usage.getByRole('cell').nth(4)).not.toHaveText('0');
      await expect(me.getByRole('region', { name: 'Used now' })).toContainText(`${CALLS} calls`);
      const overTime = me.getByRole('region', { name: 'Usage over time' });
      await expect(overTime.getByRole('img', { name: 'Calls over time' })).toBeVisible();
      await expect(overTime.getByRole('img', { name: 'Tokens over time' })).toBeVisible();
    });

    await test.step('the consumer files a resource request and comments on it', async () => {
      await consumerOpen(me, 'Requests');
      const form = me.getByRole('form', { name: 'New resource request' });
      await form.getByLabel('What you need and why').fill(REQUEST_MESSAGE);
      await form.getByLabel('Models').fill(MODEL);
      await form.getByLabel('Context limit (tokens)').fill(String(ASKED_CONTEXT_LIMIT));
      await form.getByRole('button', { name: 'Send request' }).click();
      await expect(me.getByRole('status').filter({ hasText: 'Your request was sent.' })).toBeVisible();
      const list = me.getByRole('table', { name: 'Your resource requests' });
      await expect(list.getByRole('row')).toHaveCount(2);
      await expect(list.getByRole('row').nth(1)).toContainText('Pending');

      await me.getByRole('link', { name: 'Open the request', exact: true }).click();
      const card = me.getByRole('region', { name: 'Request details' });
      await expect(card.getByText(REQUEST_MESSAGE)).toBeVisible();
      await card.getByLabel('Your comment').fill(REQUEST_COMMENT);
      await card.getByRole('button', { name: 'Add comment' }).click();
      await expect(card.getByRole('list', { name: 'Comments, oldest first' }).getByText(REQUEST_COMMENT)).toBeVisible();
      // The start page counts it as pending.
      await consumerOpen(me, 'Home');
      await expect(me.getByRole('region', { name: 'Requests' })).toContainText('You have 1 pending request.');
    });

    await test.step('the host sees the request with the comment and approves it in the admin UI', async () => {
      await adminOpen(admin, 'Requests', 'Requests');
      await admin
        .getByRole('table', { name: 'Resource requests' })
        .getByRole('button', { name: new RegExp(`^Show the request from ${CONSUMER_NAME}`) })
        .click();
      const card = admin.getByRole('region', { name: 'Request', exact: true });
      await expect(card.getByText(REQUEST_MESSAGE)).toBeVisible();
      await expect(card.getByRole('list', { name: 'Comments, oldest first' }).getByText(REQUEST_COMMENT)).toBeVisible();
      const form = card.getByRole('form', { name: 'Approve the request' });
      await expect(form.getByLabel(`Context limit for ${MODEL} (tokens)`)).toHaveValue(String(ASKED_CONTEXT_LIMIT));
      await form.getByLabel('Comment for the consumer (optional)').fill(APPROVAL_NOTE);
      await form.getByRole('button', { name: 'Approve', exact: true }).click();
      await expect(card.getByRole('status').filter({ hasText: 'Approved: the consumer has a new grant version.' })).toBeVisible();
    });

    await test.step('the consumer page shows the approved request, the note and the new limit', async () => {
      await consumerOpen(me, 'Requests');
      const row = me.getByRole('table', { name: 'Your resource requests' }).getByRole('row').nth(1);
      await expect(row).toContainText('Approved');
      await row.getByRole('link', { name: /^Open the request sent/ }).click();
      const card = me.getByRole('region', { name: 'Request details' });
      await expect(card.getByText('Approved', { exact: true }).first()).toBeVisible();
      await expect(card.getByText(/^The host approved this request/)).toBeVisible();
      await expect(card.getByText(APPROVAL_NOTE).first()).toBeVisible();
      await expect(card.getByRole('button', { name: 'Withdraw' })).toHaveCount(0);

      await card.getByRole('link', { name: 'See your limits' }).click();
      await expect(me.getByRole('heading', { level: 1, name: 'My limits' })).toBeVisible();
      const model = me.getByRole('table', { name: 'Models and their limits' }).getByRole('row').filter({ hasText: MODEL });
      await expect(model).toContainText(`${fmt(ASKED_CONTEXT_LIMIT)} tokens`);
      await expect(me.getByRole('region', { name: 'Other terms' })).toContainText('Version 2');
    });
  } finally {
    await hostCtx.close();
    await meCtx.close();
  }
});

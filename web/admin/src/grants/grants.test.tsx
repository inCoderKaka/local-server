import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import { pages } from '../registry';
import { accessKey, CID, consumer } from '../consumers/testData';
import { fakeHost } from '../test/fixtures';
import { renderApp } from '../test/render';
import { GID, grantVersion, hostModel, policyRule, settingsWithDefaults } from './testData';

const XSS = '<img src=x onerror=alert(1)>';
const grantPath = `/admin/v1/consumers/${CID}/grant`;

function host() {
  const f = fakeHost();
  f.on('GET', '/admin/v1/settings', { body: { data: settingsWithDefaults() } });
  f.on('GET', '/admin/v1/consumers/{consumer_id}', { body: consumer() });
  f.on('GET', '/admin/v1/consumers/{consumer_id}/grant', { body: grantVersion() });
  f.on('GET', '/admin/v1/consumers/{consumer_id}/grant/versions', { body: { data: [grantVersion()], next_cursor: null } });
  f.on('GET', '/admin/v1/models', { body: { data: [hostModel(), hostModel({ id: 'm2', name: 'other-model' }), hostModel({ id: 'm3', name: 'closed-model', allowed: false })] } });
  f.on('GET', '/admin/v1/policy-rules', { body: { data: [] } });
  return f;
}

function noGrant(f = host()) {
  f.on('GET', '/admin/v1/consumers/{consumer_id}/grant', errorReply(404, 'not_found', 'This consumer has no grant yet.'));
  f.on('GET', '/admin/v1/consumers/{consumer_id}/grant/versions', { body: { data: [], next_cursor: null } });
  return f;
}

async function form() {
  return screen.findByRole('form', { name: 'Set the grant' });
}

describe('grant, model and rule pages', () => {
  it('are registered once each; the grant page is reached from the consumer, the others are in the navigation', async () => {
    expect(pages.find((p) => p.id === 'grant')).toMatchObject({ path: '/consumers/:consumerId/grant', navVisible: false });
    const f = host();
    f.on('GET', '/admin/v1/consumers/{consumer_id}/access-keys', { body: { data: [accessKey()] } });
    renderApp(f, { hash: `#/consumers/${CID}` });
    const nav = await screen.findByRole('navigation', { name: 'Main' });
    expect(within(nav).getByRole('link', { name: 'Models' }).getAttribute('href')).toBe('#/models');
    expect(within(nav).getByRole('link', { name: 'Rules' }).getAttribute('href')).toBe('#/policy-rules');
    expect(within(nav).queryByRole('link', { name: 'Grant' })).toBeNull();
    const link = await screen.findByRole('link', { name: /Grant and rules/ });
    expect(link.getAttribute('href')).toBe(`#/consumers/${CID}/grant`);
  });
});

describe('grant page', () => {
  it('shows the current grant, a limit the grant does not set as no limit, and names as text', async () => {
    const f = host();
    f.on('GET', '/admin/v1/consumers/{consumer_id}', { body: consumer({ display_name: XSS }) });
    f.on('GET', '/admin/v1/consumers/{consumer_id}/grant', {
      body: grantVersion({}, { models: [{ model: XSS, context_limit: null, max_output_tokens: 512 }], allowed_request_properties: ['background', XSS], expires_at: '2027-01-01T00:00:00Z' }),
    });
    renderApp(f, { hash: `#/consumers/${CID}/grant` });
    expect(await screen.findByRole('heading', { level: 1, name: `Grant of ${XSS}` })).toBeTruthy();
    const card = await screen.findByRole('region', { name: 'Current grant' });
    const models = await within(card).findByRole('table', { name: 'Models of this grant' });
    const row = within(models).getAllByRole('row')[1]!;
    expect(row.textContent).toContain(XSS);
    expect(row.textContent).toContain('No limit');
    expect(row.textContent).toContain('512');
    expect(card.textContent).toContain('Refuse the call');
    expect(card.textContent).toContain('context_too_long');
    expect(card.textContent).toContain(`background, ${XSS}`);
    expect(within(card).getByRole('table', { name: 'Quotas of this grant' }).textContent).toContain('24h');
    expect(document.querySelector('img')).toBeNull();
    const history = screen.getByRole('table', { name: 'Grant versions, newest first' });
    expect(within(history).getAllByRole('row')[1]!.textContent).toContain('output no limit');
  });

  it('says a consumer without a grant has no access, and sets its first grant with the exact body', async () => {
    const f = noGrant();
    f.on('PUT', '/admin/v1/consumers/{consumer_id}/grant', () => {
      const v = grantVersion({ warnings: ['bogus is not a known field and was ignored'] });
      f.on('GET', '/admin/v1/consumers/{consumer_id}/grant', { body: v });
      f.on('GET', '/admin/v1/consumers/{consumer_id}/grant/versions', { body: { data: [v], next_cursor: null } });
      return { body: v };
    });
    renderApp(f, { hash: `#/consumers/${CID}/grant` });
    expect(await screen.findByText(/No grant yet: this consumer cannot call any model/)).toBeTruthy();
    const fm = await form();
    // Only shared models are offered.
    expect(within(fm).queryByLabelText('Allow closed-model')).toBeNull();
    // The concurrency starts at the host's default setting; nothing else is filled in.
    expect((within(fm).getByLabelText('Requests at once') as HTMLInputElement).value).toBe('1');

    const u = userEvent.setup();
    await u.click(within(fm).getByLabelText('Allow example-model'));
    await u.type(within(fm).getByLabelText('Context limit for example-model (tokens)'), '4096');
    await u.click(within(fm).getByRole('button', { name: 'Add a quota' }));
    const quota = within(fm).getByRole('group', { name: 'Quota 1' });
    // A new quota starts with the host's quota window.
    expect((within(quota).getByLabelText('Every') as HTMLInputElement).value).toBe('24h');
    await u.selectOptions(within(quota).getByLabelText('Counts'), 'requests');
    await u.type(within(quota).getByLabelText('At most'), '500');
    await u.click(within(fm).getByLabelText(/Cut the prompt to fit/));
    await u.click(within(fm).getByLabelText('Only these'));
    await u.type(within(fm).getByLabelText('Request types this consumer may send'), 'background, batch');
    await u.click(within(fm).getByRole('button', { name: 'Save grant' }));

    const status = await screen.findByText('Saved as version 1.');
    expect(status.closest('[role=status]')!.textContent).toContain('bogus is not a known field and was ignored');
    expect(f.callsTo('PUT', grantPath)[0]!.body).toEqual({
      models: [{ model: 'example-model', context_limit: 4096, max_output_tokens: null }],
      quotas: [{ metric: 'requests', limit: 500, window_seconds: 86400 }],
      concurrency: 1,
      on_context_overflow: 'truncate',
      allowed_request_properties: ['background', 'batch'],
      expires_at: null,
    });
    const card = screen.getByRole('region', { name: 'Current grant' });
    expect(await within(card).findByRole('table', { name: 'Models of this grant' })).toBeTruthy();
    await waitFor(() => expect(f.callsTo('GET', `${grantPath}/versions`).length).toBeGreaterThanOrEqual(2));
  });

  it('starts the form from the current version, so saving it again keeps every limit, null included', async () => {
    const f = host();
    f.on('PUT', '/admin/v1/consumers/{consumer_id}/grant', { body: grantVersion({ id: 'v2', version: 2 }) });
    renderApp(f, { hash: `#/consumers/${CID}/grant` });
    const fm = await form();
    expect((within(fm).getByLabelText('Allow example-model') as HTMLInputElement).checked).toBe(true);
    expect((within(fm).getByLabelText('Most output tokens per call for example-model') as HTMLInputElement).value).toBe('');
    const u = userEvent.setup();
    await u.click(within(fm).getByLabelText('At a date and time'));
    fireEvent.change(within(fm).getByLabelText('Expiry date and time'), { target: { value: '2027-03-01T09:30' } });
    await u.click(within(fm).getByRole('button', { name: 'Save grant' }));
    expect(await screen.findByText('Saved as version 2.')).toBeTruthy();
    expect(f.callsTo('PUT', grantPath)[0]!.body).toEqual({
      models: [{ model: 'example-model', context_limit: 4096, max_output_tokens: null }],
      quotas: [{ metric: 'tokens', limit: 100000, window_seconds: 86400 }],
      concurrency: 2,
      on_context_overflow: 'refuse',
      allowed_request_properties: null,
      expires_at: new Date('2027-03-01T09:30').toISOString(),
    });
  });

  it('checks the form before sending', async () => {
    const f = noGrant();
    renderApp(f, { hash: `#/consumers/${CID}/grant` });
    const fm = await form();
    const u = userEvent.setup();
    await u.clear(within(fm).getByLabelText('Requests at once'));
    await u.click(within(fm).getByLabelText('Allow example-model'));
    await u.type(within(fm).getByLabelText('Context limit for example-model (tokens)'), '0');
    await u.click(within(fm).getByRole('button', { name: 'Add a quota' }));
    const quota = within(fm).getByRole('group', { name: 'Quota 1' });
    await u.clear(within(quota).getByLabelText('Every'));
    await u.type(within(quota).getByLabelText('Every'), 'daily');
    await u.click(within(fm).getByRole('button', { name: 'Save grant' }));
    expect(within(fm).getByLabelText('Requests at once').getAttribute('aria-invalid')).toBe('true');
    expect(within(fm).getByLabelText('Context limit for example-model (tokens)').getAttribute('aria-invalid')).toBe('true');
    expect(within(quota).getByLabelText('At most').getAttribute('aria-invalid')).toBe('true');
    expect(within(quota).getByText(/Write a duration such as/)).toBeTruthy();
    expect(f.callsTo('PUT', grantPath)).toHaveLength(0);
  });

  it('shows a refused field at the field it names', async () => {
    const f = host();
    f.on('PUT', '/admin/v1/consumers/{consumer_id}/grant', errorReply(400, null, 'The host has not opened this model for sharing.', { param: 'limits.models.0.model' }));
    renderApp(f, { hash: `#/consumers/${CID}/grant` });
    const fm = await form();
    const u = userEvent.setup();
    await u.click(within(fm).getByLabelText('Allow other-model'));
    await u.click(within(fm).getByRole('button', { name: 'Save grant' }));
    expect((await within(fm).findByRole('alert')).textContent).toContain('The grant was not saved');
    // Index 0 of the request is example-model, the first in name order.
    const sent = f.callsTo('PUT', grantPath)[0]!.body as { models: { model: string }[] };
    expect(sent.models.map((m) => m.model)).toEqual(['example-model', 'other-model']);
    expect(within(fm).getAllByText('The host has not opened this model for sharing.').length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByText(/Saved as version/)).toBeNull();
  });

  it('shows a refused concurrency at its field', async () => {
    const f = host();
    f.on('PUT', '/admin/v1/consumers/{consumer_id}/grant', errorReply(400, null, 'The value is below the lowest allowed.', { param: 'limits.concurrency' }));
    renderApp(f, { hash: `#/consumers/${CID}/grant` });
    const fm = await form();
    await userEvent.setup().click(within(fm).getByRole('button', { name: 'Save grant' }));
    await within(fm).findByRole('alert');
    expect(within(fm).getByLabelText('Requests at once').getAttribute('aria-invalid')).toBe('true');
  });

  it('offers no form for a deleted consumer', async () => {
    const f = host();
    f.on('GET', '/admin/v1/consumers/{consumer_id}', { body: consumer({ status: 'deleted', display_name: null }) });
    renderApp(f, { hash: `#/consumers/${CID}/grant` });
    expect(await screen.findByText('This consumer is deleted: its grant cannot be changed.')).toBeTruthy();
    expect(screen.queryByRole('form', { name: 'Set the grant' })).toBeNull();
  });

  it("lists the grant's own rules and creates one for this grant", async () => {
    const f = host();
    f.on('GET', '/admin/v1/policy-rules', { body: { data: [policyRule({ grant_id: GID })] } });
    f.on('POST', '/admin/v1/policy-rules', { status: 201, body: policyRule({ id: 'r2', grant_id: GID }) });
    renderApp(f, { hash: `#/consumers/${CID}/grant` });
    const card = await screen.findByRole('region', { name: 'Rules of this grant' });
    expect(await within(card).findByText('Background work at night')).toBeTruthy();
    expect(f.callsTo('GET', '/admin/v1/policy-rules')[0]!.query.get('grant_id')).toBe(GID);
    const u = userEvent.setup();
    await u.click(within(card).getByRole('button', { name: 'New rule' }));
    const fm = within(card).getByRole('form', { name: 'New rule for this grant' });
    await u.click(within(fm).getByLabelText('Refuse these calls'));
    await u.click(within(fm).getByRole('button', { name: 'Create rule' }));
    await waitFor(() => expect(f.callsTo('POST', '/admin/v1/policy-rules')).toHaveLength(1));
    expect(f.callsTo('POST', '/admin/v1/policy-rules')[0]!.body).toEqual({ grant_id: GID, rule: { format_version: 1, deny: true } });
  });

  it('shows a grant that could not be read', async () => {
    const f = host();
    f.on('GET', '/admin/v1/consumers/{consumer_id}/grant', errorReply(500, null, 'The host could not read its database.'));
    renderApp(f, { hash: `#/consumers/${CID}/grant` });
    const alert = await screen.findByText('The grant could not be read');
    expect(alert.closest('[role=alert]')!.textContent).toContain('The host could not read its database.');
    expect(screen.queryByRole('form', { name: 'Set the grant' })).toBeNull();
  });
});

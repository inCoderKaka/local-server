import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import type { Schema } from '@local-server/shared';
import { CID, consumer } from '../consumers/testData';
import { grantVersion, hostModel, settingsWithDefaults } from '../grants/testData';
import { pages } from '../registry';
import { fakeHost, resourceRequest } from '../test/fixtures';
import { renderApp } from '../test/render';
import { limitsFromAsk } from './ask';

const XSS = '<img src=x onerror=alert(1)>';
const list = '/admin/v1/resource-requests';
const R1 = '0192e5a0-0006-7000-8000-000000000006';
const R2 = '0192e5a0-0006-7000-8000-000000000007';

function comment(body: string, kind: Schema<'PrincipalRef'>['kind'] = 'consumer'): Schema<'Comment'> {
  return {
    id: `c-${body.length}-${kind}`,
    author: { principal_id: CID, kind, display_name: kind === 'consumer' ? 'Example App' : null },
    body,
    created_at: '2026-01-15T10:05:00Z',
  };
}

/** A host with two open requests from one consumer; R1 asks for a second model and a context limit. */
function host(open = [resourceRequest(R1, { ask: { models: ['other-model'], context_limit: 8192, concurrency: 3 } }), resourceRequest(R2)]) {
  const f = fakeHost();
  let current = open;
  f.on('GET', '/admin/v1/settings', { body: { data: settingsWithDefaults() } });
  f.on('GET', list, (c) => {
    const s = c.query.get('status');
    return { body: { data: s ? current.filter((r) => r.status === s) : current, next_cursor: null } };
  });
  f.on('GET', '/admin/v1/resource-requests/{resource_request_id}', (c) => {
    const r = current.find((x) => c.path.endsWith(x.id));
    return r ? { body: { ...r, comments: [comment('Could I have more?')] } } : errorReply(404, 'not_found', 'No such request.');
  });
  f.on('GET', '/admin/v1/consumers/{consumer_id}', { body: consumer() });
  f.on('GET', '/admin/v1/consumers/{consumer_id}/grant', { body: grantVersion() });
  f.on('GET', '/admin/v1/models', { body: { data: [hostModel(), hostModel({ id: 'm2', name: 'other-model' })] } });
  /** Marks a request decided, as the host would after an approval or rejection. */
  const decide = (id: string, status: Schema<'ResourceRequestStatus'>, extra: Partial<Schema<'ResourceRequest'>> = {}) => {
    current = current.map((r) => (r.id === id ? { ...r, status, decided_at: '2026-01-15T11:00:00Z', expires_at: null, ...extra } : r));
    return { ...current.find((r) => r.id === id)!, comments: [comment('Could I have more?')] };
  };
  return { f, decide };
}

async function rows() {
  const table = await screen.findByRole('table', { name: 'Resource requests' });
  return within(table).getAllByRole('row').slice(1);
}

async function openDetail(id = R1) {
  const u = userEvent.setup();
  await waitFor(() => expect(screen.getAllByRole('button', { name: /^Show the request from Example App/ }).length).toBeGreaterThan(0));
  const i = id === R1 ? 0 : 1;
  await u.click(screen.getAllByRole('button', { name: /^Show the request from/ })[i]!);
  const card = await screen.findByRole('region', { name: 'Request' });
  await within(card).findByText('Could I have more?');
  return { u, card };
}

describe('requests page', () => {
  it('is registered at /requests in the navigation with a unique order', () => {
    const p = pages.find((x) => x.id === 'requests');
    expect(p).toMatchObject({ path: '/requests', label: 'Requests' });
    expect(pages.filter((x) => x.order === p!.order)).toHaveLength(1);
  });

  it('lists the pending requests by default, with consumer, status, ask and time, and the pending count', async () => {
    const { f } = host();
    f.on('GET', '/admin/v1/consumers/{consumer_id}', { body: consumer({ display_name: XSS }) });
    renderApp(f, { hash: '#/requests' });
    expect(await screen.findByRole('heading', { level: 1, name: 'Requests' })).toBeTruthy();
    const r = await rows();
    expect(r).toHaveLength(2);
    await waitFor(() => expect(r[0]!.textContent).toContain(XSS));
    expect(r[0]!.textContent).toContain('Pending');
    expect(r[0]!.textContent).toContain('Models: other-model');
    expect(r[0]!.textContent).toContain('Context limit: 8192 tokens');
    expect(within(r[0]!).getByRole('link').getAttribute('href')).toBe(`#/consumers/${CID}`);
    expect(await screen.findByText('2 pending')).toBeTruthy();
    // The page's list and the shell's count read the same filter.
    expect(f.callsTo('GET', list).every((c) => c.query.get('status') === 'open')).toBe(true);
    expect(document.querySelector('img')).toBeNull();
  });

  it('filters by state, and All sends no state', async () => {
    const { f, decide } = host();
    decide(R2, 'rejected');
    renderApp(f, { hash: '#/requests' });
    expect(await rows()).toHaveLength(1);
    const u = userEvent.setup();
    await u.selectOptions(screen.getByLabelText('Show'), 'rejected');
    await waitFor(async () => expect((await rows())[0]!.textContent).toContain('Rejected'));
    expect(f.callsTo('GET', list).some((c) => c.query.get('status') === 'rejected')).toBe(true);
    await u.selectOptions(screen.getByLabelText('Show'), '');
    await waitFor(async () => expect(await rows()).toHaveLength(2));
    expect(f.callsTo('GET', list).some((c) => !c.query.has('status'))).toBe(true);
  });

  it('says so when there is nothing pending', async () => {
    const { f } = host([]);
    renderApp(f, { hash: '#/requests' });
    expect(await screen.findByText('No pending requests.')).toBeTruthy();
    expect(await screen.findByText('0 pending')).toBeTruthy();
  });

  it('shows a calm note, not an error, while the host answers 501', async () => {
    const f = fakeHost();
    renderApp(f, { hash: '#/requests' });
    expect(await screen.findByRole('region', { name: 'Not available yet' })).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('shows a request with its comments as text, and adds a comment', async () => {
    const { f } = host();
    f.on('POST', '/admin/v1/resource-requests/{resource_request_id}/comments', (c) => ({
      status: 201,
      body: { ...resourceRequest(R1), comments: [comment('Could I have more?'), comment((c.body as { body: string }).body, 'host_admin')] },
    }));
    renderApp(f, { hash: '#/requests' });
    const { u, card } = await openDetail();
    expect(within(card).getByText('Please <b>more</b>')).toBeTruthy();
    expect(within(card).getByText('Could I have more?')).toBeTruthy();
    await u.type(within(card).getByLabelText('Your comment'), `Which model? ${XSS}`);
    await u.click(within(card).getByRole('button', { name: 'Add comment' }));
    expect(await within(card).findByText(`Which model? ${XSS}`)).toBeTruthy();
    expect(f.callsTo('POST', `${list}/${R1}/comments`)[0]!.body).toEqual({ body: `Which model? ${XSS}` });
    expect((within(card).getByLabelText('Your comment') as HTMLTextAreaElement).value).toBe('');
    expect(within(card).getByText('Host admin')).toBeTruthy();
    expect(document.querySelector('img')).toBeNull();
  });

  it('approves with the limits started from the grant and the ask, and a comment, then updates the list and the count', async () => {
    const { f, decide } = host();
    f.on('POST', '/admin/v1/resource-requests/{resource_request_id}/approve', () => ({
      body: { ...decide(R1, 'approved', { resulting_grant_version_id: 'v2' }), warnings: ['limits.bogus is not a known field and was ignored'] },
    }));
    renderApp(f, { hash: '#/requests' });
    expect(await screen.findByText('2 pending')).toBeTruthy();
    const { u, card } = await openDetail();
    const fm = await within(card).findByRole('form', { name: 'Approve the request' });
    // The asked model is ticked with the asked context limit; the grant's model keeps its own.
    expect((within(fm).getByLabelText('Allow other-model') as HTMLInputElement).checked).toBe(true);
    expect((within(fm).getByLabelText('Context limit for other-model (tokens)') as HTMLInputElement).value).toBe('8192');
    expect((within(fm).getByLabelText('Context limit for example-model (tokens)') as HTMLInputElement).value).toBe('4096');
    expect((within(fm).getByLabelText('Requests at once') as HTMLInputElement).value).toBe('3');
    await u.clear(within(fm).getByLabelText('Requests at once'));
    await u.type(within(fm).getByLabelText('Requests at once'), '2');
    await u.type(within(fm).getByLabelText('Comment for the consumer (optional)'), 'Enjoy');
    await u.click(within(fm).getByRole('button', { name: 'Approve' }));

    const status = await within(card).findByText('Approved: the consumer has a new grant version.');
    expect(status.closest('[role=status]')!.textContent).toContain('limits.bogus is not a known field and was ignored');
    expect(f.callsTo('POST', `${list}/${R1}/approve`)[0]!.body).toEqual({
      limits: {
        models: [
          { model: 'example-model', context_limit: 4096, max_output_tokens: null },
          { model: 'other-model', context_limit: 8192, max_output_tokens: null },
        ],
        quotas: [{ metric: 'tokens', limit: 100000, window_seconds: 86400 }],
        concurrency: 2,
        on_context_overflow: 'refuse',
        allowed_request_properties: null,
        expires_at: null,
      },
      comment: 'Enjoy',
    });
    // The open-only actions are gone, the list drops the request and the shared count follows.
    expect(within(card).queryByRole('form', { name: 'Approve the request' })).toBeNull();
    expect(within(card).getByText('Approved')).toBeTruthy();
    await waitFor(async () => expect(await rows()).toHaveLength(1));
    expect(await screen.findByText('1 pending')).toBeTruthy();
    expect(screen.getByRole('link', { name: '1 pending resource request' })).toBeTruthy();
  });

  it('approves without a comment field when the host leaves it empty, for a consumer with no grant yet', async () => {
    const { f, decide } = host([resourceRequest(R1, { ask: { models: ['example-model'] } })]);
    f.on('GET', '/admin/v1/consumers/{consumer_id}/grant', errorReply(404, 'not_found', 'This consumer has no grant yet.'));
    f.on('POST', '/admin/v1/resource-requests/{resource_request_id}/approve', () => ({ body: decide(R1, 'approved') }));
    renderApp(f, { hash: '#/requests' });
    const { u, card } = await openDetail();
    const fm = await within(card).findByRole('form', { name: 'Approve the request' });
    // A first grant takes the host's default concurrency.
    expect((within(fm).getByLabelText('Requests at once') as HTMLInputElement).value).toBe('1');
    await u.click(within(fm).getByRole('button', { name: 'Approve' }));
    await within(card).findByText('Approved: the consumer has a new grant version.');
    expect(f.callsTo('POST', `${list}/${R1}/approve`)[0]!.body).toEqual({
      limits: {
        models: [{ model: 'example-model', context_limit: null, max_output_tokens: null }],
        quotas: [],
        concurrency: 1,
        on_context_overflow: 'refuse',
        allowed_request_properties: null,
        expires_at: null,
      },
    });
  });

  it('rejects with a comment after a confirmation', async () => {
    const { f, decide } = host();
    f.on('POST', '/admin/v1/resource-requests/{resource_request_id}/reject', () => ({ body: decide(R1, 'rejected') }));
    renderApp(f, { hash: '#/requests' });
    const { u, card } = await openDetail();
    await u.type(within(card).getByLabelText('Reason for the consumer (optional)'), 'Not this month');
    await u.click(within(card).getByRole('button', { name: 'Reject' }));
    const confirm = within(card).getByRole('group', { name: 'Reject this request?' });
    await u.click(within(confirm).getByRole('button', { name: 'Reject the request' }));
    expect(await within(card).findByText('The request is now rejected.')).toBeTruthy();
    expect(f.callsTo('POST', `${list}/${R1}/reject`)[0]!.body).toEqual({ comment: 'Not this month' });
    await waitFor(async () => expect(await rows()).toHaveLength(1));
    expect(await screen.findByText('1 pending')).toBeTruthy();
  });

  it('shows a 409 on a request that was closed meanwhile, and shows it again as it is', async () => {
    const { f, decide } = host();
    f.on('POST', '/admin/v1/resource-requests/{resource_request_id}/reject', () => {
      decide(R1, 'withdrawn');
      return errorReply(409, 'conflict', 'The resource request is no longer open.', { type: 'conflict_error' });
    });
    renderApp(f, { hash: '#/requests' });
    const { u, card } = await openDetail();
    await u.click(within(card).getByRole('button', { name: 'Reject' }));
    await u.click(within(card).getByRole('button', { name: 'Reject the request' }));
    const alert = await within(card).findByRole('alert');
    expect(alert.textContent).toContain('already decided, withdrawn or expired');
    expect(alert.textContent).toContain('The resource request is no longer open.');
    expect(await within(card).findByText('Withdrawn')).toBeTruthy();
    expect(within(card).queryByRole('button', { name: 'Reject' })).toBeNull();
    expect(within(card).queryByLabelText('Your comment')).toBeNull();
    await waitFor(async () => expect(await rows()).toHaveLength(1));
  });

  it('shows a 409 on a comment the same way', async () => {
    const { f, decide } = host();
    f.on('POST', '/admin/v1/resource-requests/{resource_request_id}/comments', () => {
      decide(R1, 'expired');
      return errorReply(409, 'conflict', 'The resource request is no longer open.', { type: 'conflict_error' });
    });
    renderApp(f, { hash: '#/requests' });
    const { u, card } = await openDetail();
    await u.type(within(card).getByLabelText('Your comment'), 'Hello');
    await u.click(within(card).getByRole('button', { name: 'Add comment' }));
    expect((await within(card).findByRole('alert')).textContent).toContain('The resource request is no longer open.');
    expect(await within(card).findByText('Expired')).toBeTruthy();
  });
});

describe('limitsFromAsk', () => {
  const opts = { defaultConcurrency: 1, quotaWindowSeconds: 86400 };

  it('applies the asked limits to every grant model when the ask names none, and merges quotas and request types', () => {
    const l = limitsFromAsk(
      grantVersion({}, { allowed_request_properties: ['background'] }).limits,
      { max_output_tokens: 512, quota: { requests: 50 }, quotas: undefined, request_properties: ['batch', 'background'] },
      opts,
    );
    expect(l.models).toEqual([{ model: 'example-model', context_limit: 4096, max_output_tokens: 512 }]);
    expect(l.quotas).toEqual([
      { metric: 'tokens', limit: 100000, window_seconds: 86400 },
      { metric: 'requests', limit: 50, window_seconds: 86400 },
    ]);
    expect(l.allowed_request_properties).toEqual(['background', 'batch']);
    expect(l.concurrency).toBe(2);
  });

  it('replaces a quota with the same metric and window, and leaves the deprecated quota out when the window is unknown', () => {
    const cur = grantVersion().limits;
    expect(limitsFromAsk(cur, { quotas: [{ metric: 'tokens', limit: 5, window_seconds: 86400 }] }, opts).quotas).toEqual([
      { metric: 'tokens', limit: 5, window_seconds: 86400 },
    ]);
    expect(limitsFromAsk(cur, { quota: { tokens: 1 } }, { ...opts, quotaWindowSeconds: null }).quotas).toEqual(cur.quotas);
  });
});

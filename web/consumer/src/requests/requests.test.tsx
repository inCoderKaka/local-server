import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { formatCount, type Schema } from '@local-server/shared';
import { errorReply } from '@local-server/shared/test';
import { consumer, grantVersion, resourceRequest, signedIn } from '../test/fixtures';
import { renderApp } from '../test/render';

const XSS = '<img src=x onerror=alert(1)>';
const R1 = '0192e5a0-0006-7000-8000-000000000006';
const one = `/v1/resource-requests/${R1}`;

function comment(body: string, kind: Schema<'PrincipalRef'>['kind'], at = '2026-01-15T10:05:00Z'): Schema<'Comment'> {
  return {
    id: `c-${body.length}-${kind}-${at}`,
    author: { principal_id: 'p', kind, display_name: kind === 'consumer' ? 'Example Consumer' : 'Host admin name' },
    body,
    created_at: at,
  };
}

describe('the requests list', () => {
  it('lists the requests with their state and what they ask for, and filters by state', async () => {
    const f = signedIn();
    f.on('GET', '/v1/resource-requests', (c) => {
      const all = [resourceRequest(), resourceRequest({ id: 'r2', status: 'approved', ask: { concurrency: 3 }, decided_at: '2026-01-16T10:00:00Z' })];
      const s = c.query.get('status');
      return { body: { data: s ? all.filter((r) => r.status === s) : all, next_cursor: null } };
    });
    renderApp(f, { hash: '#/requests' });
    const table = await screen.findByRole('table', { name: 'Your resource requests' });
    let rows = within(table).getAllByRole('row').slice(1);
    expect(rows).toHaveLength(2);
    expect(rows[0]!.textContent).toContain('Pending');
    expect(rows[0]!.textContent).toContain('Models: example-model');
    expect(rows[1]!.textContent).toContain('Approved');
    expect(rows[1]!.textContent).toContain('Requests at once: 3');
    expect(within(rows[0]!).getByRole('link', { name: /^Open the request sent/ }).getAttribute('href')).toBe(`#/requests/${R1}`);

    await userEvent.setup().selectOptions(screen.getByLabelText('Show'), 'approved');
    await waitFor(() => expect(within(screen.getByRole('table', { name: 'Your resource requests' })).getAllByRole('row')).toHaveLength(2));
    rows = within(screen.getByRole('table', { name: 'Your resource requests' })).getAllByRole('row').slice(1);
    expect(rows[0]!.textContent).toContain('Approved');
    expect(f.calls.some((c) => c.path === '/v1/resource-requests' && c.query.get('status') === 'approved')).toBe(true);
  });

  it('says when there are none, and shows an error', async () => {
    renderApp(signedIn(), { hash: '#/requests' });
    expect(await screen.findByText('You have not sent a request yet.')).toBeTruthy();

    const f = signedIn();
    f.on('GET', '/v1/resource-requests', errorReply(500, 'internal_error', 'Something failed.', { type: 'server_error' }));
    renderApp(f, { hash: '#/requests' });
    expect((await screen.findByRole('alert')).textContent).toContain('Your requests could not be read');
  });
});

describe('asking for more', () => {
  it('sends a request with what is asked for and shows what the host ignored', async () => {
    const f = signedIn();
    f.on('POST', '/v1/resource-requests', { status: 201, body: { ...resourceRequest(), warnings: ['ask.bogus is not a known field and was ignored'] } });
    renderApp(f, { hash: '#/requests' });
    const form = await screen.findByRole('form', { name: 'New resource request' });
    const u = userEvent.setup();
    await u.type(within(form).getByLabelText('What you need and why'), 'A longer context, please.');
    await u.type(within(form).getByLabelText('Models'), 'example-model, second-model');
    await u.type(within(form).getByLabelText('Context limit (tokens)'), '8192');
    await u.type(within(form).getByLabelText('Requests at once'), '3');
    await u.type(within(form).getByLabelText('Tokens per window'), '200000');
    // The token window starts from the current grant's own window.
    expect((within(form).getByLabelText('Token window') as HTMLInputElement).value).toBe('24h');
    await u.click(within(form).getByRole('button', { name: 'Send request' }));

    const sent = await screen.findByText(/Your request was sent/);
    const box = sent.closest('[role="status"]') as HTMLElement;
    expect(box.textContent).toContain('ask.bogus is not a known field and was ignored');
    expect(within(box).getByRole('link', { name: 'Open the request' }).getAttribute('href')).toBe(`#/requests/${R1}`);
    expect(f.callsTo('POST', '/v1/resource-requests')[0]?.body).toEqual({
      message: 'A longer context, please.',
      ask: {
        models: ['example-model', 'second-model'],
        context_limit: 8192,
        concurrency: 3,
        quotas: [{ metric: 'tokens', limit: 200000, window_seconds: 86400 }],
      },
    });
    // The list is read again and the form is empty.
    expect(f.callsTo('GET', '/v1/resource-requests').length).toBeGreaterThan(1);
    expect((within(form).getByLabelText('What you need and why') as HTMLTextAreaElement).value).toBe('');
  });

  it('checks the fields before anything is sent', async () => {
    const f = signedIn();
    f.on('GET', '/v1/me/limits', { body: grantVersion({ quotas: [] }) });
    renderApp(f, { hash: '#/requests' });
    const form = await screen.findByRole('form', { name: 'New resource request' });
    const u = userEvent.setup();
    await u.type(within(form).getByLabelText('Context limit (tokens)'), 'lots');
    await u.type(within(form).getByLabelText('Requests per window'), '10');
    await u.click(within(form).getByRole('button', { name: 'Send request' }));
    expect(await within(form).findByText('Say what you need and why.')).toBeTruthy();
    expect(within(form).getByText('Write a whole number.')).toBeTruthy();
    expect(within(form).getByText('Write the window this amount is for, such as 24h.')).toBeTruthy();
    expect(f.callsTo('POST', '/v1/resource-requests')).toHaveLength(0);
  });

  it("shows the host's refusal as it words it", async () => {
    const f = signedIn();
    f.on('POST', '/v1/resource-requests', errorReply(409, 'too_many_open_requests', 'You already have 5 pending requests.'));
    renderApp(f, { hash: '#/requests' });
    const form = await screen.findByRole('form', { name: 'New resource request' });
    const u = userEvent.setup();
    await u.type(within(form).getByLabelText('What you need and why'), 'More, please.');
    await u.click(within(form).getByRole('button', { name: 'Send request' }));
    const alert = await within(form).findByRole('alert');
    expect(alert.textContent).toContain('The request was not sent');
    expect(alert.textContent).toContain('You already have 5 pending requests.');
  });

  it('offers no new request to a suspended account (D-155)', async () => {
    const f = signedIn();
    f.on('GET', '/v1/me', { body: { ...consumer, status: 'suspended' } });
    renderApp(f, { hash: '#/requests' });
    expect(await screen.findByText(/The host has suspended this account/)).toBeTruthy();
    expect(screen.queryByRole('form', { name: 'New resource request' })).toBeNull();
    expect(await screen.findByText('You have not sent a request yet.')).toBeTruthy();
  });
});

describe('one request', () => {
  function host(r = resourceRequest({ message: XSS, comments: [comment(`${XSS} first`, 'consumer')] })) {
    const f = signedIn();
    let current = r;
    f.on('GET', '/v1/resource-requests/{resource_request_id}', () => ({ body: current }));
    return {
      f,
      set: (next: Schema<'ResourceRequest'>) => {
        current = next;
      },
    };
  }

  it('shows the request and its comments as text', async () => {
    const { f } = host();
    const view = renderApp(f, { hash: `#/requests/${R1}` });
    expect(await screen.findByRole('heading', { level: 1, name: 'Request' })).toBeTruthy();
    const card = await screen.findByRole('region', { name: 'Request details' });
    await within(card).findByText('Pending');
    expect(within(card).getByText(XSS)).toBeTruthy();
    expect(within(card).getByText(`${XSS} first`)).toBeTruthy();
    expect(view.container.querySelector('img')).toBeNull();
    expect(card.textContent).toContain(`Context limit: ${formatCount(8192)} tokens`);
    expect(within(card).getByRole('list', { name: 'Comments, oldest first' }).textContent).toContain('You');
  });

  it('adds a comment', async () => {
    const { f } = host();
    f.on('POST', '/v1/resource-requests/{resource_request_id}/comments', (c) => ({
      status: 201,
      body: resourceRequest({ comments: [comment((c.body as { body: string }).body, 'consumer')] }),
    }));
    renderApp(f, { hash: `#/requests/${R1}` });
    const card = await screen.findByRole('region', { name: 'Request details' });
    const u = userEvent.setup();
    const add = await within(card).findByRole('button', { name: 'Add comment' });
    expect((add as HTMLButtonElement).disabled).toBe(true);
    await u.type(within(card).getByLabelText('Your comment'), 'Any news?');
    await u.click(add);
    expect(await within(card).findByText('Any news?')).toBeTruthy();
    expect(within(card).getByText('Your comment was added.')).toBeTruthy();
    expect(f.callsTo('POST', `${one}/comments`)[0]?.body).toEqual({ body: 'Any news?' });
    expect((within(card).getByLabelText('Your comment') as HTMLTextAreaElement).value).toBe('');
  });

  it('withdraws after a confirmation', async () => {
    const { f } = host();
    f.on('POST', '/v1/resource-requests/{resource_request_id}/withdraw', { body: resourceRequest({ status: 'withdrawn', decided_at: '2026-01-15T11:00:00Z' }) });
    renderApp(f, { hash: `#/requests/${R1}` });
    const card = await screen.findByRole('region', { name: 'Request details' });
    const u = userEvent.setup();
    await u.click(await within(card).findByRole('button', { name: 'Withdraw' }));
    expect(f.callsTo('POST', `${one}/withdraw`)).toHaveLength(0);
    const confirm = within(card).getByRole('group', { name: 'Withdraw this request?' });
    await u.click(within(confirm).getByRole('button', { name: 'Withdraw the request' }));
    expect(await within(card).findByText('You withdrew this request.')).toBeTruthy();
    expect(f.callsTo('POST', `${one}/withdraw`)).toHaveLength(1);
    expect(within(card).getByText('Withdrawn', { selector: 'span' })).toBeTruthy();
    expect(within(card).queryByRole('button', { name: 'Withdraw' })).toBeNull();
    expect(within(card).queryByLabelText('Your comment')).toBeNull();
  });

  it('reads the request again when the host decided it meanwhile', async () => {
    const { f, set } = host();
    f.on('POST', '/v1/resource-requests/{resource_request_id}/comments', () => {
      set(resourceRequest({ status: 'rejected', decided_at: '2026-01-15T11:00:00Z' }));
      return errorReply(409, 'conflict', 'The request is no longer open.');
    });
    renderApp(f, { hash: `#/requests/${R1}` });
    const card = await screen.findByRole('region', { name: 'Request details' });
    const u = userEvent.setup();
    await u.type(await within(card).findByLabelText('Your comment'), 'Hello?');
    await u.click(within(card).getByRole('button', { name: 'Add comment' }));
    expect((await within(card).findByRole('alert')).textContent).toContain('already decided or expired');
    expect(await within(card).findByText(/The host rejected this request/)).toBeTruthy();
  });

  it("shows the host's approval and its note, with a link to the limits", async () => {
    const decided = '2026-01-15T11:00:00Z';
    const { f } = host(
      resourceRequest({
        status: 'approved',
        decided_at: decided,
        expires_at: null,
        resulting_grant_version_id: 'gv2',
        comments: [comment('Please', 'consumer'), comment('Earlier host remark', 'host_admin'), comment(`Granted. ${XSS}`, 'host_admin', decided)],
      }),
    );
    const view = renderApp(f, { hash: `#/requests/${R1}` });
    const card = await screen.findByRole('region', { name: 'Request details' });
    const decision = await within(card).findByText(/The host approved this request/);
    const box = decision.parentElement as HTMLElement;
    expect(box.textContent).toContain(`The host's note: Granted. ${XSS}`);
    expect(box.textContent).not.toContain('Earlier host remark');
    expect(within(box).getByRole('link', { name: 'See your limits' }).getAttribute('href')).toBe('#/limits');
    expect(view.container.querySelector('img')).toBeNull();
    expect(within(card).queryByRole('button', { name: 'Withdraw' })).toBeNull();
  });

  it('shows an error for a request it cannot read', async () => {
    const f = signedIn();
    f.on('GET', '/v1/resource-requests/{resource_request_id}', errorReply(404, 'not_found', 'No such request.'));
    renderApp(f, { hash: `#/requests/${R1}` });
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('The request could not be read');
    expect(alert.textContent).toContain('No such request.');
  });
});

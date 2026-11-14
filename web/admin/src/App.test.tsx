import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import { fakeHost, user } from './test/fixtures';
import { renderApp } from './test/render';

describe('auth flow', () => {
  it('creates the first admin on first run and opens the dashboard', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/setup', { body: { admin_exists: false } });
    f.on('POST', '/admin/v1/setup', { status: 201, body: user });
    renderApp(f);
    const form = await screen.findByRole('form', { name: 'Create the first admin' });
    const u = userEvent.setup();
    await u.type(within(form).getByLabelText('Login name'), 'example-admin');
    await u.type(within(form).getByLabelText('Password'), 'pass phrase one');
    await u.type(within(form).getByLabelText('Repeat the password'), 'pass phrase two');
    await u.click(within(form).getByRole('button', { name: 'Create admin' }));
    expect(screen.getByText('The two passwords differ.')).toBeTruthy();
    expect(f.callsTo('POST', '/admin/v1/setup')).toHaveLength(0);
    await u.clear(within(form).getByLabelText('Repeat the password'));
    await u.type(within(form).getByLabelText('Repeat the password'), 'pass phrase one');
    await u.click(within(form).getByRole('button', { name: 'Create admin' }));
    expect(await screen.findByRole('heading', { name: 'Dashboard' })).toBeTruthy();
    expect(f.callsTo('POST', '/admin/v1/setup')[0]?.body).toEqual({ username: 'example-admin', password: 'pass phrase one' });
    // The session was never asked for: the setup answer started it.
    expect(f.callsTo('GET', '/admin/v1/session')).toHaveLength(0);
  });

  it('shows a refused setup', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/setup', { body: { admin_exists: false } });
    f.on('POST', '/admin/v1/setup', errorReply(409, 'conflict', 'A host admin already exists.'));
    renderApp(f);
    const form = await screen.findByRole('form', { name: 'Create the first admin' });
    const u = userEvent.setup();
    await u.type(within(form).getByLabelText('Login name'), 'a');
    await u.type(within(form).getByLabelText('Password'), 'p');
    await u.type(within(form).getByLabelText('Repeat the password'), 'p');
    await u.click(within(form).getByRole('button', { name: 'Create admin' }));
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('A host admin already exists.');
    expect(alert.textContent).toContain('Request id: req-');
  });

  it('logs in after a wrong password and a lockout', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/session', errorReply(401, null, 'Log in first: there is no valid admin session.'));
    f.on('POST', '/admin/v1/session', errorReply(401, null, 'The login name or password is wrong.'));
    renderApp(f);
    const form = await screen.findByRole('form', { name: 'Log in' });
    const u = userEvent.setup();
    await u.type(within(form).getByLabelText('Login name'), 'example-admin');
    await u.type(within(form).getByLabelText('Password'), 'wrong');
    await u.click(within(form).getByRole('button', { name: 'Log in' }));
    expect((await screen.findByRole('alert')).textContent).toContain('The login name or password is wrong.');
    // Still on the login page: a 401 from the login form is not a lost session.
    expect(screen.getByRole('form', { name: 'Log in' })).toBeTruthy();

    f.on('POST', '/admin/v1/session', errorReply(429, 'rate_limit_exceeded', 'Too many failed logins. Try again in 15 minutes.', { type: 'requests' }));
    await u.type(within(form).getByLabelText('Password'), 'wrong again');
    await u.click(within(form).getByRole('button', { name: 'Log in' }));
    const alert = await screen.findByText('Logging in is locked for now');
    expect(alert.closest('[role=alert]')?.textContent).toContain('Too many failed logins. Try again in 15 minutes.');

    f.on('POST', '/admin/v1/session', { body: user });
    await u.type(within(form).getByLabelText('Password'), 'right');
    await u.click(within(form).getByRole('button', { name: 'Log in' }));
    expect(await screen.findByRole('heading', { name: 'Dashboard' })).toBeTruthy();
    expect(f.callsTo('POST', '/admin/v1/session').at(-1)?.body).toEqual({ username: 'example-admin', password: 'right' });
  });

  it('logs out', async () => {
    const f = fakeHost();
    f.on('DELETE', '/admin/v1/session', { status: 204 });
    renderApp(f);
    await screen.findByRole('heading', { name: 'Dashboard' });
    expect(screen.getByText('example-admin')).toBeTruthy();
    await userEvent.setup().click(screen.getByRole('button', { name: 'Log out' }));
    expect(await screen.findByRole('form', { name: 'Log in' })).toBeTruthy();
    expect(screen.getByText('You are logged out.')).toBeTruthy();
    expect(f.callsTo('DELETE', '/admin/v1/session')).toHaveLength(1);
  });

  it('returns to the login page on any later 401', async () => {
    const f = fakeHost();
    renderApp(f);
    await screen.findByRole('heading', { name: 'Dashboard' });
    f.on('GET', '/admin/v1/host-metrics/history', errorReply(401, null, 'Log in first: there is no valid admin session.'));
    await userEvent.setup().click(await screen.findByRole('button', { name: 'Refresh' }));
    expect(await screen.findByRole('form', { name: 'Log in' })).toBeTruthy();
    expect(screen.getByText('Your session ended. Log in again.')).toBeTruthy();
    expect(screen.queryByRole('region', { name: 'Host resources' })).toBeNull();
  });

  it('shows an API that does not answer and retries', async () => {
    const f = fakeHost();
    f.on('GET', '/admin/v1/setup', { status: 500, body: { error: { message: 'Something failed on the host.', type: 'server_error', param: null, code: null } } });
    renderApp(f);
    expect((await screen.findByRole('alert')).textContent).toContain('Something failed on the host.');
    f.on('GET', '/admin/v1/setup', { body: { admin_exists: true } });
    await userEvent.setup().click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByRole('heading', { name: 'Dashboard' })).toBeTruthy();
  });
});

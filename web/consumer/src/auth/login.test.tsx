import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import { consumer, requestOptions, signedOut } from '../test/fixtures';
import { cancelled, fakeAssertion, installPasskeys, text } from '../test/passkeys';
import { renderApp } from '../test/render';

describe('login with a password', () => {
  it('logs in after a wrong password and a lockout', async () => {
    const f = signedOut();
    f.on('POST', '/v1/session/password', errorReply(401, null, 'The login name or password is wrong.'));
    renderApp(f);
    const form = await screen.findByRole('form', { name: 'Log in with a password' });
    const u = userEvent.setup();
    await u.type(within(form).getByLabelText('Login name'), 'example-user');
    await u.type(within(form).getByLabelText('Password'), 'wrong');
    await u.click(within(form).getByRole('button', { name: 'Log in' }));
    expect((await screen.findByRole('alert')).textContent).toContain('The login name or password is wrong.');
    // Still on the login page: a 401 from the login form is not a lost session.
    expect(screen.getByRole('form', { name: 'Log in with a password' })).toBeTruthy();
    expect(screen.queryByText('Your session ended. Log in again.')).toBeNull();
    expect((within(form).getByLabelText('Password') as HTMLInputElement).value).toBe('');

    // The host says how long the lockout lasts; the page writes no time of its own.
    f.on('POST', '/v1/session/password', errorReply(429, 'rate_limit_exceeded', 'Too many failed logins. Try again in 15 minutes.', { type: 'requests' }));
    await u.type(within(form).getByLabelText('Password'), 'wrong again');
    await u.click(within(form).getByRole('button', { name: 'Log in' }));
    const title = await screen.findByText('Logging in is locked for now');
    expect(title.closest('[role=alert]')?.textContent).toContain('Too many failed logins. Try again in 15 minutes.');

    f.on('POST', '/v1/session/password', { body: consumer });
    await u.type(within(form).getByLabelText('Password'), 'right');
    await u.click(within(form).getByRole('button', { name: 'Log in' }));
    expect(await screen.findByRole('heading', { name: 'Home' })).toBeTruthy();
    expect(f.callsTo('POST', '/v1/session/password').at(-1)?.body).toEqual({ username: 'example-user', password: 'right' });
  });
});

describe('login with a passkey', () => {
  it('uses a passkey of this browser with the host options and logs in', async () => {
    const { get } = installPasskeys();
    get.mockResolvedValue(fakeAssertion());
    const f = signedOut();
    f.on('POST', '/v1/session/passkey/begin', { body: { ceremony_id: 'ceremony-2', options: requestOptions } });
    f.on('POST', '/v1/session/passkey/finish', { body: consumer });
    renderApp(f);
    await userEvent.setup().click(await screen.findByRole('button', { name: 'Log in with a passkey' }));
    expect(await screen.findByRole('heading', { name: 'Home' })).toBeTruthy();
    const pk = get.mock.calls[0]![0].publicKey!;
    expect(text(pk.challenge)).toBe('login-challenge');
    expect(pk.rpId).toBe('host.example.invalid');
    expect(pk.timeout).toBe(300000);
    expect(pk.allowCredentials).toBeUndefined();
    expect(f.callsTo('POST', '/v1/session/passkey/finish')[0]?.body).toEqual({
      ceremony_id: 'ceremony-2',
      credential: {
        id: 'Y3JlZA',
        rawId: 'Y3JlZA',
        type: 'public-key',
        clientExtensionResults: {},
        response: {
          clientDataJSON: 'Y2xpZW50LWRhdGE',
          authenticatorData: 'YXV0aC1kYXRh',
          signature: 'c2lnbmF0dXJl',
          userHandle: 'aGFuZGxl',
        },
      },
    });
  });

  it("shows the host's refusal and stays on the login page", async () => {
    const { get } = installPasskeys();
    get.mockResolvedValue(fakeAssertion());
    const f = signedOut();
    f.on('POST', '/v1/session/passkey/begin', { body: { ceremony_id: 'ceremony-2', options: requestOptions } });
    f.on('POST', '/v1/session/passkey/finish', errorReply(401, null, 'The passkey could not be verified. Try again or use another way to log in.'));
    renderApp(f);
    await userEvent.setup().click(await screen.findByRole('button', { name: 'Log in with a passkey' }));
    expect((await screen.findByRole('alert')).textContent).toContain('The passkey could not be verified.');
    expect(screen.getByRole('form', { name: 'Log in with a password' })).toBeTruthy();
  });

  it('says so when the person cancels', async () => {
    const { get } = installPasskeys();
    get.mockRejectedValue(cancelled());
    const f = signedOut();
    f.on('POST', '/v1/session/passkey/begin', { body: { ceremony_id: 'ceremony-2', options: requestOptions } });
    renderApp(f);
    await userEvent.setup().click(await screen.findByRole('button', { name: 'Log in with a passkey' }));
    expect((await screen.findByRole('alert')).textContent).toContain('The passkey step was cancelled or took too long.');
    expect(f.callsTo('POST', '/v1/session/passkey/finish')).toHaveLength(0);
  });

  it('offers no passkey button in a browser without passkeys', async () => {
    renderApp(signedOut());
    await screen.findByRole('form', { name: 'Log in with a password' });
    expect(screen.queryByRole('button', { name: 'Log in with a passkey' })).toBeNull();
    expect(screen.getByText('This browser cannot use passkeys. Log in with your password.')).toBeTruthy();
  });
});

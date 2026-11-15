import { screen, within } from '@testing-library/react';
import userEvent, { type UserEvent } from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import { privacy, terms } from '../lib/legal';
import { consumer, creationOptions, signedOut } from '../test/fixtures';
import { cancelled, fakeAttestation, installPasskeys, text } from '../test/passkeys';
import { renderApp } from '../test/render';

const TOKEN = 'ls_invite_abcd1234_secretpart';

async function form() {
  return screen.findByRole('form', { name: 'Sign up' });
}

async function fillProfile(u: UserEvent, f: HTMLElement) {
  await u.type(within(f).getByLabelText('Your name'), 'Example Consumer');
  await u.type(within(f).getByLabelText('Email (optional)'), 'user@example.invalid');
}

async function fillPassword(u: UserEvent, f: HTMLElement, repeat = 'a long enough password') {
  await u.type(within(f).getByLabelText('Login name'), 'example-user');
  await u.type(within(f).getByLabelText('Password'), 'a long enough password');
  await u.type(within(f).getByLabelText('Repeat the password'), repeat);
}

async function accept(u: UserEvent, f: HTMLElement) {
  await u.click(within(f).getByLabelText(/accept the Terms of Use/));
  await u.click(within(f).getByLabelText(/accept the Privacy and consent notice/));
}

describe('sign-up with a password', () => {
  it('signs up from an invite link and opens the start page', async () => {
    const f = signedOut();
    f.on('POST', '/v1/invite-redemptions/password', { status: 201, body: consumer });
    renderApp(f, { hash: '#/signup', invite: TOKEN });
    const el = await form();
    const code = within(el).getByLabelText('Invite code') as HTMLInputElement;
    expect(code.value).toBe(TOKEN);
    expect(within(el).getByText('Filled in from your invite link.')).toBeTruthy();
    const u = userEvent.setup();
    await fillProfile(u, el);
    await fillPassword(u, el);
    await accept(u, el);
    await u.click(within(el).getByRole('button', { name: 'Create account' }));
    expect(await screen.findByRole('heading', { name: 'Home' })).toBeTruthy();
    expect(f.callsTo('POST', '/v1/invite-redemptions/password')[0]?.body).toEqual({
      invite_token: TOKEN,
      display_name: 'Example Consumer',
      email: 'user@example.invalid',
      username: 'example-user',
      password: 'a long enough password',
      consent: { terms_version: terms.version, privacy_version: privacy.version },
    });
  });

  it('takes the token out of a pasted invite link and leaves out an empty email', async () => {
    const f = signedOut();
    f.on('POST', '/v1/invite-redemptions/password', { status: 201, body: consumer });
    renderApp(f, { hash: '#/signup' });
    const el = await form();
    const u = userEvent.setup();
    await u.type(within(el).getByLabelText('Invite code'), `https://host.example.invalid/invite#${TOKEN}`);
    await u.type(within(el).getByLabelText('Your name'), 'Example Consumer');
    await fillPassword(u, el);
    await accept(u, el);
    await u.click(within(el).getByRole('button', { name: 'Create account' }));
    await screen.findByRole('heading', { name: 'Home' });
    const body = f.callsTo('POST', '/v1/invite-redemptions/password')[0]?.body as Record<string, unknown>;
    expect(body.invite_token).toBe(TOKEN);
    expect('email' in body).toBe(false);
  });

  it('requires both consents before anything is sent', async () => {
    const f = signedOut();
    renderApp(f, { hash: '#/signup', invite: TOKEN });
    const el = await form();
    const u = userEvent.setup();
    await fillProfile(u, el);
    await fillPassword(u, el);
    await u.click(within(el).getByLabelText(/accept the Terms of Use/));
    await u.click(within(el).getByRole('button', { name: 'Create account' }));
    expect((await screen.findByRole('alert')).textContent).toContain('accept both the Terms of Use and the Privacy and consent notice');
    expect(f.callsTo('POST', '/v1/invite-redemptions/password')).toHaveLength(0);
  });

  it('shows the texts to accept and their versions', async () => {
    renderApp(signedOut(), { hash: '#/signup' });
    const el = await form();
    expect(within(el).getByText(terms.version)).toBeTruthy();
    expect(within(el).getByText(privacy.version)).toBeTruthy();
    expect(within(el).getByLabelText('Terms of Use').textContent).toContain('# Terms of Use');
    expect(within(el).getByLabelText('Privacy and consent notice').textContent).toContain('# Privacy and consent');
  });

  it('refuses two different passwords without a call', async () => {
    const f = signedOut();
    renderApp(f, { hash: '#/signup', invite: TOKEN });
    const el = await form();
    const u = userEvent.setup();
    await fillProfile(u, el);
    await fillPassword(u, el, 'another password');
    await accept(u, el);
    await u.click(within(el).getByRole('button', { name: 'Create account' }));
    expect((await screen.findByRole('alert')).textContent).toContain('The two passwords differ.');
    expect(f.callsTo('POST', '/v1/invite-redemptions/password')).toHaveLength(0);
  });

  it("shows the host's refusal and stays on the form", async () => {
    const f = signedOut();
    f.on(
      'POST',
      '/v1/invite-redemptions/password',
      errorReply(410, null, 'This invite link was already used, was withdrawn by the host or has expired. Ask the host for a new one.'),
    );
    renderApp(f, { hash: '#/signup', invite: TOKEN });
    const el = await form();
    const u = userEvent.setup();
    await fillProfile(u, el);
    await fillPassword(u, el);
    await accept(u, el);
    await u.click(within(el).getByRole('button', { name: 'Create account' }));
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('This invite link was already used');
    expect(alert.textContent).toContain('Request id: req-');
    expect(screen.getByRole('form', { name: 'Sign up' })).toBeTruthy();
    expect((within(el).getByLabelText('Password') as HTMLInputElement).value).toBe('');

    f.on('POST', '/v1/invite-redemptions/password', errorReply(400, null, 'The password must be at least 12 characters long.', { param: 'password' }));
    await fillPassword(u, el);
    await u.click(within(el).getByRole('button', { name: 'Create account' }));
    expect((await screen.findByText('The password must be at least 12 characters long.')).closest('[role=alert]')).toBeTruthy();
  });
});

describe('sign-up with a passkey', () => {
  it('makes a passkey with the host options and signs up', async () => {
    const { create } = installPasskeys();
    create.mockResolvedValue(fakeAttestation());
    const f = signedOut();
    f.on('POST', '/v1/invite-redemptions/passkey/begin', { body: { ceremony_id: 'ceremony-1', options: creationOptions } });
    f.on('POST', '/v1/invite-redemptions/passkey/finish', { status: 201, body: { ...consumer, username: null } });
    renderApp(f, { hash: '#/signup', invite: TOKEN });
    const el = await form();
    const u = userEvent.setup();
    await fillProfile(u, el);
    await u.click(within(el).getByLabelText(/With a passkey/));
    expect(within(el).queryByLabelText('Login name')).toBeNull();
    expect(within(el).queryByLabelText('Password')).toBeNull();
    await accept(u, el);
    await u.click(within(el).getByRole('button', { name: 'Create account' }));
    expect(await screen.findByRole('heading', { name: 'Home' })).toBeTruthy();

    expect(f.callsTo('POST', '/v1/invite-redemptions/passkey/begin')[0]?.body).toEqual({
      invite_token: TOKEN,
      display_name: 'Example Consumer',
      email: 'user@example.invalid',
      consent: { terms_version: terms.version, privacy_version: privacy.version },
    });
    // The browser got the binary fields as bytes and every other option as the host sent it.
    const pk = create.mock.calls[0]![0].publicKey!;
    expect(text(pk.challenge)).toBe('challenge');
    expect(text(pk.user.id)).toBe('user-handle');
    expect(pk.timeout).toBe(300000);
    expect(pk.authenticatorSelection).toEqual(creationOptions.authenticatorSelection);
    expect(pk.rp).toEqual(creationOptions.rp);
    expect(f.callsTo('POST', '/v1/invite-redemptions/passkey/finish')[0]?.body).toEqual({
      ceremony_id: 'ceremony-1',
      credential: {
        id: 'Y3JlZA',
        rawId: 'Y3JlZA',
        type: 'public-key',
        authenticatorAttachment: 'platform',
        clientExtensionResults: {},
        response: { clientDataJSON: 'Y2xpZW50LWRhdGE', attestationObject: 'YXR0ZXN0YXRpb24', transports: ['internal'] },
      },
    });
  });

  it('says so when the person cancels, and sends nothing to finish', async () => {
    const { create } = installPasskeys();
    create.mockRejectedValue(cancelled());
    const f = signedOut();
    f.on('POST', '/v1/invite-redemptions/passkey/begin', { body: { ceremony_id: 'ceremony-1', options: creationOptions } });
    renderApp(f, { hash: '#/signup', invite: TOKEN });
    const el = await form();
    const u = userEvent.setup();
    await fillProfile(u, el);
    await u.click(within(el).getByLabelText(/With a passkey/));
    await accept(u, el);
    await u.click(within(el).getByRole('button', { name: 'Create account' }));
    expect((await screen.findByRole('alert')).textContent).toContain('The passkey step was cancelled or took too long.');
    expect(f.callsTo('POST', '/v1/invite-redemptions/passkey/finish')).toHaveLength(0);
    expect(screen.getByRole('form', { name: 'Sign up' })).toBeTruthy();
  });

  it("shows the host's refusal of the invite before the browser is asked", async () => {
    const { create } = installPasskeys();
    const f = signedOut();
    f.on('POST', '/v1/invite-redemptions/passkey/begin', errorReply(404, 'not_found', 'This invite link is not valid.'));
    renderApp(f, { hash: '#/signup', invite: TOKEN });
    const el = await form();
    const u = userEvent.setup();
    await fillProfile(u, el);
    await u.click(within(el).getByLabelText(/With a passkey/));
    await accept(u, el);
    await u.click(within(el).getByRole('button', { name: 'Create account' }));
    expect((await screen.findByRole('alert')).textContent).toContain('This invite link is not valid.');
    expect(create).not.toHaveBeenCalled();
  });

  it('offers only a password in a browser without passkeys', async () => {
    renderApp(signedOut(), { hash: '#/signup', invite: TOKEN });
    const el = await form();
    expect((within(el).getByLabelText(/With a passkey/) as HTMLInputElement).disabled).toBe(true);
    expect(within(el).getByText('This browser cannot use passkeys, so sign up with a password.')).toBeTruthy();
    expect(within(el).getByLabelText('Login name')).toBeTruthy();
  });
});

import { screen, waitFor, within } from '@testing-library/react';
import userEvent, { type UserEvent } from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import { creationOptions, settings, signedIn } from '../test/fixtures';
import { cancelled, fakeAttestation, installPasskeys, text } from '../test/passkeys';
import { renderApp } from '../test/render';

async function passwordForm() {
  return screen.findByRole('form', { name: 'Change your password' });
}

async function fill(u: UserEvent, form: HTMLElement, current: string, next: string, repeat = next) {
  if (current) await u.type(within(form).getByLabelText('Current password'), current);
  if (next) await u.type(within(form).getByLabelText('New password'), next);
  if (repeat) await u.type(within(form).getByLabelText('Repeat the new password'), repeat);
}

describe('my settings', () => {
  it('shows each setting with its value and default, saves one and resets one', async () => {
    const f = signedIn();
    f.on('PATCH', '/v1/me/settings', (c) => {
      const values = (c.body as { values: Record<string, unknown> }).values;
      return { body: { data: settings.map((s) => (s.key in values ? { ...s, value: values[s.key] ?? s.default_value } : s)) } };
    });
    renderApp(f, { hash: '#/settings' });
    const list = await screen.findByRole('list', { name: 'Your settings' });
    expect(list.textContent).toContain('jobs.result_ttl');
    expect(list.textContent).toContain("How long a job's result is kept.");
    expect(list.textContent).toContain('Now: 1h · Default: 24h');
    const u = userEvent.setup();
    const field = within(list).getByLabelText('New value for jobs.deadline');
    await u.clear(field);
    await u.type(field, '2h');
    await u.click(within(list).getByRole('button', { name: 'Save jobs.deadline' }));
    await waitFor(() => expect(list.textContent).toContain('Now: 2h · Default: 24h'));
    expect(f.callsTo('PATCH', '/v1/me/settings')[0]?.body).toEqual({ values: { 'jobs.deadline': '2h' } });

    await u.click(within(list).getByRole('button', { name: 'Reset jobs.result_ttl to default' }));
    await waitFor(() => expect(f.callsTo('PATCH', '/v1/me/settings')[1]?.body).toEqual({ values: { 'jobs.result_ttl': null } }));
    expect(await within(list).findByText('Back to the default.')).toBeTruthy();
  });

  it('refuses a value that is not a duration before sending it', async () => {
    const f = signedIn();
    renderApp(f, { hash: '#/settings' });
    const list = await screen.findByRole('list', { name: 'Your settings' });
    const u = userEvent.setup();
    const field = within(list).getByLabelText('New value for jobs.deadline');
    await u.clear(field);
    await u.type(field, 'tomorrow');
    await u.click(within(list).getByRole('button', { name: 'Save jobs.deadline' }));
    expect((await within(list).findByRole('alert')).textContent).toContain('Enter a duration such as 90s, 10m or 24h.');
    expect(f.callsTo('PATCH', '/v1/me/settings')).toHaveLength(0);
  });

  it('says when there are none, and shows an error', async () => {
    const f = signedIn();
    f.on('GET', '/v1/me/settings', { body: { data: [] } });
    renderApp(f, { hash: '#/settings' });
    expect(await screen.findByText('This host has no settings for you to change.')).toBeTruthy();

    const g = signedIn();
    g.on('GET', '/v1/me/settings', errorReply(500, 'internal_error', 'Something failed.', { type: 'server_error' }));
    renderApp(g, { hash: '#/settings' });
    expect((await screen.findByRole('alert')).textContent).toContain('Your settings could not be read');
  });
});

describe('changing the password', () => {
  it('checks the fields on the page and sends nothing until they agree', async () => {
    const f = signedIn();
    renderApp(f, { hash: '#/settings' });
    const form = await passwordForm();
    const u = userEvent.setup();
    await u.click(within(form).getByRole('button', { name: 'Change password' }));
    expect((await within(form).findByRole('alert')).textContent).toContain('Enter your current password.');
    await fill(u, form, 'old password here', 'a new long password', 'a different password');
    await u.click(within(form).getByRole('button', { name: 'Change password' }));
    expect(within(form).getByRole('alert').textContent).toContain('The two new passwords differ.');
    expect(within(form).queryByRole('group', { name: 'Change the password?' })).toBeNull();
    expect(f.callsTo('PUT', '/v1/me/password')).toHaveLength(0);
  });

  it('asks once more, then changes it and clears the fields', async () => {
    const f = signedIn();
    f.on('PUT', '/v1/me/password', { status: 204 });
    renderApp(f, { hash: '#/settings' });
    const form = await passwordForm();
    const u = userEvent.setup();
    await fill(u, form, 'old password here', 'a new long password');
    await u.click(within(form).getByRole('button', { name: 'Change password' }));
    const confirm = within(form).getByRole('group', { name: 'Change the password?' });
    expect(confirm.textContent).toContain('logged out');
    expect(f.callsTo('PUT', '/v1/me/password')).toHaveLength(0);
    await u.click(within(confirm).getByRole('button', { name: 'Change the password' }));
    expect(await within(form).findByText(/Your password is changed/)).toBeTruthy();
    expect(f.callsTo('PUT', '/v1/me/password')[0]?.body).toEqual({ current_password: 'old password here', new_password: 'a new long password' });
    for (const label of ['Current password', 'New password', 'Repeat the new password']) {
      expect((within(form).getByLabelText(label) as HTMLInputElement).value).toBe('');
    }
  });

  it("shows the host's refusal by the field it names", async () => {
    const f = signedIn();
    f.on('PUT', '/v1/me/password', errorReply(400, 'invalid_request', 'The current password is wrong.', { param: 'current_password' }));
    renderApp(f, { hash: '#/settings' });
    const form = await passwordForm();
    const u = userEvent.setup();
    await fill(u, form, 'not the password', 'a new long password');
    await u.click(within(form).getByRole('button', { name: 'Change password' }));
    await u.click(within(form).getByRole('button', { name: 'Change the password' }));
    const alert = await within(form).findByRole('alert');
    expect(alert.textContent).toContain('The password was not changed');
    const current = within(form).getByLabelText('Current password');
    expect(current.getAttribute('aria-invalid')).toBe('true');
    expect(screen.getAllByText('The current password is wrong.').length).toBe(2);
  });
});

describe('adding a passkey', () => {
  it('makes a passkey with the host options and finishes the ceremony', async () => {
    const { create } = installPasskeys();
    create.mockResolvedValue(fakeAttestation());
    const f = signedIn();
    f.on('POST', '/v1/me/passkeys/begin', { body: { ceremony_id: 'cer-1', options: creationOptions } });
    f.on('POST', '/v1/me/passkeys/finish', { status: 204 });
    renderApp(f, { hash: '#/settings' });
    const card = await screen.findByRole('region', { name: 'Passkeys' });
    await userEvent.setup().click(within(card).getByRole('button', { name: 'Add a passkey' }));
    expect(await within(card).findByText('The passkey is added. You can log in with it on this device.')).toBeTruthy();
    const opts = create.mock.calls[0]![0].publicKey!;
    expect(text(opts.challenge)).toBe('challenge');
    expect(opts.timeout).toBe(creationOptions.timeout);
    const body = f.callsTo('POST', '/v1/me/passkeys/finish')[0]?.body as { ceremony_id: string; credential: { id: string; response: Record<string, unknown> } };
    expect(body.ceremony_id).toBe('cer-1');
    expect(body.credential.id).toBe('Y3JlZA');
    expect(Object.keys(body.credential.response).sort()).toEqual(['attestationObject', 'clientDataJSON', 'transports']);
  });

  it('says so in plain words when the person cancels, and finishes nothing', async () => {
    const { create } = installPasskeys();
    create.mockRejectedValue(cancelled());
    const f = signedIn();
    f.on('POST', '/v1/me/passkeys/begin', { body: { ceremony_id: 'cer-1', options: creationOptions } });
    renderApp(f, { hash: '#/settings' });
    const card = await screen.findByRole('region', { name: 'Passkeys' });
    await userEvent.setup().click(within(card).getByRole('button', { name: 'Add a passkey' }));
    const alert = await within(card).findByRole('alert');
    expect(alert.textContent).toContain('The passkey was not added');
    expect(alert.textContent).toContain('The passkey step was cancelled or took too long. Try again.');
    expect(f.callsTo('POST', '/v1/me/passkeys/finish')).toHaveLength(0);
  });

  it('offers nothing in a browser without passkeys', async () => {
    renderApp(signedIn(), { hash: '#/settings' });
    const card = await screen.findByRole('region', { name: 'Passkeys' });
    expect(within(card).getByText('This browser cannot use passkeys.')).toBeTruthy();
    expect(within(card).queryByRole('button')).toBeNull();
  });
});

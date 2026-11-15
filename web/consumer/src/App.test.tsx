import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import { consumer, signedIn, signedOut } from './test/fixtures';
import { allPages, renderApp } from './test/render';

describe('routing while logged out', () => {
  it('shows the login form on the start page and on a page route, then that page after login', async () => {
    const f = signedOut();
    f.on('POST', '/v1/session/password', { body: consumer });
    renderApp(f, { pages: allPages, hash: '#/fake-b' });
    const form = await screen.findByRole('form', { name: 'Log in with a password' });
    expect(screen.queryByRole('heading', { name: 'Fake page B' })).toBeNull();
    expect(document.title).toBe('Log in · local-server');
    const u = userEvent.setup();
    await u.type(within(form).getByLabelText('Login name'), 'example-user');
    await u.type(within(form).getByLabelText('Password'), 'a long enough password');
    await u.click(within(form).getByRole('button', { name: 'Log in' }));
    expect(await screen.findByRole('heading', { name: 'Fake page B' })).toBeTruthy();
    expect(window.location.hash).toBe('#/fake-b');
  });

  it('shows the sign-up form on #/signup and links between the two', async () => {
    renderApp(signedOut(), { hash: '#/signup' });
    expect(await screen.findByRole('form', { name: 'Sign up' })).toBeTruthy();
    expect(document.title).toBe('Sign up · local-server');
    await userEvent.setup().click(screen.getByRole('link', { name: 'Log in' }));
    expect(await screen.findByRole('form', { name: 'Log in with a password' })).toBeTruthy();
    await userEvent.setup().click(screen.getByRole('link', { name: 'Sign up' }));
    expect(await screen.findByRole('form', { name: 'Sign up' })).toBeTruthy();
  });

  it('answers an unknown route with not found and a link to the login', async () => {
    renderApp(signedOut(), { hash: '#/no-such-page' });
    expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeTruthy();
    expect(screen.getByText('/no-such-page')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Go to the login page' }).getAttribute('href')).toBe('#/login');
    expect(screen.queryByRole('form')).toBeNull();
  });
});

describe('routing while logged in', () => {
  it('opens the start page with the consumer name and the navigation', async () => {
    renderApp(signedIn());
    expect(await screen.findByRole('heading', { name: 'Home' })).toBeTruthy();
    const header = screen.getByRole('banner');
    expect(within(header).getByText('Example Consumer')).toBeTruthy();
    expect(within(header).getByRole('button', { name: 'Log out' })).toBeTruthy();
    expect(within(screen.getByRole('navigation', { name: 'Main' })).getByRole('link', { name: 'Home' })).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Your account' }).textContent).toContain('You are logged in as Example Consumer.');
    expect(document.title).toBe('Home · local-server');
  });

  it('sends the login and sign-up routes to the start page', async () => {
    for (const hash of ['#/login', '#/signup']) {
      const view = renderApp(signedIn(), { hash });
      expect(await screen.findByRole('heading', { name: 'Home' })).toBeTruthy();
      await waitFor(() => expect(window.location.hash).toBe('#/'));
      expect(screen.queryByRole('form')).toBeNull();
      view.unmount();
    }
  });

  it('answers an unknown route with not found inside the layout', async () => {
    renderApp(signedIn(), { hash: '#/no-such-page' });
    expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Log out' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Go to the start page' }).getAttribute('href')).toBe('#/');
  });

  it('shows a consumer name as text, never as markup', async () => {
    const f = signedIn();
    f.on('GET', '/v1/me', { body: { ...consumer, display_name: '<img src=x onerror=alert(1)>' } });
    renderApp(f);
    await screen.findByRole('heading', { name: 'Home' });
    expect(screen.getAllByText('<img src=x onerror=alert(1)>').length).toBeGreaterThan(0);
    expect(document.querySelector('img')).toBeNull();
  });
});

describe('the session', () => {
  it('logs out', async () => {
    const f = signedIn();
    f.on('DELETE', '/v1/session', { status: 204 });
    renderApp(f);
    await screen.findByRole('heading', { name: 'Home' });
    await userEvent.setup().click(screen.getByRole('button', { name: 'Log out' }));
    expect(await screen.findByRole('form', { name: 'Log in with a password' })).toBeTruthy();
    expect(screen.getByText('You are logged out.')).toBeTruthy();
    expect(f.callsTo('DELETE', '/v1/session')).toHaveLength(1);
  });

  it('returns to the login form on any later 401', async () => {
    const f = signedIn();
    f.on('GET', '/v1/me/limits', errorReply(401, null, 'Log in first: there is no valid session.'));
    renderApp(f, { pages: allPages, hash: '#/fake-a' });
    await screen.findByRole('heading', { name: 'Fake page A' });
    await userEvent.setup().click(screen.getByRole('button', { name: 'Load limits' }));
    expect(await screen.findByRole('form', { name: 'Log in with a password' })).toBeTruthy();
    expect(screen.getByText('Your session ended. Log in again.')).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'Fake page A' })).toBeNull();
  });

  it('shows a host that does not answer and retries', async () => {
    const f = signedIn();
    f.on('GET', '/v1/me', { status: 500, body: { error: { message: 'Something failed on the host.', type: 'server_error', param: null, code: null } } });
    renderApp(f);
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('Something failed on the host.');
    expect(alert.textContent).toContain('Request id: req-');
    f.on('GET', '/v1/me', { body: consumer });
    await userEvent.setup().click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByRole('heading', { name: 'Home' })).toBeTruthy();
  });
});

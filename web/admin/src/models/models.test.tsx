import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import { hostModel, MID, settingsWithDefaults } from '../grants/testData';
import { fakeHost } from '../test/fixtures';
import { renderApp } from '../test/render';

const XSS = '<img src=x onerror=alert(1)>';
const patchPath = `/admin/v1/models/${MID}`;

function host() {
  const f = fakeHost();
  f.on('GET', '/admin/v1/settings', { body: { data: settingsWithDefaults() } });
  f.on('GET', '/admin/v1/models', {
    body: { data: [hostModel(), hostModel({ id: 'm2', name: XSS, allowed: false, loaded: false, size_bytes: null, keep_alive_seconds: 3600 })] },
  });
  return f;
}

async function table() {
  return screen.findByRole('table', { name: 'Models the engine has' });
}

describe('models page', () => {
  it('lists the models with their state, and a model name as text', async () => {
    renderApp(host(), { hash: '#/models' });
    const t = await table();
    const rows = within(t).getAllByRole('row');
    expect(rows[1]!.textContent).toContain('example-model');
    expect(rows[1]!.textContent).toContain('Loaded');
    expect(rows[1]!.textContent).toContain('4.0 GiB');
    expect((within(rows[1]!).getByLabelText('Share example-model') as HTMLInputElement).checked).toBe(true);
    expect((within(rows[1]!).getByLabelText('Preload example-model') as HTMLInputElement).checked).toBe(false);
    // An empty keep-alive is the host's default, read from its setting.
    const keep = within(rows[1]!).getByLabelText('Keep-alive for example-model') as HTMLInputElement;
    expect(keep.value).toBe('');
    expect(keep.placeholder).toBe('Host default (5m)');
    expect(rows[2]!.textContent).toContain(XSS);
    expect(rows[2]!.textContent).toContain('Not loaded');
    expect((within(rows[2]!).getByLabelText(`Keep-alive for ${XSS}`) as HTMLInputElement).value).toBe('1h');
    expect(document.querySelector('img')).toBeNull();
    expect(screen.getByText(/still unloads every idle model after its default keep-alive/)).toBeTruthy();
  });

  it('shares and preloads a model with one field each', async () => {
    const f = host();
    f.on('PATCH', '/admin/v1/models/{model_id}', (c) => ({ body: hostModel(c.body as object) }));
    renderApp(f, { hash: '#/models' });
    await table();
    const u = userEvent.setup();
    await u.click(screen.getByLabelText('Share example-model'));
    await waitFor(() => expect((screen.getByLabelText('Share example-model') as HTMLInputElement).checked).toBe(false));
    await u.click(screen.getByLabelText('Preload example-model'));
    await waitFor(() => expect((screen.getByLabelText('Preload example-model') as HTMLInputElement).checked).toBe(true));
    expect(f.callsTo('PATCH', patchPath).map((c) => c.body)).toEqual([{ allowed: false }, { preload: true }]);
  });

  it('sets a keep-alive and returns it to the host default', async () => {
    const f = host();
    f.on('PATCH', '/admin/v1/models/{model_id}', (c) => ({ body: hostModel(c.body as object) }));
    renderApp(f, { hash: '#/models' });
    await table();
    const u = userEvent.setup();
    await u.type(screen.getByLabelText('Keep-alive for example-model'), '10m');
    await u.click(screen.getByRole('button', { name: 'Save keep-alive for example-model' }));
    await waitFor(() => expect((screen.getByLabelText('Keep-alive for example-model') as HTMLInputElement).value).toBe('10m'));
    await u.clear(screen.getByLabelText('Keep-alive for example-model'));
    await u.click(screen.getByRole('button', { name: 'Save keep-alive for example-model' }));
    await waitFor(() => expect(f.callsTo('PATCH', patchPath)).toHaveLength(2));
    expect(f.callsTo('PATCH', patchPath).map((c) => c.body)).toEqual([{ keep_alive_seconds: 600 }, { keep_alive_seconds: null }]);
  });

  it('asks for a readable keep-alive before sending', async () => {
    const f = host();
    renderApp(f, { hash: '#/models' });
    await table();
    const u = userEvent.setup();
    await u.type(screen.getByLabelText('Keep-alive for example-model'), 'soon');
    await u.click(screen.getByRole('button', { name: 'Save keep-alive for example-model' }));
    expect(screen.getByText(/Write a duration such as/)).toBeTruthy();
    expect(screen.getByLabelText('Keep-alive for example-model').getAttribute('aria-invalid')).toBe('true');
    expect(f.callsTo('PATCH', patchPath)).toHaveLength(0);
  });

  it('shows a refused change and keeps the stored state', async () => {
    const f = host();
    f.on('PATCH', '/admin/v1/models/{model_id}', errorReply(400, null, 'A model whose name has a comma cannot be preloaded.', { param: 'preload' }));
    renderApp(f, { hash: '#/models' });
    await table();
    await userEvent.setup().click(screen.getByLabelText('Preload example-model'));
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('The change to example-model was not saved');
    expect(alert.textContent).toContain('cannot be preloaded');
    expect((screen.getByLabelText('Preload example-model') as HTMLInputElement).checked).toBe(false);
  });

  it('shows a list error', async () => {
    const f = host();
    f.on('GET', '/admin/v1/models', errorReply(502, null, 'The engine did not answer.'));
    renderApp(f, { hash: '#/models' });
    const alert = await screen.findByText('The models could not be read');
    expect(alert.closest('[role=alert]')!.textContent).toContain('The engine did not answer.');
  });
});

import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import { fakeHost, settingsList, type Setting } from '../test/fixtures';
import { renderApp } from '../test/render';

const SECRET = 'sealed-example-value';

function list(extra: Setting[] = []): Setting[] {
  return [
    ...settingsList(),
    { key: 'scheduler.ollama_num_parallel', owner: 'host', value: 1, default_value: 1, description: 'The value of OLLAMA_NUM_PARALLEL that Ollama runs with.' },
    { key: 'backup.s3.sealed_secret', owner: 'host', value: SECRET, default_value: '' },
    ...extra,
  ];
}

/** A host whose PATCH applies the values (null returns the default) and answers the whole list. */
function host(initial = list()) {
  const f = fakeHost();
  let current = initial;
  f.on('GET', '/admin/v1/settings', () => ({ body: { data: current } }));
  f.on('PATCH', '/admin/v1/settings', (c) => {
    const values = (c.body as { values: Record<string, unknown> }).values;
    current = current.map((s) => (s.key in values ? { ...s, value: values[s.key] === null ? s.default_value : values[s.key] } : s));
    return { body: { data: current } };
  });
  return f;
}

async function group(name: string) {
  return screen.findByRole('region', { name });
}

function row(key: string): HTMLElement {
  return screen.getByText(key, { selector: 'code.font-semibold' }).closest('li')!;
}

describe('settings page', () => {
  it('groups the settings by key prefix with description, value, default and unit', async () => {
    renderApp(host(), { hash: '#/settings' });
    const admin = await group('admin');
    expect(within(admin).getByText('admin.ui_poll_interval')).toBeTruthy();
    expect(await group('host')).toBeTruthy();
    const scheduler = await group('scheduler');
    const budget = within(scheduler).getByText('scheduler.memory_budget_bytes').closest('li')!;
    expect(budget.textContent).toContain('budget');
    expect(budget.textContent).toContain(`Current: ${24 * 1024 ** 3} bytes (24.0 GiB)`);
    expect(budget.textContent).toContain('Default: 0 bytes');
    expect(budget.textContent).toContain('In bytes.');
    expect(within(row('admin.ui_poll_interval')).getByText(/A duration such as/)).toBeTruthy();
    expect((within(row('host.quiet_hours.enabled')).getByLabelText('New value for host.quiet_hours.enabled') as HTMLInputElement).type).toBe('checkbox');
  });

  it('saves a value with PATCH and uses the answer, without reading the list again', async () => {
    const f = host();
    renderApp(f, { hash: '#/settings' });
    await group('admin');
    const r = row('admin.ui_poll_interval');
    fireEvent.change(within(r).getByLabelText('New value for admin.ui_poll_interval'), { target: { value: '45s' } });
    fireEvent.click(within(r).getByRole('button', { name: 'Save admin.ui_poll_interval' }));
    await waitFor(() => expect(row('admin.ui_poll_interval').textContent).toContain('Current: 45s'));
    expect(f.callsTo('PATCH', '/admin/v1/settings').map((c) => c.body)).toEqual([{ values: { 'admin.ui_poll_interval': '45s' } }]);
    expect(f.callsTo('GET', '/admin/v1/settings')).toHaveLength(1);
  });

  it('sends a whole number as a number and refuses one that is not', async () => {
    const f = host();
    renderApp(f, { hash: '#/settings' });
    await group('scheduler');
    const r = row('scheduler.ollama_num_parallel');
    const input = within(r).getByLabelText('New value for scheduler.ollama_num_parallel');
    fireEvent.change(input, { target: { value: '1.5' } });
    fireEvent.click(within(r).getByRole('button', { name: 'Save scheduler.ollama_num_parallel' }));
    expect(await within(r).findByText('Enter a whole number.')).toBeTruthy();
    expect(f.callsTo('PATCH', '/admin/v1/settings')).toHaveLength(0);
    fireEvent.change(input, { target: { value: '4' } });
    fireEvent.click(within(r).getByRole('button', { name: 'Save scheduler.ollama_num_parallel' }));
    await waitFor(() => expect(f.callsTo('PATCH', '/admin/v1/settings')).toHaveLength(1));
    expect(f.callsTo('PATCH', '/admin/v1/settings')[0]!.body).toEqual({ values: { 'scheduler.ollama_num_parallel': 4 } });
  });

  it('resets a setting to its default with null', async () => {
    const f = host();
    renderApp(f, { hash: '#/settings' });
    await group('scheduler');
    fireEvent.click(within(row('scheduler.memory_budget_bytes')).getByRole('button', { name: 'Reset scheduler.memory_budget_bytes to default' }));
    await waitFor(() => expect(row('scheduler.memory_budget_bytes').textContent).toContain('Current: 0 bytes'));
    expect(f.callsTo('PATCH', '/admin/v1/settings')[0]!.body).toEqual({ values: { 'scheduler.memory_budget_bytes': null } });
  });

  it('never shows, fills in or sends back a secret value', async () => {
    const f = host();
    renderApp(f, { hash: '#/settings' });
    await group('backup');
    expect(document.body.textContent).not.toContain(SECRET);
    const r = row('backup.s3.sealed_secret');
    expect(r.textContent).toContain('Current: Hidden');
    const input = within(r).getByLabelText('New value for backup.s3.sealed_secret') as HTMLInputElement;
    expect(input.type).toBe('password');
    expect(input.value).toBe('');
    const save = within(r).getByRole('button', { name: 'Save backup.s3.sealed_secret' }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    fireEvent.change(input, { target: { value: 'a-new-one' } });
    fireEvent.click(save);
    await waitFor(() => expect(f.callsTo('PATCH', '/admin/v1/settings')).toHaveLength(1));
    expect(f.callsTo('PATCH', '/admin/v1/settings')[0]!.body).toEqual({ values: { 'backup.s3.sealed_secret': 'a-new-one' } });
    await waitFor(() => expect((within(row('backup.s3.sealed_secret')).getByLabelText('New value for backup.s3.sealed_secret') as HTMLInputElement).value).toBe(''));
    expect(document.body.textContent).not.toContain('a-new-one');
  });

  it('explains raising both parallel settings next to the setting, or on the page without it', async () => {
    const { unmount } = renderApp(host(), { hash: '#/settings' });
    await group('scheduler');
    const note = within(row('scheduler.ollama_num_parallel')).getByText('Running more requests at once on Ollama').parentElement!;
    expect(note.textContent).toContain('OLLAMA_NUM_PARALLEL');
    expect(note.textContent).toContain('restart Ollama');
    expect(note.textContent).toContain('Raising only one has no effect on throughput');
    expect(note.textContent).toContain('what Ollama actually serves');
    unmount();
    renderApp(host(settingsList()), { hash: '#/settings' });
    await group('scheduler');
    expect(screen.getByText('Running more requests at once on Ollama')).toBeTruthy();
  });

  it('shows the API error of a refused change', async () => {
    const f = host();
    f.on('PATCH', '/admin/v1/settings', errorReply(403, 'forbidden', 'Setting admin.ui_poll_interval is not owned by the host, so the host cannot change it.'));
    renderApp(f, { hash: '#/settings' });
    await group('admin');
    fireEvent.click(within(row('admin.ui_poll_interval')).getByRole('button', { name: 'Save admin.ui_poll_interval' }));
    expect(await within(row('admin.ui_poll_interval')).findByText(/is not owned by the host/)).toBeTruthy();
    expect(row('admin.ui_poll_interval').textContent).toContain('Current: 30s');
  });
});

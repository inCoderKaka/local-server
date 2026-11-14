import { act, fireEvent, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fakeHost, settingsList } from '../test/fixtures';
import { allPages, fixturePages, renderApp, settle } from '../test/render';
import { pollIntervalFrom } from './settings';

const metrics = '/admin/v1/host-metrics';

function setHidden(hidden: boolean) {
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden });
  document.dispatchEvent(new Event('visibilitychange'));
}

afterEach(() => {
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => false });
});

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
  await settle();
}

describe('polling interval from admin.ui_poll_interval', () => {
  it('polls at the setting value', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    const f = fakeHost();
    f.on('GET', '/admin/v1/settings', { body: { data: settingsList({ 'admin.ui_poll_interval': { value: '5s', default_value: '30s' } }) } });
    renderApp(f, { pages: fixturePages, hash: '#/fake-a' });
    await settle();
    expect(f.callsTo('GET', metrics)).toHaveLength(1);
    await advance(4999);
    expect(f.callsTo('GET', metrics)).toHaveLength(1);
    await advance(1);
    expect(f.callsTo('GET', metrics)).toHaveLength(2);
    expect(f.callsTo('GET', '/admin/v1/host')).toHaveLength(2);
    expect(f.callsTo('GET', '/admin/v1/resource-requests')).toHaveLength(2);
    // The settings are read once per session, not polled.
    expect(f.callsTo('GET', '/admin/v1/settings')).toHaveLength(1);
  });

  it("falls back to the setting's own default_value when the value does not parse", async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    const f = fakeHost();
    f.on('GET', '/admin/v1/settings', { body: { data: settingsList({ 'admin.ui_poll_interval': { value: 'soon', default_value: '7s' } }) } });
    renderApp(f, { pages: fixturePages, hash: '#/fake-a' });
    await settle();
    await advance(6999);
    expect(f.callsTo('GET', metrics)).toHaveLength(1);
    await advance(1);
    expect(f.callsTo('GET', metrics)).toHaveLength(2);
  });

  it('does not poll at an invented interval when neither value parses', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    const f = fakeHost();
    f.on('GET', '/admin/v1/settings', { body: { data: settingsList({ 'admin.ui_poll_interval': { value: 'x', default_value: 'y' } }) } });
    renderApp(f, { pages: fixturePages, hash: '#/fake-a' });
    await settle();
    await advance(3_600_000);
    expect(f.callsTo('GET', metrics)).toHaveLength(1);
  });

  it('pauses while the tab is hidden and refreshes when it is shown', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    const f = fakeHost();
    f.on('GET', '/admin/v1/settings', { body: { data: settingsList({ 'admin.ui_poll_interval': { value: '5s' } }) } });
    renderApp(f, { pages: fixturePages, hash: '#/fake-a' });
    await settle();
    act(() => setHidden(true));
    await advance(60_000);
    expect(f.callsTo('GET', metrics)).toHaveLength(1);
    act(() => setHidden(false));
    await settle();
    expect(f.callsTo('GET', metrics)).toHaveLength(2);
    await advance(5000);
    expect(f.callsTo('GET', metrics)).toHaveLength(3);
  });

  it('re-reads the interval when settings are saved', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    const f = fakeHost();
    f.on('GET', '/admin/v1/settings', { body: { data: settingsList({ 'admin.ui_poll_interval': { value: '1h' } }) } });
    f.on('PATCH', '/admin/v1/settings', { body: { data: settingsList({ 'admin.ui_poll_interval': { value: '2s' } }) } });
    renderApp(f, { pages: allPages });
    await settle(40);
    const before = f.callsTo('GET', metrics).length;
    // fireEvent, not user-event: no timers of its own while the clock is faked.
    fireEvent.click(screen.getByRole('button', { name: 'Save quiet hours' }));
    await settle();
    expect(screen.getByText('Saved.')).toBeTruthy();
    await advance(2000);
    expect(f.callsTo('GET', metrics).length).toBe(before + 1);
  });

  it('reads the interval from the API values only', () => {
    expect(pollIntervalFrom(undefined)).toBeNull();
    expect(pollIntervalFrom([])).toBeNull();
    expect(pollIntervalFrom(settingsList({ 'admin.ui_poll_interval': { value: '1m30s' } }))).toBe(90_000);
    expect(pollIntervalFrom(settingsList({ 'admin.ui_poll_interval': { value: '0s', default_value: '45s' } }))).toBe(45_000);
    expect(pollIntervalFrom(settingsList({ 'admin.ui_poll_interval': { value: 30, default_value: '1h30m' } }))).toBe(5_400_000);
  });
});

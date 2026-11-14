import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { errorReply } from '@local-server/shared/test';
import { GID, policyRule, RID, settingsWithDefaults } from '../grants/testData';
import { fakeHost } from '../test/fixtures';
import { renderApp } from '../test/render';
import { describeRule, docOfValues, valuesOfDoc } from './ruleDoc';

const XSS = '<img src=x onerror=alert(1)>';
const rulePath = `/admin/v1/policy-rules/${RID}`;

function host() {
  const f = fakeHost();
  f.on('GET', '/admin/v1/settings', { body: { data: settingsWithDefaults() } });
  f.on('GET', '/admin/v1/policy-rules', {
    body: {
      data: [
        policyRule(),
        policyRule({ id: 'r2', name: XSS, grant_id: GID, position: 5, enabled: false, version: 3, rule: { format_version: 1, when: { models: ['big-model'] }, deny: true } }),
      ],
    },
  });
  return f;
}

async function card() {
  return screen.findByRole('region', { name: 'All rules' });
}

async function rulesTable() {
  return within(await card()).findByRole('table', { name: 'Rules, in the order they are checked' });
}

describe('rules page', () => {
  it('lists every rule with its scope, what it does and its state, and a rule name as text', async () => {
    const f = host();
    renderApp(f, { hash: '#/policy-rules' });
    const rows = within(await rulesTable()).getAllByRole('row');
    expect(rows[1]!.textContent).toContain('Background work at night');
    expect(rows[1]!.textContent).toContain('The whole host');
    expect(rows[1]!.textContent).toContain('Calls with background: only 00:00-07:00 (Local); priority background');
    expect(rows[1]!.textContent).toContain('On');
    expect(rows[2]!.textContent).toContain(XSS);
    expect(rows[2]!.textContent).toContain(`One grant ${GID}`);
    expect(rows[2]!.textContent).toContain('Calls to big-model: refused');
    expect(rows[2]!.textContent).toContain('Off');
    expect(document.querySelector('img')).toBeNull();
    // Every rule is asked for: no grant filter.
    expect([...f.callsTo('GET', '/admin/v1/policy-rules')[0]!.query.keys()]).toEqual([]);
    expect(screen.getByText(/Switching a rule on or off keeps its version/)).toBeTruthy();
  });

  it('creates a host-wide rule with background hours and a priority, with the exact body', async () => {
    const f = host();
    f.on('POST', '/admin/v1/policy-rules', { status: 201, body: policyRule({ id: 'r3' }) });
    renderApp(f, { hash: '#/policy-rules' });
    const c = await card();
    await rulesTable();
    const u = userEvent.setup();
    await u.click(within(c).getByRole('button', { name: 'New rule' }));
    const fm = within(c).getByRole('form', { name: 'New rule for the whole host' });
    await u.type(within(fm).getByLabelText('Name (optional)'), ' Night work ');
    await u.click(within(fm).getByRole('button', { name: 'Add a request type' }));
    const prop = within(fm).getByRole('group', { name: 'Request type 1' });
    await u.type(within(prop).getByLabelText('Request type'), 'background');
    await u.click(within(fm).getByRole('button', { name: 'Add a request type' }));
    const prop2 = within(fm).getByRole('group', { name: 'Request type 2' });
    await u.type(within(prop2).getByLabelText('Request type'), 'team');
    await u.selectOptions(within(prop2).getByLabelText('Matches when it'), 'text');
    await u.type(within(prop2).getByLabelText('Value'), 'research');
    await u.click(within(fm).getByLabelText('Allow these calls only in set hours'));
    await u.type(within(fm).getByLabelText('Time zone'), 'Europe/Berlin');
    const w = within(fm).getByRole('group', { name: 'Window 1' });
    await u.type(within(w).getByLabelText('From'), '22:00');
    await u.type(within(w).getByLabelText('Until'), '07:00');
    await u.selectOptions(within(fm).getByLabelText('Priority'), 'background');
    await u.type(within(fm).getByLabelText('Most output tokens per call'), '512');
    await u.click(within(fm).getByRole('button', { name: 'Create rule' }));

    await waitFor(() => expect(within(c).queryByRole('form', { name: 'New rule for the whole host' })).toBeNull());
    expect(f.callsTo('POST', '/admin/v1/policy-rules')[0]!.body).toEqual({
      name: 'Night work',
      rule: {
        format_version: 1,
        when: { properties: { background: true, team: 'research' } },
        time_zone: 'Europe/Berlin',
        windows: [{ start: '22:00', end: '07:00' }],
        priority: 'background',
        caps: { max_output_tokens: 512 },
      },
    });
    // The list is read again, in the host's order.
    expect(f.callsTo('GET', '/admin/v1/policy-rules').length).toBeGreaterThanOrEqual(2);
  });

  it('checks the rule before sending', async () => {
    const f = host();
    renderApp(f, { hash: '#/policy-rules' });
    const c = await card();
    const u = userEvent.setup();
    await u.click(within(c).getByRole('button', { name: 'New rule' }));
    const fm = within(c).getByRole('form', { name: 'New rule for the whole host' });
    await u.click(within(fm).getByRole('button', { name: 'Create rule' }));
    expect(within(fm).getByText(/Choose what the rule does/)).toBeTruthy();
    await u.click(within(fm).getByRole('button', { name: 'Add a request type' }));
    await u.type(within(fm).getByLabelText('Request type'), 'Not Valid');
    await u.click(within(fm).getByLabelText('Allow these calls only in set hours'));
    const w = within(fm).getByRole('group', { name: 'Window 1' });
    await u.type(within(w).getByLabelText('From'), '7am');
    await u.click(within(fm).getByRole('button', { name: 'Create rule' }));
    expect(within(fm).getByLabelText('Request type').getAttribute('aria-invalid')).toBe('true');
    expect(within(fm).getByLabelText('Time zone').getAttribute('aria-invalid')).toBe('true');
    expect(within(w).getByLabelText('From').getAttribute('aria-invalid')).toBe('true');
    expect(f.callsTo('POST', '/admin/v1/policy-rules')).toHaveLength(0);
  });

  it('shows a refused rule, at the field when the host names one', async () => {
    const f = host();
    f.on('POST', '/admin/v1/policy-rules', errorReply(400, null, 'The name must be 1 to 120 characters.', { param: 'name' }));
    renderApp(f, { hash: '#/policy-rules' });
    const c = await card();
    const u = userEvent.setup();
    await u.click(within(c).getByRole('button', { name: 'New rule' }));
    const fm = within(c).getByRole('form', { name: 'New rule for the whole host' });
    await u.type(within(fm).getByLabelText('Name (optional)'), 'x');
    await u.click(within(fm).getByLabelText('Refuse these calls'));
    await u.click(within(fm).getByRole('button', { name: 'Create rule' }));
    expect((await within(fm).findByRole('alert')).textContent).toContain('The rule was not saved');
    expect(within(fm).getByLabelText('Name (optional)').getAttribute('aria-invalid')).toBe('true');

    f.on('POST', '/admin/v1/policy-rules', errorReply(400, null, 'The rule is not valid under the policy rule format, or it is longer than this host allows.', { param: 'rule' }));
    await u.click(within(fm).getByRole('button', { name: 'Create rule' }));
    await waitFor(() => expect(within(fm).getByRole('alert').textContent).toContain('not valid under the policy rule format'));
    expect(within(fm).getByLabelText('Name (optional)').getAttribute('aria-invalid')).toBeNull();
  });

  it('edits a rule from its stored document and sends name, position and rule', async () => {
    const f = host();
    f.on('PUT', '/admin/v1/policy-rules/{policy_rule_id}', { body: policyRule({ name: 'Renamed', version: 2 }) });
    renderApp(f, { hash: '#/policy-rules' });
    const c = await card();
    await rulesTable();
    const u = userEvent.setup();
    await u.click(within(c).getByRole('button', { name: 'Edit rule Background work at night' }));
    const fm = within(c).getByRole('form', { name: 'Edit the rule' });
    expect((within(fm).getByLabelText('Time zone') as HTMLInputElement).value).toBe('Local');
    expect(fm.textContent).toContain('stores a new version');
    const name = within(fm).getByLabelText('Name');
    await u.clear(name);
    await u.type(name, 'Renamed');
    await u.click(within(fm).getByRole('button', { name: 'Save rule' }));
    await waitFor(() => expect(f.callsTo('PUT', rulePath)).toHaveLength(1));
    expect(f.callsTo('PUT', rulePath)[0]!.body).toEqual({ name: 'Renamed', position: 0, rule: policyRule().rule });
  });

  it('switches a rule off by sending its stored document unchanged', async () => {
    const f = host();
    f.on('PUT', '/admin/v1/policy-rules/{policy_rule_id}', { body: policyRule({ enabled: false }) });
    renderApp(f, { hash: '#/policy-rules' });
    await rulesTable();
    await userEvent.setup().click(screen.getByRole('button', { name: 'Switch off rule Background work at night' }));
    expect(await screen.findByRole('button', { name: 'Switch on rule Background work at night' })).toBeTruthy();
    expect(f.callsTo('PUT', rulePath)[0]!.body).toEqual({ enabled: false, rule: policyRule().rule });
  });

  it('shows a refused switch', async () => {
    const f = host();
    f.on('PUT', '/admin/v1/policy-rules/{policy_rule_id}', errorReply(404, 'not_found', 'No such policy rule.'));
    renderApp(f, { hash: '#/policy-rules' });
    await rulesTable();
    await userEvent.setup().click(screen.getByRole('button', { name: 'Switch off rule Background work at night' }));
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('Rule Background work at night was not switched');
    expect(alert.textContent).toContain('No such policy rule.');
  });

  it('deletes a rule only after a confirmation', async () => {
    const f = host();
    f.on('DELETE', '/admin/v1/policy-rules/{policy_rule_id}', { status: 204 });
    renderApp(f, { hash: '#/policy-rules' });
    const c = await card();
    await rulesTable();
    const u = userEvent.setup();
    await u.click(within(c).getByRole('button', { name: 'Delete rule Background work at night' }));
    let confirm = within(c).getByRole('group', { name: 'Delete rule Background work at night?' });
    expect(confirm.textContent).toContain('stops applying at once');
    await u.click(within(confirm).getByRole('button', { name: 'Cancel' }));
    expect(f.callsTo('DELETE', rulePath)).toHaveLength(0);
    await u.click(within(c).getByRole('button', { name: 'Delete rule Background work at night' }));
    confirm = within(c).getByRole('group', { name: 'Delete rule Background work at night?' });
    await u.click(within(confirm).getByRole('button', { name: 'Delete rule' }));
    await waitFor(() => expect(within(c).queryByText('Background work at night')).toBeNull());
    expect(f.callsTo('DELETE', rulePath)).toHaveLength(1);
  });

  it("shows a rule's history", async () => {
    const f = host();
    f.on('GET', '/admin/v1/policy-rules/{policy_rule_id}/versions', {
      body: { data: [policyRule({ version: 2 }), policyRule({ version: 1, name: 'First name', enabled: undefined, rule: { format_version: 1, deny: true } })] },
    });
    renderApp(f, { hash: '#/policy-rules' });
    await rulesTable();
    await userEvent.setup().click(screen.getByRole('button', { name: 'History of rule Background work at night' }));
    const t = await screen.findByRole('table', { name: 'Versions of this rule, newest first' });
    const rows = within(t).getAllByRole('row');
    expect(rows[1]!.textContent).toContain('2');
    expect(rows[2]!.textContent).toContain('First name');
    expect(rows[2]!.textContent).toContain('Every call: refused');
  });

  it('does not offer the form for a rule document it cannot show, and shows the document as text', async () => {
    const f = host();
    f.on('GET', '/admin/v1/policy-rules', { body: { data: [policyRule({ rule: { format_version: 2, future: XSS } })] } });
    renderApp(f, { hash: '#/policy-rules' });
    const row = within(await rulesTable()).getAllByRole('row')[1]!;
    expect(row.textContent).toContain('cannot show as a form');
    expect(row.textContent).toContain(XSS);
    expect(within(row).queryByRole('button', { name: /^Edit/ })).toBeNull();
    expect(within(row).getByRole('button', { name: 'Switch off rule Background work at night' })).toBeTruthy();
    expect(document.querySelector('img')).toBeNull();
  });
});

describe('rule document', () => {
  it('reads a stored document back into the same document', () => {
    const docs = [
      policyRule().rule,
      { format_version: 1, deny: true },
      { format_version: 1, when: { properties: { tier: 3, batch: false }, models: ['a', 'b'] }, caps: { context_cap: 2048, max_concurrency: 1 } },
    ];
    for (const d of docs) {
      const v = valuesOfDoc(d);
      expect(v).not.toBeNull();
      expect(docOfValues(v!).doc).toEqual(d);
    }
  });

  it('refuses to read what it cannot show', () => {
    expect(valuesOfDoc({ format_version: 2 })).toBeNull();
    expect(valuesOfDoc({ format_version: 1, extra: true })).toBeNull();
    expect(valuesOfDoc({ format_version: 1, when: { properties: { a: [1] } } })).toBeNull();
    expect(describeRule({ format_version: 1, when: { other: 1 } })).toBeNull();
  });

  it('refuses a window whose end equals its start and a deny with other effects is never built', () => {
    const v = valuesOfDoc({ format_version: 1, time_zone: 'UTC', windows: [{ start: '01:00', end: '01:00' }] })!;
    expect(docOfValues(v).problems['windows.0.end']).toBeTruthy();
    const deny = { ...valuesOfDoc(policyRule().rule)!, effect: 'deny' as const };
    expect(docOfValues(deny).doc).toEqual({ format_version: 1, when: { properties: { background: true } }, deny: true });
  });
});

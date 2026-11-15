import { useState, type ReactNode } from 'react';
import { asApiError, Badge, Button, call, Card, ConfirmAction, ErrorBanner, formatDateTime, Spinner, Table, useApi, useClient, UserText, type ApiError, type Column } from '@local-server/shared';
import { HOST_SETTING, intSetting } from '../grants/values';
import { useSettings } from '../lib/settings';
import { RuleForm } from './RuleForm';
import { describeRule, valuesOfDoc, type PolicyRule } from './ruleDoc';

/** The name of a rule as text: the host typed it, but it is shown as text like any typed name. */
function RuleName({ rule }: { rule: PolicyRule }) {
  return <UserText text={rule.name ?? rule.id} />;
}

/** What a rule does, and its document when this page cannot put it in words. */
function RuleEffect({ rule }: { rule: PolicyRule }) {
  const text = describeRule(rule.rule);
  if (text !== null) return <UserText text={text} />;
  return (
    <span className="flex flex-col gap-1">
      <span className="italic text-slate-600">A rule this page cannot show as a form. It can be switched on or off and deleted.</span>
      <code className="text-xs">
        <UserText text={JSON.stringify(rule.rule)} />
      </code>
    </span>
  );
}

function RuleHistory({ rule }: { rule: PolicyRule }) {
  const client = useClient();
  const versions = useApi(
    async () => (await call(client.GET('/admin/v1/policy-rules/{policy_rule_id}/versions', { params: { path: { policy_rule_id: rule.id } } }))).data,
    [client, rule.id, rule.version],
  );
  const columns: Column<PolicyRule>[] = [
    { key: 'version', header: 'Version', render: (r) => String(r.version) },
    { key: 'name', header: 'Name', render: (r) => <RuleName rule={r} /> },
    { key: 'position', header: 'Position', render: (r) => String(r.position ?? 0) },
    { key: 'stored', header: 'Stored', render: (r) => formatDateTime(r.created_at) },
    { key: 'does', header: 'What it did', render: (r) => <RuleEffect rule={r} /> },
  ];
  return (
    <section aria-label={`History of rule ${rule.name ?? rule.id}`} className="flex flex-col gap-2 rounded border border-slate-200 p-3">
      <ErrorBanner error={versions.error} title="The rule's history could not be read" />
      {versions.loading && !versions.data && <Spinner label="Loading the rule's history" />}
      {versions.data && <Table caption="Versions of this rule, newest first" columns={columns} rows={versions.data} rowKey={(r) => String(r.version)} />}
    </section>
  );
}

/**
 * Policy rules: every rule (`grantId` undefined) or one grant's own rules, with forms to create
 * and edit them, a switch, a deletion that is confirmed first and each rule's history (req_001
 * D-15, D-25, D-87, D-132). A rule created here is for the grant, or for the whole host when the
 * panel lists every rule.
 */
export function RulesPanel({ grantId, title, intro }: { grantId?: string; title: string; intro?: ReactNode }) {
  const client = useClient();
  const settings = useSettings();
  const rules = useApi(
    async () => (await call(client.GET('/admin/v1/policy-rules', { params: { query: grantId ? { grant_id: grantId } : {} } }))).data,
    [client, grantId],
  );
  const [editing, setEditing] = useState<'new' | string | null>(null);
  const [history, setHistory] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<{ rule: PolicyRule; error: ApiError } | null>(null);
  const maxFailures = intSetting(settings.list, HOST_SETTING.ruleMaxFailures);

  const replace = (r: PolicyRule) => rules.setData((rules.data ?? []).map((x) => (x.id === r.id ? r : x)));

  async function toggle(r: PolicyRule) {
    setBusy(r.id);
    setError(null);
    try {
      // The document is sent as stored and only the switch changes, so no new version is made.
      replace(await call(client.PUT('/admin/v1/policy-rules/{policy_rule_id}', { params: { path: { policy_rule_id: r.id } }, body: { enabled: !r.enabled, rule: r.rule } })));
    } catch (e) {
      setError({ rule: r, error: asApiError(e) });
    } finally {
      setBusy(null);
    }
  }

  async function remove(r: PolicyRule) {
    await call(client.DELETE('/admin/v1/policy-rules/{policy_rule_id}', { params: { path: { policy_rule_id: r.id } } }));
    if (editing === r.id) setEditing(null);
    if (history === r.id) setHistory(null);
    rules.setData((rules.data ?? []).filter((x) => x.id !== r.id));
  }

  const columns: Column<PolicyRule>[] = [
    { key: 'position', header: 'Position', render: (r) => String(r.position ?? 0) },
    { key: 'name', header: 'Name', render: (r) => <RuleName rule={r} /> },
    ...(grantId
      ? []
      : [
          {
            key: 'scope',
            header: 'Applies to',
            render: (r: PolicyRule) =>
              r.grant_id ? (
                <span>
                  One grant <code className="text-xs">{r.grant_id}</code>
                </span>
              ) : (
                'The whole host'
              ),
          },
        ]),
    { key: 'does', header: 'What it does', render: (r) => <RuleEffect rule={r} /> },
    { key: 'state', header: 'State', render: (r) => (r.enabled ? <Badge tone="good">On</Badge> : <Badge tone="neutral">Off</Badge>) },
    { key: 'version', header: 'Version', render: (r) => String(r.version) },
    {
      key: 'actions',
      header: 'Actions',
      render: (r) => {
        const label = r.name ?? r.id;
        return (
          <div className="flex flex-wrap gap-2">
            {valuesOfDoc(r.rule) && (
              <Button aria-label={`Edit rule ${label}`} onClick={() => setEditing(r.id)}>
                Edit
              </Button>
            )}
            <Button aria-label={`${r.enabled ? 'Switch off' : 'Switch on'} rule ${label}`} disabled={busy === r.id} onClick={() => void toggle(r)}>
              {r.enabled ? 'Switch off' : 'Switch on'}
            </Button>
            <Button aria-label={`History of rule ${label}`} aria-expanded={history === r.id} onClick={() => setHistory(history === r.id ? null : r.id)}>
              History
            </Button>
            <ConfirmAction label="Delete" name={`Delete rule ${label}`} title={`Delete rule ${label}?`} confirmLabel="Delete rule" onConfirm={() => remove(r)}>
              The rule stops applying at once. Its stored versions are kept but no longer shown here. This cannot be undone.
            </ConfirmAction>
          </div>
        );
      },
    },
  ];

  const editRule = editing && editing !== 'new' ? rules.data?.find((r) => r.id === editing) : undefined;
  const historyRule = history ? rules.data?.find((r) => r.id === history) : undefined;

  return (
    <Card title={title}>
      <div className="flex flex-col gap-3">
        {intro && <p className="text-sm text-slate-700">{intro}</p>}
        <p className="text-xs text-slate-600">
          Switching a rule on or off keeps its version; changing its name, position or rule stores a new version.
          {maxFailures !== null
            ? ` A stored rule the host cannot read refuses the calls it covers; the host switches it off after ${maxFailures} failed checks, unless it is a rule that refuses calls.`
            : ''}
        </p>
        <ErrorBanner error={rules.error} title="The rules could not be read" />
        {error && (
          <ErrorBanner
            error={error.error}
            title={
              <>
                Rule <RuleName rule={error.rule} /> was not switched
              </>
            }
          />
        )}
        {rules.loading && !rules.data && <Spinner label="Loading rules" />}
        {rules.data && <Table caption="Rules, in the order they are checked" columns={columns} rows={rules.data} rowKey={(r) => r.id} empty="No rules yet." />}
        {historyRule && <RuleHistory rule={historyRule} />}
        {editing === null && (
          <div>
            <Button variant="primary" onClick={() => setEditing('new')}>
              New rule
            </Button>
          </div>
        )}
        {(editing === 'new' || editRule) && (
          <RuleForm
            key={editing}
            rule={editRule}
            grantId={grantId ?? null}
            onCancel={() => setEditing(null)}
            onSaved={() => {
              setEditing(null);
              // Read the list again: a new rule or a new position changes the order.
              void rules.refetch();
            }}
          />
        )}
      </div>
    </Card>
  );
}

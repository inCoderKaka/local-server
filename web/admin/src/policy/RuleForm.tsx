import { useId, useState, type FormEvent } from 'react';
import { asApiError, Button, call, CheckboxField, ErrorBanner, Field, useClient, wholeFrom, type ApiError } from '@local-server/shared';
import { docOfValues, emptyRule, nextKey, valuesOfDoc, type PolicyRule, type Priority, type PropKind, type PropRow, type RuleValues, type WindowRow } from './ruleDoc';

const kindText: Record<PropKind, string> = { true: 'Is set (true)', false: 'Is false', number: 'Is the number', text: 'Is the text' };
const priorityText: Record<Priority, string> = {
  '': 'Not set by this rule',
  interactive: 'Interactive: someone is waiting, runs first',
  background: 'Background: can wait, runs after interactive calls',
};

/**
 * Creates a rule (for one grant, or with `grantId` null for the whole host) or edits one. The
 * fields are the rule format's: which calls (request types and models), and either refuse them
 * or give them background hours, a priority class and limits (req_001 D-15, D-25). Saving a
 * change to the name, position or rule stores a new rule version (D-87, D-132).
 */
export function RuleForm({
  rule,
  grantId,
  onSaved,
  onCancel,
}: {
  /** The rule to edit; absent for a new rule. */
  rule?: PolicyRule;
  /** The grant a new rule is for; null for the whole host. */
  grantId: string | null;
  onSaved: (r: PolicyRule) => void;
  onCancel: () => void;
}) {
  const client = useClient();
  const ids = useId();
  const [name, setName] = useState(rule?.name ?? '');
  const [position, setPosition] = useState(rule?.position === undefined ? '' : String(rule.position));
  const [v, setV] = useState<RuleValues>(() => (rule ? valuesOfDoc(rule.rule) : null) ?? emptyRule());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const [problems, setProblems] = useState<Record<string, string>>({});

  const set = (patch: Partial<RuleValues>) => setV((x) => ({ ...x, ...patch }));
  const setProp = (key: number, patch: Partial<PropRow>) => setV((x) => ({ ...x, props: x.props.map((r) => (r.key === key ? { ...r, ...patch } : r)) }));
  const setWindow = (key: number, patch: Partial<WindowRow>) => setV((x) => ({ ...x, windows: x.windows.map((w) => (w.key === key ? { ...w, ...patch } : w)) }));
  const serverError = (param: string) => (error?.param === param ? error.message : undefined);
  const title = rule ? 'Edit the rule' : grantId ? 'New rule for this grant' : 'New rule for the whole host';

  async function submit(e: FormEvent) {
    e.preventDefault();
    const { doc, problems: p } = docOfValues(v);
    const pos = wholeFrom(position, 0);
    if (pos.error) p.position = pos.error;
    if (rule && !name.trim()) p.name = 'Give the rule a name.';
    else if (name.trim().length > 120) p.name = 'The name must be at most 120 characters.';
    setProblems(p);
    if (!doc || Object.keys(p).length > 0) return;
    setBusy(true);
    setError(null);
    try {
      const shared = { ...(name.trim() ? { name: name.trim() } : {}), ...(pos.value !== undefined ? { position: pos.value } : {}), rule: doc };
      const saved = rule
        ? await call(client.PUT('/admin/v1/policy-rules/{policy_rule_id}', { params: { path: { policy_rule_id: rule.id } }, body: shared }))
        : await call(client.POST('/admin/v1/policy-rules', { body: { ...(grantId ? { grant_id: grantId } : {}), ...shared } }));
      onSaved(saved);
    } catch (err) {
      setError(asApiError(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} noValidate aria-label={title} className="flex flex-col gap-4 border-t border-slate-200 pt-3">
      <h3 className="text-sm font-semibold">{title}</h3>
      <p className="text-xs text-slate-600">
        {rule
          ? 'Saving a change to the name, position or rule stores a new version; earlier versions stay in the history. Switching a rule on or off keeps its version.'
          : 'A new rule is switched on at once.'}
      </p>
      <ErrorBanner error={error} title="The rule was not saved" />
      <div className="grid gap-3 sm:grid-cols-2">
        <Field
          label={rule ? 'Name' : 'Name (optional)'}
          hint={rule ? '1 to 120 characters.' : 'Up to 120 characters. Without one the rule is named by its id.'}
          value={name}
          onChange={(e) => setName(e.target.value)}
          error={problems.name ?? serverError('name')}
        />
        <Field
          label={rule ? 'Position' : 'Position (optional)'}
          hint={rule ? 'Rules are checked lowest first.' : 'Rules are checked lowest first. Empty means 0.'}
          inputMode="numeric"
          value={position}
          onChange={(e) => setPosition(e.target.value)}
          error={problems.position ?? serverError('position')}
        />
      </div>

      <fieldset className="flex flex-col gap-2">
        <legend className="text-sm font-semibold text-slate-900">Which calls</legend>
        <p className="text-xs text-slate-600">
          A call matches when it names every request type below with that value (in its LS-Request-Properties header) and asks for one of the
          models. With neither, the rule applies to every call.
        </p>
        {v.props.map((r, i) => (
          <div key={r.key} role="group" aria-label={`Request type ${i + 1}`} className="grid items-end gap-2 sm:grid-cols-[1fr_1fr_1fr_auto]">
            <Field label="Request type" value={r.name} onChange={(e) => setProp(r.key, { name: e.target.value })} error={problems[`props.${i}.name`]} />
            <div className="flex flex-col gap-1">
              <label htmlFor={`${ids}-k${r.key}`} className="text-sm font-medium text-slate-800">
                Matches when it
              </label>
              <select
                id={`${ids}-k${r.key}`}
                value={r.kind}
                onChange={(e) => setProp(r.key, { kind: e.target.value as PropKind })}
                className="rounded border border-slate-300 px-2 py-1 text-sm"
              >
                {(Object.keys(kindText) as PropKind[]).map((k) => (
                  <option key={k} value={k}>
                    {kindText[k]}
                  </option>
                ))}
              </select>
            </div>
            {r.kind === 'number' || r.kind === 'text' ? (
              <Field label="Value" value={r.value} onChange={(e) => setProp(r.key, { value: e.target.value })} error={problems[`props.${i}.value`]} />
            ) : (
              <span />
            )}
            <Button aria-label={`Remove request type ${i + 1}`} onClick={() => set({ props: v.props.filter((x) => x.key !== r.key) })}>
              Remove
            </Button>
          </div>
        ))}
        <div>
          <Button onClick={() => set({ props: [...v.props, { key: nextKey(), name: '', kind: 'true', value: '' }] })}>Add a request type</Button>
        </div>
        <Field
          label="Only these models (optional)"
          hint="Engine model names separated by commas. Empty means every model."
          value={v.models}
          onChange={(e) => set({ models: e.target.value })}
        />
      </fieldset>

      <fieldset className="flex flex-col gap-2">
        <legend className="text-sm font-semibold text-slate-900">What the rule does</legend>
        <label className="flex items-center gap-2 text-sm">
          <input type="radio" name={`${ids}-effect`} checked={v.effect === 'shape'} onChange={() => set({ effect: 'shape' })} />
          Set hours, a priority or limits for these calls
        </label>
        <label className="flex items-center gap-2 text-sm">
          <input type="radio" name={`${ids}-effect`} checked={v.effect === 'deny'} onChange={() => set({ effect: 'deny' })} />
          Refuse these calls
        </label>
        {problems.effect && <p className="text-xs text-red-700">{problems.effect}</p>}
      </fieldset>

      {v.effect === 'shape' && (
        <>
          <fieldset className="flex flex-col gap-2">
            <legend className="text-sm font-semibold text-slate-900">Background hours</legend>
            <CheckboxField
              label="Allow these calls only in set hours"
              checked={v.useWindows}
              onChange={(e) => set({ useWindows: e.target.checked, windows: e.target.checked && v.windows.length === 0 ? [{ key: nextKey(), start: '', end: '' }] : v.windows })}
            />
            {v.useWindows && (
              <>
                <p className="text-xs text-slate-600">
                  A matching call outside every window is refused with outside_time_window and told when the next window opens. These are not the
                  host's quiet hours.
                </p>
                <Field
                  label="Time zone"
                  hint="An IANA name such as Europe/Berlin, UTC, or Local for this machine's zone."
                  value={v.timeZone}
                  onChange={(e) => set({ timeZone: e.target.value })}
                  error={problems.timeZone}
                />
                {problems.windows && <p className="text-xs text-red-700">{problems.windows}</p>}
                {v.windows.map((w, i) => (
                  <div key={w.key} role="group" aria-label={`Window ${i + 1}`} className="grid items-end gap-2 sm:grid-cols-[1fr_1fr_auto]">
                    <Field label="From" hint="HH:MM, 24 hours" inputMode="numeric" value={w.start} onChange={(e) => setWindow(w.key, { start: e.target.value })} error={problems[`windows.${i}.start`]} />
                    <Field
                      label="Until"
                      hint="HH:MM; before the start means the window crosses midnight"
                      inputMode="numeric"
                      value={w.end}
                      onChange={(e) => setWindow(w.key, { end: e.target.value })}
                      error={problems[`windows.${i}.end`]}
                    />
                    <Button aria-label={`Remove window ${i + 1}`} onClick={() => set({ windows: v.windows.filter((x) => x.key !== w.key) })}>
                      Remove
                    </Button>
                  </div>
                ))}
                <div>
                  <Button onClick={() => set({ windows: [...v.windows, { key: nextKey(), start: '', end: '' }] })}>Add a window</Button>
                </div>
              </>
            )}
          </fieldset>

          <div className="flex flex-col gap-1">
            <label htmlFor={`${ids}-priority`} className="text-sm font-semibold text-slate-900">
              Priority
            </label>
            <select
              id={`${ids}-priority`}
              value={v.priority}
              onChange={(e) => set({ priority: e.target.value as Priority })}
              className="rounded border border-slate-300 px-2 py-1 text-sm sm:w-1/2"
            >
              {(Object.keys(priorityText) as Priority[]).map((k) => (
                <option key={k} value={k}>
                  {priorityText[k]}
                </option>
              ))}
            </select>
            <p className="text-xs text-slate-600">The first matching rule by position that sets a priority decides it; without one a call is interactive.</p>
          </div>

          <fieldset className="flex flex-col gap-2">
            <legend className="text-sm font-semibold text-slate-900">Limits</legend>
            <p className="text-xs text-slate-600">A rule can only narrow: each limit is the smallest of the grant's and every matching rule's. Empty sets none.</p>
            <div className="grid gap-3 sm:grid-cols-3">
              <Field label="Context limit (tokens)" inputMode="numeric" value={v.contextCap} onChange={(e) => set({ contextCap: e.target.value })} error={problems.contextCap} />
              <Field
                label="Most output tokens per call"
                inputMode="numeric"
                value={v.maxOutputTokens}
                onChange={(e) => set({ maxOutputTokens: e.target.value })}
                error={problems.maxOutputTokens}
              />
              <Field
                label="Requests at once"
                inputMode="numeric"
                value={v.maxConcurrency}
                onChange={(e) => set({ maxConcurrency: e.target.value })}
                error={problems.maxConcurrency}
              />
            </div>
          </fieldset>
        </>
      )}

      <div className="flex gap-2">
        <Button type="submit" variant="primary" disabled={busy}>
          {rule ? 'Save rule' : 'Create rule'}
        </Button>
        <Button disabled={busy} onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </form>
  );
}

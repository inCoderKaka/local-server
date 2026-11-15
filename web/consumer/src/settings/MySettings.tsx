import { useEffect, useState } from 'react';
import {
  asApiError,
  Button,
  call,
  Card,
  CheckboxField,
  ErrorBanner,
  Field,
  parseGoDuration,
  Spinner,
  useApi,
  useClient,
  type ApiError,
  type Schema,
} from '@local-server/shared';

type Setting = Schema<'Setting'>;
type Kind = 'bool' | 'int' | 'duration' | 'string';

/**
 * The kind of a setting from the JSON type of its default (a number for a whole number, true or
 * false for a switch, a string otherwise); a string default that reads as a Go duration makes it
 * a duration.
 */
function kindOf(s: Setting): Kind {
  const d = s.default_value ?? s.value;
  if (typeof d === 'boolean') return 'bool';
  if (typeof d === 'number') return 'int';
  if (typeof d === 'string' && d !== '' && parseGoDuration(d) !== null) return 'duration';
  return 'string';
}

function show(v: unknown): string {
  if (typeof v === 'boolean') return v ? 'On' : 'Off';
  if (v === '' || v === null || v === undefined) return '(empty)';
  return String(v);
}

function draftOf(s: Setting): string | boolean {
  if (typeof s.value === 'boolean') return s.value;
  return s.value === null || s.value === undefined ? '' : String(s.value);
}

const hints: Record<Kind, string | undefined> = {
  bool: undefined,
  int: 'A whole number.',
  duration: 'A duration such as 90s, 10m or 24h.',
  string: undefined,
};

/** The value to send, or an error when the draft does not fit the setting's kind. */
function parse(kind: Kind, draft: string | boolean): { value: unknown } | { error: string } {
  if (kind === 'bool') return { value: draft === true };
  const text = String(draft).trim();
  if (kind === 'int') {
    const n = Number(text);
    return text !== '' && Number.isSafeInteger(n) ? { value: n } : { error: 'Enter a whole number.' };
  }
  if (kind === 'duration') return parseGoDuration(text) !== null ? { value: text } : { error: 'Enter a duration such as 90s, 10m or 24h.' };
  return { value: String(draft) };
}

function SettingRow({ s, onSaved }: { s: Setting; onSaved: (list: Setting[]) => void }) {
  const client = useClient();
  const kind = kindOf(s);
  const [draft, setDraft] = useState<string | boolean>(() => draftOf(s));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | string | null>(null);
  const [status, setStatus] = useState('');
  // A new value from the host (this row's save or a reload) starts the field over.
  useEffect(() => setDraft(draftOf(s)), [s.key, s.value]);

  async function send(value: unknown, done: string) {
    setBusy(true);
    setError(null);
    setStatus('');
    try {
      onSaved((await call(client.PATCH('/v1/me/settings', { body: { values: { [s.key]: value } } }))).data);
      setStatus(done);
    } catch (e) {
      setError(asApiError(e));
    } finally {
      setBusy(false);
    }
  }

  function save() {
    const p = parse(kind, draft);
    if ('error' in p) setError(p.error);
    else void send(p.value, 'Saved.');
  }

  return (
    <li className="flex flex-col gap-2 border-b border-slate-100 py-3 text-sm last:border-b-0">
      <div className="flex flex-col gap-1">
        <code className="font-semibold">{s.key}</code>
        {s.description && <p className="text-slate-700">{s.description}</p>}
        <p className="text-slate-600">
          Now: <span className="text-slate-900">{show(s.value)}</span> · Default: <span className="text-slate-900">{show(s.default_value)}</span>
        </p>
      </div>
      <div className="flex flex-wrap items-end gap-2">
        {kind === 'bool' ? (
          <CheckboxField label={`New value for ${s.key}`} checked={draft === true} onChange={(e) => setDraft(e.target.checked)} />
        ) : (
          <Field
            label={`New value for ${s.key}`}
            hint={hints[kind]}
            type={kind === 'int' ? 'number' : 'text'}
            step={kind === 'int' ? 1 : undefined}
            autoComplete="off"
            value={String(draft)}
            onChange={(e) => setDraft(e.target.value)}
          />
        )}
        <Button variant="primary" disabled={busy} aria-label={`Save ${s.key}`} onClick={save}>
          Save
        </Button>
        <Button disabled={busy} aria-label={`Reset ${s.key} to default`} onClick={() => void send(null, 'Back to the default.')}>
          Reset to default
        </Button>
      </div>
      <ErrorBanner error={error} />
      <p role="status" aria-live="polite" className="text-emerald-900">
        {status}
      </p>
    </li>
  );
}

/**
 * The settings this consumer owns (GET and PATCH /v1/me/settings), each with its description,
 * value and default, from the host (req_001 D-38, D-54). A save answers the whole list, which
 * replaces the shown one.
 */
export function MySettings() {
  const client = useClient();
  const list = useApi(async () => (await call(client.GET('/v1/me/settings'))).data, [client]);
  return (
    <Card title="Your settings">
      <ErrorBanner error={list.error} title="Your settings could not be read" />
      {list.loading && !list.data && <Spinner label="Loading your settings" />}
      {list.data && list.data.length === 0 && <p className="text-sm text-slate-600">This host has no settings for you to change.</p>}
      {list.data && list.data.length > 0 && (
        <ul aria-label="Your settings" className="flex flex-col">
          {list.data.map((s) => (
            <SettingRow key={s.key} s={s} onSaved={list.setData} />
          ))}
        </ul>
      )}
    </Card>
  );
}

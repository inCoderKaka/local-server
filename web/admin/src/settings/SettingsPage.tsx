import { useEffect, useMemo, useState } from 'react';
import {
  asApiError,
  Button,
  call,
  Card,
  CheckboxField,
  ErrorBanner,
  Field,
  formatBytes,
  parseGoDuration,
  Spinner,
  useClient,
  type ApiError,
} from '@local-server/shared';
import { useSettings, type Setting } from '../lib/settings';

/** The setting D-95 explains; owned by the scheduler. */
export const OLLAMA_NUM_PARALLEL_KEY = 'scheduler.ollama_num_parallel';

type Kind = 'bool' | 'int' | 'duration' | 'string';

/**
 * The kind of a setting from the JSON type of its default (the API sends a number for a whole
 * number, true or false for a switch, and a string otherwise); a string default that reads as a
 * Go duration makes it a duration.
 */
function kindOf(s: Setting): Kind {
  const d = s.default_value ?? s.value;
  if (typeof d === 'boolean') return 'bool';
  if (typeof d === 'number') return 'int';
  if (typeof d === 'string' && d !== '' && parseGoDuration(d) !== null) return 'duration';
  return 'string';
}

/**
 * Whether a key names a secret, by the same words the host uses to keep a value out of the audit
 * trail. Its value is never shown, never put in a field and only sent when the host types a new
 * one (req_001 D-31, D-40).
 */
export function isSecretKey(key: string): boolean {
  const k = key.toLowerCase();
  return ['secret', 'token', 'private', 'apikey', 'api_key'].some((w) => k.includes(w));
}

/** The unit a key names by its suffix, if any. */
function unitOf(key: string): string | null {
  if (key.endsWith('_bytes')) return 'bytes';
  if (key.endsWith('_seconds')) return 'seconds';
  if (key.endsWith('_kib')) return 'KiB';
  if (key.endsWith('_percent')) return 'percent';
  return null;
}

function show(s: Setting, v: unknown): string {
  if (typeof v === 'boolean') return v ? 'On' : 'Off';
  if (v === '' || v === null || v === undefined) return '(empty)';
  const unit = unitOf(s.key);
  if (typeof v === 'number' && unit === 'bytes') return `${v} bytes (${formatBytes(v)})`;
  if (unit) return `${String(v)} ${unit}`;
  return String(v);
}

function draftOf(s: Setting): string | boolean {
  if (isSecretKey(s.key)) return '';
  if (typeof s.value === 'boolean') return s.value;
  return s.value === null || s.value === undefined ? '' : String(s.value);
}

const hints: Record<Kind, string | null> = {
  bool: null,
  int: 'A whole number.',
  duration: 'A duration such as 90s, 10m or 24h.',
  string: null,
};

/** The value to send, or an error message when the draft does not fit the setting's kind. */
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

/** D-95: both parallel settings have to be raised together. */
export function OllamaParallelNote() {
  return (
    <div className="rounded border border-sky-200 bg-sky-50 px-3 py-2 text-sm text-sky-900">
      <p className="font-semibold">Running more requests at once on Ollama</p>
      <p>
        Raise two values together, to the same number: Ollama's <code>OLLAMA_NUM_PARALLEL</code> environment variable and local-server's{' '}
        <code>{OLLAMA_NUM_PARALLEL_KEY}</code> setting. Raising only one has no effect on throughput.
      </p>
      <ol className="list-decimal pl-5">
        <li>
          Set <code>OLLAMA_NUM_PARALLEL</code> in the environment where Ollama runs, then restart Ollama.
        </li>
        <li>
          Set <code>{OLLAMA_NUM_PARALLEL_KEY}</code> here to the same value. local-server never runs more requests at once than this, and it can do no
          more than what Ollama actually serves.
        </li>
      </ol>
    </div>
  );
}

function SettingRow({ s }: { s: Setting }) {
  const client = useClient();
  const { replace } = useSettings();
  const kind = kindOf(s);
  const secret = isSecretKey(s.key);
  const [draft, setDraft] = useState<string | boolean>(() => draftOf(s));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | string | null>(null);
  // A new value from the API (this row's save or a reload) starts the field over; other rows keep their drafts.
  useEffect(() => setDraft(draftOf(s)), [s.key, s.value]);

  async function send(value: unknown) {
    setBusy(true);
    setError(null);
    try {
      const res = await call(client.PATCH('/admin/v1/settings', { body: { values: { [s.key]: value } } }));
      replace(res.data);
    } catch (e) {
      setError(asApiError(e));
    } finally {
      setBusy(false);
    }
  }

  function save() {
    const p = parse(kind, draft);
    if ('error' in p) setError(p.error);
    else void send(p.value);
  }

  const unit = unitOf(s.key);
  const hint = [hints[kind], unit ? `In ${unit}.` : null, secret ? 'Hidden: type a new value to replace it.' : null].filter(Boolean).join(' ');
  return (
    <li className="flex flex-col gap-2 border-b border-slate-100 py-3 text-sm last:border-b-0">
      <div className="flex flex-col gap-1">
        <code className="font-semibold">{s.key}</code>
        {s.description && <p className="text-slate-700">{s.description}</p>}
        <p className="text-slate-600">
          Current: <span className="text-slate-900">{secret ? 'Hidden' : show(s, s.value)}</span> · Default:{' '}
          <span className="text-slate-900">{secret ? 'Hidden' : show(s, s.default_value)}</span>
        </p>
      </div>
      {s.key === OLLAMA_NUM_PARALLEL_KEY && <OllamaParallelNote />}
      <div className="flex flex-wrap items-end gap-2">
        {kind === 'bool' ? (
          <CheckboxField label={`New value for ${s.key}`} checked={draft === true} onChange={(e) => setDraft(e.target.checked)} />
        ) : (
          <Field
            label={`New value for ${s.key}`}
            hint={hint || undefined}
            type={secret ? 'password' : kind === 'int' ? 'number' : 'text'}
            step={kind === 'int' ? 1 : undefined}
            autoComplete={secret ? 'new-password' : 'off'}
            value={String(draft)}
            onChange={(e) => setDraft(e.target.value)}
          />
        )}
        <Button variant="primary" disabled={busy || (secret && draft === '')} aria-label={`Save ${s.key}`} onClick={save}>
          Save
        </Button>
        <Button disabled={busy} aria-label={`Reset ${s.key} to default`} onClick={() => void send(null)}>
          Reset to default
        </Button>
      </div>
      <ErrorBanner error={error} />
    </li>
  );
}

/** The group a key belongs to: its first dotted part. */
function groupOf(key: string): string {
  const i = key.indexOf('.');
  return i > 0 ? key.slice(0, i) : key;
}

/**
 * Every host-owned setting, grouped by the first part of its key, each with its description,
 * value, default and a control to change or reset it (req_001 D-38, D-54). Saving one replaces
 * the shell's settings with the API's answer, so values the shell derives follow at once.
 */
export default function SettingsPage() {
  const { list, loading, reload } = useSettings();
  const groups = useMemo(() => {
    const m = new Map<string, Setting[]>();
    for (const s of list ?? []) {
      const g = groupOf(s.key);
      m.set(g, [...(m.get(g) ?? []), s]);
    }
    return [...m.entries()].sort(([a], [b]) => a.localeCompare(b));
  }, [list]);
  const hasParallel = (list ?? []).some((s) => s.key === OLLAMA_NUM_PARALLEL_KEY);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-2">
        <h1 className="text-xl font-semibold">Settings</h1>
        <Button onClick={() => void reload()}>Refresh</Button>
      </div>
      <p className="text-sm text-slate-700">
        Every time, limit and retention value the host owns. Reset to default returns a setting to the default shown. Each change is recorded in the audit
        log.
      </p>
      {!hasParallel && <OllamaParallelNote />}
      {loading && !list && <Spinner label="Loading the settings" />}
      {groups.map(([g, items]) => (
        <Card key={g} title={g}>
          <ul>
            {items.map((s) => (
              <SettingRow key={s.key} s={s} />
            ))}
          </ul>
        </Card>
      ))}
    </div>
  );
}

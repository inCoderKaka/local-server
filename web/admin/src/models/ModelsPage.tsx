import { useState, type FormEvent } from 'react';
import {
  asApiError,
  Badge,
  Button,
  call,
  Card,
  ErrorBanner,
  formatBytes,
  Spinner,
  Table,
  useApi,
  UserText,
  useClient,
  type ApiError,
  type Column,
  type Schema,
} from '@local-server/shared';
import { boolSetting, formatSeconds, HOST_SETTING, secondsFrom, textSetting } from '../grants/values';
import { useSettings } from '../lib/settings';

type HostModel = Schema<'HostModel'>;
type HostModelUpdate = Schema<'HostModelUpdate'>;

/** The keep-alive of one model: empty is the host's default, otherwise a duration. */
function KeepAlive({ model, busy, onSave }: { model: HostModel; busy: boolean; onSave: (seconds: number | null) => Promise<boolean> }) {
  const settings = useSettings();
  const stored = model.keep_alive_seconds == null ? '' : formatSeconds(model.keep_alive_seconds);
  const [text, setText] = useState(stored);
  const [problem, setProblem] = useState<string | null>(null);
  const hostDefault = textSetting(settings.list, HOST_SETTING.keepAlive);

  async function submit(e: FormEvent) {
    e.preventDefault();
    let seconds: number | null = null;
    if (text.trim() !== '') {
      const s = secondsFrom(text);
      if (s.error) {
        setProblem(s.error);
        return;
      }
      seconds = s.value!;
    }
    setProblem(null);
    if (await onSave(seconds)) setText(seconds === null ? '' : formatSeconds(seconds));
  }

  return (
    <form onSubmit={submit} noValidate aria-label={`Keep-alive of ${model.name}`} className="flex flex-col gap-1">
      <div className="flex items-center gap-2">
        <input
          aria-label={`Keep-alive for ${model.name}`}
          aria-invalid={problem ? true : undefined}
          value={text}
          placeholder={hostDefault ? `Host default (${hostDefault})` : 'Host default'}
          onChange={(e) => setText(e.target.value)}
          className="w-36 rounded border border-slate-300 px-2 py-1 text-sm aria-invalid:border-red-600"
        />
        <Button type="submit" aria-label={`Save keep-alive for ${model.name}`} disabled={busy || text === stored}>
          Save
        </Button>
      </div>
      {problem && <p className="text-xs text-red-700">{problem}</p>}
    </form>
  );
}

/**
 * The host's models: whether new grants may include each one (shared), whether it is kept loaded
 * (preload), its keep-alive, and whether it is loaded now (req_001 D-22, D-25).
 */
export default function ModelsPage() {
  const client = useClient();
  const settings = useSettings();
  const models = useApi(async () => (await call(client.GET('/admin/v1/models'))).data, [client]);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<{ model: HostModel; error: ApiError } | null>(null);
  const defaultAllowed = boolSetting(settings.list, HOST_SETTING.modelDefaultAllowed);
  const hostKeepAlive = textSetting(settings.list, HOST_SETTING.keepAlive);

  async function update(m: HostModel, body: HostModelUpdate): Promise<boolean> {
    setBusy(m.id);
    setError(null);
    try {
      const changed = await call(client.PATCH('/admin/v1/models/{model_id}', { params: { path: { model_id: m.id } }, body }));
      models.setData((models.data ?? []).map((x) => (x.id === changed.id ? changed : x)));
      return true;
    } catch (e) {
      setError({ model: m, error: asApiError(e) });
      return false;
    } finally {
      setBusy(null);
    }
  }

  const columns: Column<HostModel>[] = [
    { key: 'name', header: 'Model', render: (m) => <UserText text={m.name} /> },
    { key: 'engine', header: 'Engine', render: (m) => <UserText text={m.engine} /> },
    { key: 'loaded', header: 'Now', render: (m) => (m.loaded ? <Badge tone="good">Loaded</Badge> : <Badge tone="neutral">Not loaded</Badge>) },
    { key: 'size', header: 'Size', render: (m) => formatBytes(m.size_bytes) },
    {
      key: 'shared',
      header: 'Shared',
      render: (m) => (
        <input
          type="checkbox"
          className="h-4 w-4"
          aria-label={`Share ${m.name}`}
          checked={m.allowed}
          disabled={busy === m.id}
          onChange={(e) => void update(m, { allowed: e.target.checked })}
        />
      ),
    },
    {
      key: 'preload',
      header: 'Preload',
      render: (m) => (
        <input
          type="checkbox"
          className="h-4 w-4"
          aria-label={`Preload ${m.name}`}
          checked={m.preload}
          disabled={busy === m.id}
          onChange={(e) => void update(m, { preload: e.target.checked })}
        />
      ),
    },
    {
      key: 'keep',
      header: 'Keep-alive',
      render: (m) => <KeepAlive key={`${m.id}-${m.keep_alive_seconds ?? 'default'}`} model={m} busy={busy === m.id} onSave={(s) => update(m, { keep_alive_seconds: s })} />,
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Models</h1>
      <Card title="Host models">
        <div className="flex flex-col gap-3">
          <ul className="list-disc pl-5 text-sm text-slate-700">
            <li>
              Shared: a grant may include the model. Turning it off stops new grant versions from including it; grants that hold it already keep
              it.
              {defaultAllowed === true && ' A model the host sees for the first time is shared at once (host setting admin.model_default_allowed).'}
              {defaultAllowed === false && ' A model the host sees for the first time is not shared until you share it.'}
            </li>
            <li>Preload: the model is loaded when the host starts and kept loaded; it is never unloaded for being idle or to make room.</li>
            <li>
              Keep-alive: how long an idle model stays loaded, as a duration such as 5m or 1h. Empty means the host's default
              {hostKeepAlive ? ` (now ${hostKeepAlive})` : ''}. The value is stored per model, but this version of the host still unloads every
              idle model after its default keep-alive (host setting scheduler.keep_alive).
            </li>
          </ul>
          <ErrorBanner error={models.error} title="The models could not be read" />
          {error && (
            <ErrorBanner
              error={error.error}
              title={
                <>
                  The change to <UserText text={error.model.name} /> was not saved
                </>
              }
            />
          )}
          {models.loading && !models.data && <Spinner label="Loading models" />}
          {models.data && <Table caption="Models the engine has" columns={columns} rows={models.data} rowKey={(m) => m.id} empty="The engine has no models." />}
        </div>
      </Card>
    </div>
  );
}

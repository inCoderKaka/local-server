import { Badge, Card } from '@local-server/shared';
import { useShell } from '../lib/shell';

/** The engine and the tailnet as small status cards, from GET /admin/v1/host. */
export function StatusCards() {
  const h = useShell().host.data;
  if (!h) return null;
  const t = h.tailnet;
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      <Card title="Engine">
        <p className="text-sm">
          <Badge tone={h.engine_state === 'available' ? 'good' : 'bad'}>{h.engine_state === 'available' ? 'Answers' : 'Does not answer'}</Badge>
        </p>
        <p className="mt-2 text-sm">Version: {h.engine_version ?? 'n/a'}</p>
        <p className="text-xs text-slate-600">local-server {h.software_version}</p>
      </Card>
      <Card title="Tailnet">
        {t ? (
          <>
            <p className="text-sm">
              <Badge tone={t.state === 'ready' ? 'good' : 'warn'}>{t.state}</Badge>
            </p>
            {t.message && <p className="mt-2 text-sm">{t.message}</p>}
            {t.fix && <p className="mt-1 text-sm text-slate-700">What to do: {t.fix}</p>}
            {t.dns_name && <p className="mt-1 text-xs text-slate-600">{t.dns_name}</p>}
          </>
        ) : (
          <p className="text-sm text-slate-600">n/a</p>
        )}
      </Card>
    </div>
  );
}

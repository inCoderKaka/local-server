import { useId, useState } from 'react';
import { asApiError, call, Card, ErrorBanner, formatDateTime, useClient, type ApiError } from '@local-server/shared';
import { sharingLabel } from '../layout/SharingBadge';
import { useShell } from '../lib/shell';

/**
 * The pause / resume switch (req_001 D-06): pausing stops new work at once, running requests
 * finish and queued jobs wait. The sleep warning is always shown (D-30).
 */
export function SharingCard() {
  const client = useClient();
  const { host } = useShell();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const labelId = useId();
  const h = host.data;
  const paused = h?.sharing_state === 'paused';

  async function toggle() {
    setBusy(true);
    setError(null);
    try {
      host.setData(await call(paused ? client.POST('/admin/v1/host/resume') : client.POST('/admin/v1/host/pause')));
    } catch (e) {
      setError(asApiError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card title="Sharing">
      <div className="flex flex-col gap-3 text-sm">
        <ErrorBanner error={error} />
        {h && (
          <>
            <div className="flex items-center gap-3">
              <button
                type="button"
                role="switch"
                aria-checked={!paused}
                aria-labelledby={labelId}
                disabled={busy}
                onClick={() => void toggle()}
                className={`relative h-6 w-11 rounded-full transition-colors ${paused ? 'bg-slate-300' : 'bg-emerald-600'} disabled:opacity-50`}
              >
                <span
                  aria-hidden="true"
                  className={`absolute top-0.5 h-5 w-5 rounded-full bg-white shadow transition-all ${paused ? 'left-0.5' : 'left-5'}`}
                />
              </button>
              <span id={labelId}>Share this host with consumers</span>
            </div>
            <p>
              State: <strong>{sharingLabel[h.sharing_state]}</strong>
              {paused && <> since {formatDateTime(h.paused_at)}</>}
              {h.sharing_state === 'quiet' && <> until {formatDateTime(h.sharing_resumes_at)}</>}
            </p>
            {paused && <p className="text-slate-600">Running requests finish; queued jobs wait until sharing resumes.</p>}
            {h.sharing_state === 'quiet' && (
              <p className="text-slate-600">Quiet hours are on: the host serves again when they end. Pausing still works during quiet hours.</p>
            )}
          </>
        )}
        <p role="note" className="rounded border border-amber-300 bg-amber-50 px-3 py-2 text-amber-900">
          Closing the lid or putting this machine to sleep cannot be blocked: while it sleeps, it stops serving consumers.
        </p>
      </div>
    </Card>
  );
}

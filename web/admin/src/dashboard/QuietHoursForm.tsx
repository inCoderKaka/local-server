import { useEffect, useState, type FormEvent } from 'react';
import { asApiError, Button, call, Card, CheckboxField, ErrorBanner, Field, useClient, type ApiError } from '@local-server/shared';
import { SETTING } from '../lib/settingKeys';
import { settingOf, useSettings, type Setting } from '../lib/settings';
import { useShell } from '../lib/shell';

interface Values {
  enabled: boolean;
  timeZone: string;
  start: string;
  end: string;
}

function valuesOf(list: Setting[] | undefined): Values | null {
  if (!list) return null;
  const v = (k: string) => settingOf(list, k)?.value;
  return {
    enabled: v(SETTING.quietEnabled) === true,
    timeZone: String(v(SETTING.quietTimeZone) ?? ''),
    start: String(v(SETTING.quietStart) ?? ''),
    end: String(v(SETTING.quietEnd) ?? ''),
  };
}

/**
 * Quiet hours: a daily window in which the host stops serving (req_001 D-06). The four
 * host-owned settings are written together; the server checks them and its refusal is shown.
 */
export function QuietHoursForm() {
  const client = useClient();
  const settings = useSettings();
  const { host } = useShell();
  const [form, setForm] = useState<Values | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [saved, setSaved] = useState(false);
  const [busy, setBusy] = useState(false);

  // Start from the stored values; a later reload does not overwrite what is being typed.
  useEffect(() => {
    if (!form) setForm(valuesOf(settings.list));
  }, [settings.list, form]);

  if (!form) return null;
  const set = (patch: Partial<Values>) => {
    setSaved(false);
    setForm({ ...form, ...patch });
  };
  const fieldError = (key: string) => (error?.param === key ? error.message : undefined);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!form) return;
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      const res = await call(
        client.PATCH('/admin/v1/settings', {
          body: {
            values: {
              [SETTING.quietEnabled]: form.enabled,
              [SETTING.quietTimeZone]: form.timeZone,
              [SETTING.quietStart]: form.start,
              [SETTING.quietEnd]: form.end,
            },
          },
        }),
      );
      settings.replace(res.data);
      setForm(valuesOf(res.data));
      setSaved(true);
      void host.refetch();
    } catch (err) {
      setError(asApiError(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card title="Quiet hours">
      <form onSubmit={submit} aria-label="Quiet hours" className="flex flex-col gap-3">
        <p className="text-sm text-slate-700">Every day in this window the host stops serving, as if paused, and lets the machine sleep.</p>
        <ErrorBanner error={error} title="The quiet hours were not saved" />
        <CheckboxField label="Use quiet hours" checked={form.enabled} onChange={(e) => set({ enabled: e.target.checked })} />
        <div className="grid gap-3 sm:grid-cols-3">
          <Field
            label="Time zone"
            hint="An IANA name such as Europe/Berlin, or Local for this machine's zone."
            value={form.timeZone}
            onChange={(e) => set({ timeZone: e.target.value })}
            error={fieldError(SETTING.quietTimeZone)}
          />
          <Field
            label="Start"
            hint="HH:MM, 24 hours"
            inputMode="numeric"
            value={form.start}
            onChange={(e) => set({ start: e.target.value })}
            error={fieldError(SETTING.quietStart)}
          />
          <Field
            label="End"
            hint="HH:MM; before the start means the window crosses midnight"
            inputMode="numeric"
            value={form.end}
            onChange={(e) => set({ end: e.target.value })}
            error={fieldError(SETTING.quietEnd)}
          />
        </div>
        <div className="flex items-center gap-3">
          <Button type="submit" variant="primary" disabled={busy}>
            Save quiet hours
          </Button>
          {saved && (
            <span role="status" className="text-sm text-emerald-800">
              Saved.
            </span>
          )}
        </div>
      </form>
    </Card>
  );
}

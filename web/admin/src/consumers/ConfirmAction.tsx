import { useState, type ReactNode } from 'react';
import { asApiError, Button, ErrorBanner, type ApiError } from '@local-server/shared';

/**
 * A button for an action that cannot be undone. The first press only opens a confirmation that
 * says what will happen; the action runs on the second, explicit press. While it runs both
 * buttons are disabled; a refusal is shown inside the confirmation.
 */
export function ConfirmAction({
  label,
  name,
  title,
  confirmLabel,
  children,
  onConfirm,
  disabled,
}: {
  /** The text of the first button. */
  label: string;
  /** The accessible name of the first button, when its text alone is not unique (a table row). */
  name?: string;
  /** The title of the confirmation; it names the confirmation for assistive technology. */
  title: string;
  /** The text of the button that runs the action. */
  confirmLabel: string;
  /** What will happen. */
  children: ReactNode;
  onConfirm: () => Promise<void>;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  if (!open) {
    return (
      <Button variant="danger" aria-label={name} disabled={disabled} onClick={() => setOpen(true)}>
        {label}
      </Button>
    );
  }

  async function confirm() {
    setBusy(true);
    setError(null);
    try {
      await onConfirm();
      setOpen(false);
    } catch (e) {
      setError(asApiError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div role="group" aria-label={title} className="flex flex-col gap-2 rounded border border-red-300 bg-red-50 p-3 text-sm text-red-950">
      <p className="font-semibold">{title}</p>
      <div>{children}</div>
      <ErrorBanner error={error} />
      <div className="flex gap-2">
        <Button variant="danger" disabled={busy} onClick={() => void confirm()}>
          {confirmLabel}
        </Button>
        <Button
          disabled={busy}
          onClick={() => {
            setOpen(false);
            setError(null);
          }}
        >
          Cancel
        </Button>
      </div>
    </div>
  );
}

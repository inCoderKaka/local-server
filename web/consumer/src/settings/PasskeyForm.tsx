import { useState } from 'react';
import { asApiError, Button, call, Card, ErrorBanner, useClient } from '@local-server/shared';
import { createPasskey, PasskeyError, passkeysSupported } from '../lib/webauthn';

/**
 * Adds a passkey to the account (POST /v1/me/passkeys/begin, then the browser makes the passkey,
 * then /finish), so this device can log in without a password (req_001 D-13). Every WebAuthn
 * option, the timeout included, is the host's.
 */
export function PasskeyForm() {
  const client = useClient();
  const canPasskey = passkeysSupported();
  const [problem, setProblem] = useState<unknown>(null);
  const [status, setStatus] = useState('');
  const [busy, setBusy] = useState(false);

  async function add() {
    setBusy(true);
    setProblem(null);
    try {
      const ceremony = await call(client.POST('/v1/me/passkeys/begin'));
      setStatus("Follow your browser's prompt to make the passkey.");
      const credential = await createPasskey(ceremony.options);
      await call(client.POST('/v1/me/passkeys/finish', { body: { ceremony_id: ceremony.ceremony_id, credential } }));
      setStatus('The passkey is added. You can log in with it on this device.');
    } catch (err) {
      setStatus('');
      setProblem(err instanceof PasskeyError ? err : asApiError(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card title="Passkeys">
      <div className="flex flex-col gap-3 text-sm">
        <p>A passkey lets you log in with this device&apos;s screen lock, fingerprint or face, or a security key, instead of a password.</p>
        <ErrorBanner error={problem} title={problem ? 'The passkey was not added' : undefined} />
        {canPasskey ? (
          <div>
            <Button variant="primary" disabled={busy} onClick={() => void add()}>
              Add a passkey
            </Button>
          </div>
        ) : (
          <p className="text-slate-600">This browser cannot use passkeys.</p>
        )}
        <p role="status" aria-live="polite" className="text-slate-700">
          {status}
        </p>
      </div>
    </Card>
  );
}

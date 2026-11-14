import { useState, type FormEvent } from 'react';
import { asApiError, Button, call, ErrorBanner, Field, useClient, type ApiError, type Schema } from '@local-server/shared';
import { ExpiryChoice, expiryValue, noExpiry, type Expiry } from './ExpiryChoice';

export type IssuedKey = Schema<'AccessKeyIssued'>;

/**
 * Issues an access key for a consumer: an optional name, an optional expiry (none means the key
 * never expires) and an optional tailnet node it is bound to (req_001 D-25, D-39). The answer,
 * with its secret, goes to `onIssued` and nowhere else.
 */
export function IssueKeyForm({ consumerId, onIssued }: { consumerId: string; onIssued: (k: IssuedKey) => void }) {
  const client = useClient();
  const [name, setName] = useState('');
  const [expiry, setExpiry] = useState<Expiry>(noExpiry);
  const [node, setNode] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const [expiryError, setExpiryError] = useState<string | undefined>();

  const fieldError = (param: string) => (error?.param === param ? error.message : undefined);

  async function submit(e: FormEvent) {
    e.preventDefault();
    const exp = expiryValue(expiry);
    setExpiryError(exp.error);
    if (exp.error) return;
    setBusy(true);
    setError(null);
    try {
      const issued = await call(
        client.POST('/admin/v1/consumers/{consumer_id}/access-keys', {
          params: { path: { consumer_id: consumerId } },
          body: {
            ...(name.trim() ? { name: name.trim() } : {}),
            ...(exp.value ? { expires_at: exp.value } : {}),
            ...(node.trim() ? { bound_node: node.trim() } : {}),
          },
        }),
      );
      setName('');
      setExpiry(noExpiry);
      setNode('');
      onIssued(issued);
    } catch (err) {
      setError(asApiError(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} noValidate aria-label="Issue an access key" className="flex flex-col gap-3 border-t border-slate-200 pt-3">
      <h3 className="text-sm font-semibold">Issue an access key</h3>
      <ErrorBanner error={error} title="The key was not issued" />
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="Key name (optional)" hint="A label to recognise the key, for example the app or machine it is for." value={name} onChange={(e) => setName(e.target.value)} error={fieldError('name')} />
        <Field
          label="Bind to a tailnet node (optional)"
          hint="Its tailnet address, node ID, machine name or MagicDNS name. The key then works only from that node."
          value={node}
          onChange={(e) => setNode(e.target.value)}
          error={fieldError('bound_node')}
        />
      </div>
      <ExpiryChoice value={expiry} onChange={setExpiry} defaultLabel="Never" error={expiryError ?? fieldError('expires_at')} />
      <div>
        <Button type="submit" variant="primary" disabled={busy}>
          Issue key
        </Button>
      </div>
    </form>
  );
}

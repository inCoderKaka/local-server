import { useState, type FormEvent } from 'react';
import { asApiError, Button, call, Card, ErrorBanner, Field, formatDateTime, useClient, type ApiError, type Schema } from '@local-server/shared';
import { ExpiryChoice, expiryValue, noExpiry, type Expiry } from '../consumers/ExpiryChoice';
import { ShownOnce } from '../consumers/ShownOnce';
import { settingOf, useSettings } from '../lib/settings';

/** The host setting that sets how long an invite without an expiry stays valid. */
const INVITE_TTL = 'invites.ttl';
/** The host setting put in front of the invite path; empty gives a relative link. */
const LINK_BASE_URL = 'invites.link_base_url';

type Issued = Schema<'InviteIssued'>;

/**
 * Creates a one-time sign-up invite (req_001 D-13). Its link, which carries the token after `#`,
 * is shown once and is held only in this component's state: the host keeps only a hash.
 */
export function CreateInviteForm({ onCreated }: { onCreated: () => void }) {
  const client = useClient();
  const settings = useSettings();
  const [note, setNote] = useState('');
  const [expiry, setExpiry] = useState<Expiry>(noExpiry);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const [expiryError, setExpiryError] = useState<string | undefined>();
  const [issued, setIssued] = useState<Issued | null>(null);

  const ttl = settingOf(settings.list, INVITE_TTL)?.value;
  const defaultLabel = typeof ttl === 'string' && ttl ? `The host's invite validity (setting ${INVITE_TTL}: ${ttl})` : `The host's invite validity (setting ${INVITE_TTL})`;
  const fieldError = (param: string) => (error?.param === param ? error.message : undefined);

  async function submit(e: FormEvent) {
    e.preventDefault();
    const exp = expiryValue(expiry);
    setExpiryError(exp.error);
    if (exp.error) return;
    setBusy(true);
    setError(null);
    try {
      const res = await call(
        client.POST('/admin/v1/invites', {
          body: { ...(note.trim() ? { note: note.trim() } : {}), ...(exp.value ? { expires_at: exp.value } : {}) },
        }),
      );
      setNote('');
      setExpiry(noExpiry);
      setIssued(res);
      onCreated();
    } catch (err) {
      setError(asApiError(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card title="New invite">
      <div className="flex flex-col gap-3">
        {issued && (
          <ShownOnce title="Invite link" label="Invite link" value={issued.link} onDismiss={() => setIssued(null)}>
            <p>
              Send this link to the person you invite. It works once, until {formatDateTime(issued.invite.expires_at)}. It is shown only now: the
              host keeps only a hash of its token.
            </p>
            {issued.link.startsWith('/') && (
              <p className="mt-1">
                The link is relative because the host setting {LINK_BASE_URL} is empty: put the address of the consumer web page in front of it.
              </p>
            )}
          </ShownOnce>
        )}
        <form onSubmit={submit} noValidate aria-label="New invite" className="flex flex-col gap-3">
          <p className="text-sm text-slate-700">An invite lets one person sign up as a consumer with a login of their own.</p>
          <ErrorBanner error={error} title="The invite was not created" />
          <Field label="Note (optional)" hint="For you, for example who the invite is for." value={note} onChange={(e) => setNote(e.target.value)} error={fieldError('note')} />
          <ExpiryChoice value={expiry} onChange={setExpiry} defaultLabel={defaultLabel} error={expiryError ?? fieldError('expires_at')} />
          <div>
            <Button type="submit" variant="primary" disabled={busy}>
              Create invite
            </Button>
          </div>
        </form>
      </div>
    </Card>
  );
}

import { useState, type FormEvent } from 'react';
import { asApiError, Button, call, Card, ErrorBanner, Field, Link, useClient } from '@local-server/shared';
import { inviteTokenFrom } from '../lib/invite';
import { privacy, terms } from '../lib/legal';
import type { Consumer } from '../lib/session';
import { createPasskey, PasskeyError, passkeysSupported } from '../lib/webauthn';
import { Consent } from './Consent';

type Method = 'password' | 'passkey';

/**
 * Sign-up from an invite (req_001 D-13): the invite code (filled in from an opened invite link),
 * a name, an optional email, the way to log in (a password, or a passkey made by this browser)
 * and the acceptance of the Terms of Use and the Privacy and consent notice (req_001 D-44, D-45).
 * The host checks the password policy and the invite; its refusals are shown as it words them.
 */
export function SignupPage({ initialToken, onSignedIn }: { initialToken: string | null; onSignedIn: (c: Consumer) => void }) {
  const client = useClient();
  const canPasskey = passkeysSupported();
  const [token, setToken] = useState(initialToken ?? '');
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [method, setMethod] = useState<Method>('password');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [repeat, setRepeat] = useState('');
  const [acceptTerms, setAcceptTerms] = useState(false);
  const [acceptPrivacy, setAcceptPrivacy] = useState(false);
  const [problem, setProblem] = useState<unknown>(null);
  const [status, setStatus] = useState('');
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    const invite = inviteTokenFrom(token);
    if (!invite) {
      setProblem('Enter the invite code from your invite link.');
      return;
    }
    if (!acceptTerms || !acceptPrivacy) {
      setProblem('To sign up, accept both the Terms of Use and the Privacy and consent notice.');
      return;
    }
    if (method === 'password' && password !== repeat) {
      setProblem('The two passwords differ.');
      return;
    }
    const consent = { terms_version: terms.version, privacy_version: privacy.version };
    const contact = email.trim() ? { email: email.trim() } : {};
    setProblem(null);
    setBusy(true);
    try {
      if (method === 'password') {
        onSignedIn(
          await call(
            client.POST('/v1/invite-redemptions/password', {
              body: { invite_token: invite, display_name: name, username, password, consent, ...contact },
            }),
          ),
        );
        return;
      }
      const ceremony = await call(
        client.POST('/v1/invite-redemptions/passkey/begin', { body: { invite_token: invite, display_name: name, consent, ...contact } }),
      );
      setStatus("Follow your browser's prompt to make the passkey.");
      const credential = await createPasskey(ceremony.options);
      onSignedIn(await call(client.POST('/v1/invite-redemptions/passkey/finish', { body: { ceremony_id: ceremony.ceremony_id, credential } })));
    } catch (err) {
      setProblem(err instanceof PasskeyError ? err : asApiError(err));
      setPassword('');
      setRepeat('');
    } finally {
      setStatus('');
      setBusy(false);
    }
  }

  return (
    <div className="mx-auto mt-12 max-w-lg px-4">
      <Card title="Sign up with your invite">
        <form onSubmit={submit} className="flex flex-col gap-3" aria-label="Sign up">
          <ErrorBanner error={problem} />
          <Field
            label="Invite code"
            required
            autoComplete="off"
            spellCheck={false}
            value={token}
            onChange={(e) => setToken(e.target.value)}
            hint={initialToken ? 'Filled in from your invite link.' : 'From your invite link. You can paste the whole link.'}
          />
          <Field label="Your name" required autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} hint="Shown to the host." />
          <Field
            label="Email (optional)"
            type="email"
            autoComplete="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            hint="Only the host sees it."
          />
          <fieldset className="flex flex-col gap-1">
            <legend className="mb-1 text-sm font-medium text-slate-800">How you will log in</legend>
            <label className="flex items-center gap-2 text-sm text-slate-800">
              <input type="radio" name="method" checked={method === 'password'} onChange={() => setMethod('password')} />
              With a login name and password
            </label>
            <label className="flex items-center gap-2 text-sm text-slate-800">
              <input type="radio" name="method" disabled={!canPasskey} checked={method === 'passkey'} onChange={() => setMethod('passkey')} />
              With a passkey (this device&apos;s screen lock, fingerprint or face, or a security key)
            </label>
            {!canPasskey && <p className="text-xs text-slate-600">This browser cannot use passkeys, so sign up with a password.</p>}
          </fieldset>
          {method === 'password' && (
            <>
              <Field label="Login name" required autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} />
              <Field
                label="Password"
                type="password"
                required
                autoComplete="new-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                hint="The host's password rules apply."
              />
              <Field
                label="Repeat the password"
                type="password"
                required
                autoComplete="new-password"
                value={repeat}
                onChange={(e) => setRepeat(e.target.value)}
              />
            </>
          )}
          <Consent doc={terms} checked={acceptTerms} onChange={setAcceptTerms} />
          <Consent doc={privacy} checked={acceptPrivacy} onChange={setAcceptPrivacy} />
          <p role="status" className="text-sm text-slate-700">
            {status}
          </p>
          <Button type="submit" variant="primary" disabled={busy}>
            Create account
          </Button>
          <p className="text-sm">
            Already have an account? <Link to="/login">Log in</Link>
          </p>
        </form>
      </Card>
    </div>
  );
}

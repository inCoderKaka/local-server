import { useState, type FormEvent } from 'react';
import { asApiError, Button, call, Card, ErrorBanner, Field, useClient, type ApiError } from '@local-server/shared';

type Problem = ApiError | string | null;

/**
 * Changes the password (PUT /v1/me/password). The current password is needed; the host checks the
 * new one against its password rules and counts a wrong current password toward its login lockout.
 * A change logs every other session of this account out, so the page asks once more before it
 * sends. The fields are cleared after every try; nothing is kept.
 */
export function PasswordForm() {
  const client = useClient();
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [repeat, setRepeat] = useState('');
  const [confirming, setConfirming] = useState(false);
  const [problem, setProblem] = useState<Problem>(null);
  const [status, setStatus] = useState('');
  const [busy, setBusy] = useState(false);
  const fieldError = (param: string) => (problem && typeof problem !== 'string' && problem.param === param ? problem.message : undefined);

  function check(e: FormEvent) {
    e.preventDefault();
    setStatus('');
    if (current === '') return setProblem('Enter your current password.');
    if (next === '') return setProblem('Enter a new password.');
    if (next !== repeat) return setProblem('The two new passwords differ.');
    if (next === current) return setProblem('The new password is the same as the current one.');
    setProblem(null);
    setConfirming(true);
  }

  async function send() {
    setBusy(true);
    setProblem(null);
    try {
      await call(client.PUT('/v1/me/password', { body: { current_password: current, new_password: next } }));
      setStatus('Your password is changed. Every other session of this account is logged out.');
    } catch (err) {
      setProblem(asApiError(err));
    } finally {
      setCurrent('');
      setNext('');
      setRepeat('');
      setConfirming(false);
      setBusy(false);
    }
  }

  return (
    <Card title="Change your password">
      <form onSubmit={check} noValidate aria-label="Change your password" className="flex flex-col gap-3">
        <ErrorBanner error={problem} title={typeof problem === 'string' ? undefined : 'The password was not changed'} />
        <Field
          label="Current password"
          type="password"
          autoComplete="current-password"
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          error={fieldError('current_password')}
        />
        <Field
          label="New password"
          type="password"
          autoComplete="new-password"
          value={next}
          onChange={(e) => setNext(e.target.value)}
          hint="The host's password rules apply."
          error={fieldError('new_password')}
        />
        <Field label="Repeat the new password" type="password" autoComplete="new-password" value={repeat} onChange={(e) => setRepeat(e.target.value)} />
        {confirming ? (
          <div role="group" aria-label="Change the password?" className="flex flex-col gap-2 rounded border border-amber-300 bg-amber-50 p-3 text-sm">
            <p className="font-semibold">Change the password?</p>
            <p>Every other browser or device logged in to this account is logged out. This one stays logged in.</p>
            <div className="flex gap-2">
              <Button variant="primary" disabled={busy} onClick={() => void send()}>
                Change the password
              </Button>
              <Button disabled={busy} onClick={() => setConfirming(false)}>
                Cancel
              </Button>
            </div>
          </div>
        ) : (
          <div>
            <Button type="submit" variant="primary">
              Change password
            </Button>
          </div>
        )}
        <p role="status" aria-live="polite" className="text-sm text-emerald-900">
          {status}
        </p>
      </form>
    </Card>
  );
}

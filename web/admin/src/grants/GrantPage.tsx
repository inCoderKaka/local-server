import { useState } from 'react';
import { asApiError, call, Card, ErrorBanner, Link, Spinner, useApi, useClient, useParams } from '@local-server/shared';
import { ConsumerName } from '../consumers/labels';
import { RulesPanel } from '../policy/RulesPanel';
import { GrantForm } from './GrantForm';
import { GrantHistory } from './GrantHistory';
import { GrantView } from './GrantView';
import type { GrantVersion } from './labels';

/**
 * One consumer's grant: its current terms, the form that stores a new version, the version history
 * and the policy rules of this grant (req_001 D-25, D-48, D-87). A consumer starts with no grant,
 * and so with no access, until the host sets one or approves a resource request (D-150).
 */
export default function GrantPage() {
  const client = useClient();
  const { consumerId = '' } = useParams();
  const path = { params: { path: { consumer_id: consumerId } } };
  const consumer = useApi(() => call(client.GET('/admin/v1/consumers/{consumer_id}', path)), [client, consumerId]);
  const grant = useApi(async (): Promise<GrantVersion | null> => {
    try {
      return await call(client.GET('/admin/v1/consumers/{consumer_id}/grant', path));
    } catch (e) {
      // The consumer is read on its own; a 404 here means it has no grant yet.
      const err = asApiError(e);
      if (err.status === 404) return null;
      throw err;
    }
  }, [client, consumerId]);
  const models = useApi(async () => (await call(client.GET('/admin/v1/models'))).data, [client]);
  const [saved, setSaved] = useState<GrantVersion | null>(null);

  const c = consumer.data;
  const current = grant.data;
  const consumerHref = `/consumers/${encodeURIComponent(consumerId)}`;

  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm">
        <Link to={consumerHref} className="text-sky-800 underline">
          Back to the consumer
        </Link>
      </p>
      <h1 className="text-xl font-semibold">
        Grant of {c ? <ConsumerName consumer={c} /> : 'a consumer'}
      </h1>
      <ErrorBanner error={consumer.error} title="The consumer could not be read" />
      {consumer.loading && !c && <Spinner label="Loading the consumer" />}
      {c && (
        <>
          <Card title="Current grant">
            <ErrorBanner error={grant.error} title="The grant could not be read" />
            {grant.loading && current === undefined && <Spinner label="Loading the grant" />}
            {current === null && (
              <p className="text-sm text-slate-700">No grant yet: this consumer cannot call any model until you set a grant below or approve one of its resource requests.</p>
            )}
            {current && <GrantView version={current} />}
          </Card>

          {current !== undefined && (
            <Card title={current ? 'Change the grant' : 'Set a grant'}>
              <div className="flex flex-col gap-3">
                {saved && (
                  <div role="status" className="rounded border border-emerald-300 bg-emerald-50 px-3 py-2 text-sm text-emerald-950">
                    <p>Saved as version {saved.version}.</p>
                    {saved.warnings && saved.warnings.length > 0 && (
                      <>
                        <p className="mt-1 font-medium">The host noted:</p>
                        <ul className="list-disc pl-5">
                          {saved.warnings.map((w) => (
                            <li key={w}>{w}</li>
                          ))}
                        </ul>
                      </>
                    )}
                  </div>
                )}
                <ErrorBanner error={models.error} title="The host's models could not be read" />
                {c.status === 'deleted' ? (
                  <p className="text-sm text-slate-700">This consumer is deleted: its grant cannot be changed.</p>
                ) : (
                  <GrantForm
                    // A saved version starts the form again from what was stored.
                    key={current?.id ?? 'none'}
                    consumerId={consumerId}
                    current={current}
                    hostModels={models.data}
                    onSaved={(v) => {
                      setSaved(v);
                      grant.setData(v);
                    }}
                  />
                )}
              </div>
            </Card>
          )}

          <GrantHistory consumerId={consumerId} reloadKey={current?.id ?? 'none'} />

          {current && (
            <RulesPanel
              grantId={current.grant_id}
              title="Rules of this grant"
              intro={
                <>
                  These rules apply only to this consumer's calls. Rules for the whole host apply too; they are on the{' '}
                  <Link to="/policy-rules" className="text-sky-800 underline">
                    Rules
                  </Link>{' '}
                  page.
                </>
              }
            />
          )}
        </>
      )}
    </div>
  );
}

import { useId, useState, type FormEvent, type ReactNode } from 'react';
import { Button, call, Card, ErrorBanner, formatDateTime, Spinner, Table, UserText, useClient, type Schema } from '@local-server/shared';
import { usePagedList } from '../consumers/pagedList';

type AuditEvent = Schema<'AuditEvent'>;
type Principal = Schema<'PrincipalRef'>;

const kindLabel: Record<Principal['kind'], string> = { host_admin: 'Host admin', consumer: 'Consumer', platform_admin: 'Platform admin' };

function Actor({ actor }: { actor: AuditEvent['actor'] }) {
  if (!actor) return <span className="text-slate-600">local-server</span>;
  return (
    <span className="flex flex-col">
      {actor.display_name ? <UserText text={actor.display_name} /> : <span className="font-mono text-xs">{actor.principal_id}</span>}
      <span className="text-xs text-slate-600">{kindLabel[actor.kind] ?? actor.kind}</span>
    </span>
  );
}

type Plain = string | number | boolean;

function isPlain(v: unknown): v is Plain {
  return typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean';
}

/**
 * The details of an event: each field whose value is a string, number, true or false, or a list
 * of those. Any other value (an object, a mixed list) is left out rather than shown as raw JSON.
 * Strings may carry names people wrote, so they are text, never HTML (req_001 D-31). Audit events
 * never carry prompt or response content (D-40).
 */
function Details({ details }: { details: AuditEvent['details'] }) {
  const items: [string, ReactNode][] = [];
  for (const [k, v] of Object.entries(details ?? {})) {
    if (isPlain(v)) items.push([k, <UserText key={k} text={String(v)} />]);
    else if (Array.isArray(v) && v.every(isPlain)) items.push([k, <UserText key={k} text={v.map(String).join(', ')} />]);
  }
  if (items.length === 0) return null;
  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-2 text-xs">
      {items.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="font-medium text-slate-700">{k}</dt>
          <dd>{v}</dd>
        </div>
      ))}
    </dl>
  );
}

const columns = [
  { key: 't', header: 'Time', render: (e: AuditEvent) => formatDateTime(e.occurred_at) },
  { key: 'a', header: 'Actor', render: (e: AuditEvent) => <Actor actor={e.actor} /> },
  { key: 'x', header: 'Action', render: (e: AuditEvent) => <code className="text-xs">{e.action}</code> },
  {
    key: 'g',
    header: 'Target',
    render: (e: AuditEvent) =>
      e.target_kind || e.target_id ? (
        <span className="flex flex-col">
          {e.target_kind && <span>{e.target_kind}</span>}
          {e.target_id && <span className="font-mono text-xs">{e.target_id}</span>}
        </span>
      ) : null,
  },
  { key: 'd', header: 'Details', render: (e: AuditEvent) => <Details details={e.details} /> },
];

/** A local calendar day (`2026-01-15`) as the time it starts, or undefined when empty or unreadable. */
function dayStart(day: string, plusDays = 0): string | undefined {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day);
  if (!m) return undefined;
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]) + plusDays).toISOString();
}

interface Filter {
  action: string;
  fromDay: string;
  toDay: string;
}

const noFilter: Filter = { action: '', fromDay: '', toDay: '' };

/** The audit log, newest first, a page at a time, filtered by action and days (req_001 D-07). */
export default function AuditPage() {
  const client = useClient();
  const ids = { action: useId(), from: useId(), to: useId() };
  const [draft, setDraft] = useState<Filter>(noFilter);
  const [filter, setFilter] = useState<Filter>(noFilter);
  const list = usePagedList(
    (cursor) =>
      call(
        client.GET('/admin/v1/audit-events', {
          params: {
            query: {
              action: filter.action.trim() || undefined,
              from: dayStart(filter.fromDay),
              // The API's `to` is exclusive; the chosen last day is included.
              to: dayStart(filter.toDay, 1),
              cursor,
            },
          },
        }),
      ),
    [client, filter],
  );

  function apply(e: FormEvent) {
    e.preventDefault();
    setFilter(draft);
  }

  const inputClass = 'rounded border border-slate-300 px-2 py-1';
  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-xl font-semibold">Audit log</h1>
      <Card title="Filter">
        <form onSubmit={apply} className="flex flex-wrap items-end gap-4 text-sm">
          <div className="flex flex-col gap-1">
            <label htmlFor={ids.action} className="font-medium">
              Action
            </label>
            <input
              id={ids.action}
              value={draft.action}
              placeholder="for example access_key.revoked"
              onChange={(e) => setDraft({ ...draft, action: e.target.value })}
              className={inputClass}
            />
          </div>
          <div className="flex flex-col gap-1">
            <label htmlFor={ids.from} className="font-medium">
              From day
            </label>
            <input id={ids.from} type="date" value={draft.fromDay} onChange={(e) => setDraft({ ...draft, fromDay: e.target.value })} className={inputClass} />
          </div>
          <div className="flex flex-col gap-1">
            <label htmlFor={ids.to} className="font-medium">
              To day (included)
            </label>
            <input id={ids.to} type="date" value={draft.toDay} onChange={(e) => setDraft({ ...draft, toDay: e.target.value })} className={inputClass} />
          </div>
          <Button type="submit" variant="primary">
            Show
          </Button>
          <Button
            onClick={() => {
              setDraft(noFilter);
              setFilter(noFilter);
            }}
          >
            Clear
          </Button>
        </form>
      </Card>
      <Card title="Events, newest first" actions={<Button onClick={() => void list.reload()}>Refresh</Button>}>
        <ErrorBanner error={list.error} title="The audit log could not be read" />
        {!list.loaded && list.loading && <Spinner label="Loading the audit log" />}
        {list.loaded && (
          <Table<AuditEvent> caption="Audited actions" rowKey={(e) => e.id} rows={list.items} columns={columns} empty="No events match." />
        )}
        {list.next && (
          <div className="mt-3">
            <Button disabled={list.loading} onClick={() => void list.loadMore()}>
              Load more
            </Button>
          </div>
        )}
      </Card>
    </div>
  );
}

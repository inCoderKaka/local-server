import { Badge, UserText, type Schema } from '@local-server/shared';

export type Consumer = Schema<'Consumer'>;
export type AccessKey = Schema<'AccessKey'>;

export const kindLabel: Record<Consumer['kind'], string> = {
  individual: 'Individual (a person)',
  application: 'Application',
};

const consumerTone = { active: 'good', suspended: 'warn', deleted: 'neutral' } as const;
const consumerStatus: Record<Consumer['status'], string> = { active: 'Active', suspended: 'Suspended', deleted: 'Deleted' };

export function ConsumerStatusBadge({ status }: { status: Consumer['status'] }) {
  return <Badge tone={consumerTone[status]}>{consumerStatus[status]}</Badge>;
}

const keyTone = { active: 'good', expired: 'neutral', revoked: 'bad' } as const;
const keyStatus: Record<AccessKey['status'], string> = { active: 'Active', expired: 'Expired', revoked: 'Revoked' };

export function KeyStatusBadge({ status }: { status: AccessKey['status'] }) {
  return <Badge tone={keyTone[status]}>{keyStatus[status]}</Badge>;
}

/** The visible part of a key, as it starts in the secret (`ls_<prefix>_…`). */
export function keyLabel(k: Pick<AccessKey, 'prefix'>): string {
  return `ls_${k.prefix}_…`;
}

/** A consumer's name as text; a deleted consumer has none. */
export function ConsumerName({ consumer }: { consumer: Pick<Consumer, 'display_name'> }) {
  if (consumer.display_name === null || consumer.display_name === undefined) {
    return <span className="italic text-slate-600">No name</span>;
  }
  return <UserText text={consumer.display_name} />;
}

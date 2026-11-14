import { Badge, type Schema } from '@local-server/shared';
import { useShell } from '../lib/shell';

type SharingState = Schema<'SharingState'>;

export const sharingLabel: Record<SharingState, string> = {
  sharing: 'Sharing',
  paused: 'Paused',
  quiet: 'Quiet hours',
};

const tone = { sharing: 'good', paused: 'warn', quiet: 'info' } as const;

/** The host's sharing state, on every page. */
export function SharingBadge() {
  const { host } = useShell();
  const s = host.data?.sharing_state;
  if (!s) return null;
  return (
    <Badge tone={tone[s]}>
      <span className="sr-only">Sharing state: </span>
      {sharingLabel[s]}
    </Badge>
  );
}

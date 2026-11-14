import { useId, useState, type ReactNode } from 'react';
import { Button } from '@local-server/shared';

/**
 * Shows a secret the host answered once (an access key secret, an invite link): the host keeps
 * only a hash, so it can never be shown again (req_001 D-25, D-40). The value lives only in the
 * state of the component that holds it: never in the address, browser storage or a log. It goes
 * when the host presses "I have copied it" or leaves the page.
 */
export function ShownOnce({
  title,
  label,
  value,
  children,
  onDismiss,
}: {
  /** The title of the panel; it names the region. */
  title: string;
  /** The label of the field that holds the value. */
  label: string;
  value: string;
  /** What the value is for and why it is shown only now. */
  children: ReactNode;
  onDismiss: () => void;
}) {
  const titleId = useId();
  const fieldId = useId();
  const [copy, setCopy] = useState<'idle' | 'copied' | 'failed'>('idle');
  const clipboard = typeof navigator !== 'undefined' ? navigator.clipboard : undefined;

  async function copyValue() {
    try {
      if (!clipboard) throw new Error('no clipboard');
      await clipboard.writeText(value);
      setCopy('copied');
    } catch {
      setCopy('failed');
    }
  }

  return (
    <section aria-labelledby={titleId} className="flex flex-col gap-2 rounded-lg border border-amber-300 bg-amber-50 p-4 text-sm text-amber-950">
      <h3 id={titleId} className="font-semibold">
        {title}
      </h3>
      <div>{children}</div>
      <label htmlFor={fieldId} className="font-medium">
        {label}
      </label>
      <input
        id={fieldId}
        readOnly
        value={value}
        autoComplete="off"
        spellCheck={false}
        onFocus={(e) => e.currentTarget.select()}
        className="w-full rounded border border-amber-400 bg-white px-2 py-1 font-mono text-xs"
      />
      <div className="flex flex-wrap items-center gap-2">
        {clipboard && <Button onClick={() => void copyValue()}>Copy</Button>}
        <Button variant="primary" onClick={onDismiss}>
          I have copied it
        </Button>
        {copy === 'copied' && <span role="status">Copied.</span>}
        {copy === 'failed' && <span role="status">Copying did not work: select the text and copy it.</span>}
        {!clipboard && <span>This browser does not allow copying from here: select the text and copy it.</span>}
      </div>
    </section>
  );
}

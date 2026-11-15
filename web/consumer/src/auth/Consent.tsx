import { CheckboxField } from '@local-server/shared';
import type { LegalText } from '../lib/legal';

/** One legal text to accept: the text to read, then the checkbox that accepts its version. */
export function Consent({ doc, checked, onChange }: { doc: LegalText; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <div className="flex flex-col gap-1">
      <details className="rounded border border-slate-200 bg-slate-50 px-2 py-1 text-sm">
        <summary className="cursor-pointer text-slate-800">Read the {doc.title}</summary>
        <div
          tabIndex={0}
          aria-label={doc.title}
          className="mt-2 max-h-64 overflow-y-auto whitespace-pre-wrap break-words text-xs text-slate-800"
        >
          {doc.text}
        </div>
      </details>
      <CheckboxField
        label={
          <>
            I have read and accept the {doc.title} (version <code>{doc.version}</code>)
          </>
        }
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
      />
    </div>
  );
}

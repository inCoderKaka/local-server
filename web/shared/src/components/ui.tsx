import { useId, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode } from 'react';
import { ApiError } from '../api/client';

type Variant = 'primary' | 'secondary' | 'danger';

const variants: Record<Variant, string> = {
  primary: 'bg-slate-900 text-white hover:bg-slate-700 disabled:bg-slate-400',
  secondary: 'border border-slate-300 bg-white text-slate-900 hover:bg-slate-100 disabled:text-slate-400',
  danger: 'bg-red-700 text-white hover:bg-red-600 disabled:bg-red-300',
};

/** A button; `type` is `button` unless given, so it never submits a form by accident. */
export function Button({
  variant = 'secondary',
  className,
  type = 'button',
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant }) {
  return (
    <button
      type={type}
      className={`rounded px-3 py-1.5 text-sm font-medium focus:outline-2 focus:outline-offset-2 focus:outline-sky-600 ${variants[variant]} ${className ?? ''}`}
      {...rest}
    />
  );
}

/** A titled section of a page. Its title names the region for assistive technology. */
export function Card({ title, children, actions }: { title: ReactNode; children: ReactNode; actions?: ReactNode }) {
  const id = useId();
  return (
    <section aria-labelledby={id} className="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
      <div className="mb-3 flex items-center justify-between gap-2">
        <h2 id={id} className="text-base font-semibold text-slate-900">
          {title}
        </h2>
        {actions}
      </div>
      {children}
    </section>
  );
}

type Tone = 'neutral' | 'good' | 'warn' | 'bad' | 'info';

const tones: Record<Tone, string> = {
  neutral: 'bg-slate-100 text-slate-800',
  good: 'bg-emerald-100 text-emerald-900',
  warn: 'bg-amber-100 text-amber-900',
  bad: 'bg-red-100 text-red-900',
  info: 'bg-sky-100 text-sky-900',
};

/** A small label for a state or a count. */
export function Badge({ tone = 'neutral', children, label }: { tone?: Tone; children: ReactNode; label?: string }) {
  return (
    <span aria-label={label} className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${tones[tone]}`}>
      {children}
    </span>
  );
}

/** A labelled text input with an optional hint and error, wired with aria-describedby. */
export function Field({
  label,
  hint,
  error,
  className,
  ...input
}: InputHTMLAttributes<HTMLInputElement> & { label: ReactNode; hint?: ReactNode; error?: ReactNode }) {
  const id = useId();
  const hintId = hint ? `${id}-hint` : undefined;
  const errId = error ? `${id}-error` : undefined;
  return (
    <div className={`flex flex-col gap-1 ${className ?? ''}`}>
      <label htmlFor={id} className="text-sm font-medium text-slate-800">
        {label}
      </label>
      <input
        id={id}
        aria-invalid={error ? true : undefined}
        aria-describedby={[hintId, errId].filter(Boolean).join(' ') || undefined}
        className="rounded border border-slate-300 px-2 py-1 text-sm aria-invalid:border-red-600"
        {...input}
      />
      {hint && (
        <p id={hintId} className="text-xs text-slate-600">
          {hint}
        </p>
      )}
      {error && (
        <p id={errId} className="text-xs text-red-700">
          {error}
        </p>
      )}
    </div>
  );
}

/** A labelled checkbox. */
export function CheckboxField({ label, ...input }: InputHTMLAttributes<HTMLInputElement> & { label: ReactNode }) {
  const id = useId();
  return (
    <div className="flex items-center gap-2">
      <input id={id} type="checkbox" className="h-4 w-4" {...input} />
      <label htmlFor={id} className="text-sm text-slate-800">
        {label}
      </label>
    </div>
  );
}

export interface Column<R> {
  key: string;
  header: ReactNode;
  render: (row: R) => ReactNode;
}

/** A plain table with a caption; `empty` is shown when there are no rows. */
export function Table<R>({
  caption,
  columns,
  rows,
  rowKey,
  empty,
}: {
  caption: ReactNode;
  columns: Column<R>[];
  rows: R[];
  rowKey: (row: R, index: number) => string;
  empty?: ReactNode;
}) {
  return (
    <table className="w-full border-collapse text-left text-sm">
      <caption className="mb-2 text-left text-sm text-slate-600">{caption}</caption>
      <thead>
        <tr>
          {columns.map((c) => (
            <th key={c.key} scope="col" className="border-b border-slate-200 px-2 py-1 font-medium">
              {c.header}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.length === 0 ? (
          <tr>
            <td colSpan={columns.length} className="px-2 py-2 text-slate-600">
              {empty ?? 'Nothing to show.'}
            </td>
          </tr>
        ) : (
          rows.map((r, i) => (
            <tr key={rowKey(r, i)} className="border-b border-slate-100">
              {columns.map((c) => (
                <td key={c.key} className="px-2 py-1">
                  {c.render(r)}
                </td>
              ))}
            </tr>
          ))
        )}
      </tbody>
    </table>
  );
}

/**
 * Shows an error: its message and, for an API error, the request id the host can find in its
 * log. Nothing is shown for null.
 */
export function ErrorBanner({ error, title }: { error: unknown; title?: ReactNode }) {
  if (!error) return null;
  const message = error instanceof Error ? error.message : String(error);
  const requestId = error instanceof ApiError ? error.requestId : null;
  return (
    <div role="alert" className="rounded border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-900">
      {title && <p className="font-semibold">{title}</p>}
      <p>{message}</p>
      {requestId && <p className="mt-1 text-xs text-red-800">Request id: {requestId}</p>}
    </div>
  );
}

/** A loading indicator with a text label for assistive technology. */
export function Spinner({ label = 'Loading' }: { label?: string }) {
  return (
    <span role="status" className="inline-flex items-center gap-2 text-sm text-slate-600">
      <span aria-hidden="true" className="h-3 w-3 animate-spin rounded-full border-2 border-slate-300 border-t-slate-700" />
      {label}
    </span>
  );
}

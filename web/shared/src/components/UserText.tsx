/**
 * Renders text a consumer or another person wrote (a name, a comment, a request message) as a
 * plain React text node: never as HTML, so markup in it is shown, not run (req_001 D-31). Line
 * breaks are kept and long words wrap.
 */
export function UserText({ text, className }: { text: string | null | undefined; className?: string }) {
  if (text === null || text === undefined) return null;
  return <span className={`whitespace-pre-wrap break-words ${className ?? ''}`.trim()}>{text}</span>;
}

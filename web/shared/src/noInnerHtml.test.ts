import { describe, expect, it } from 'vitest';

// Every source file of every web workspace (web/*/src/**) and the admin page shell, as text.
// Test files are left out: they may quote the forbidden names as data, as this one does.
const raw = import.meta.glob(
  ['../../*/src/**/*.{ts,tsx,js,jsx,mjs,html}', '../../*/index.html', '!**/node_modules/**', '!**/*.test.{ts,tsx}'],
  { query: '?raw', import: 'default', eager: true },
) as Record<string, string>;

// Vite names a match relative to this file (./x for shared, ../../admin/src/x for admin); make the
// names absolute paths so the checks below read the same for every workspace.
const sources: Record<string, string> = Object.fromEntries(
  Object.entries(raw).map(([k, v]) => [new URL(k, import.meta.url).pathname, v]),
);

// Text a consumer or anyone else wrote is rendered as text, never parsed as HTML (req_001 D-31):
// none of these may appear in shipped code.
const forbidden: [string, RegExp][] = [
  ['dangerouslySetInnerHTML', /dangerouslySetInnerHTML/],
  ['innerHTML', /\binnerHTML\b/],
  ['outerHTML', /\bouterHTML\b/],
  ['insertAdjacentHTML', /\binsertAdjacentHTML\b/],
  ['document.write', /\bdocument\s*\.\s*write(ln)?\b/],
];

describe('no HTML injection sinks in web sources', () => {
  it('scans the sources of both shared and admin', () => {
    const names = Object.keys(sources);
    expect(names.some((n) => n.includes('/shared/src/'))).toBe(true);
    expect(names.some((n) => n.includes('/admin/src/'))).toBe(true);
    expect(names.some((n) => n.endsWith('.test.ts') || n.endsWith('.test.tsx'))).toBe(false);
  });

  it('finds no forbidden sink', () => {
    const hits: string[] = [];
    for (const [name, text] of Object.entries(sources)) {
      for (const [what, re] of forbidden) {
        if (re.test(text)) hits.push(`${name}: ${what}`);
      }
    }
    expect(hits).toEqual([]);
  });

  it('would catch each sink', () => {
    const samples = [
      '<div dangerouslySetInnerHTML={{ __html: x }} />',
      'el.innerHTML = x',
      'el.outerHTML = x',
      "el.insertAdjacentHTML('beforeend', x)",
      'document.write(x)',
    ];
    samples.forEach((s, i) => expect(forbidden[i]![1].test(s)).toBe(true));
  });
});

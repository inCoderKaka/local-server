import type { Schema } from '@local-server/shared';
import { listFrom, wholeFrom } from '../grants/values';

export type PolicyRule = Schema<'PolicyRule'>;

/**
 * The policy rule document, format version 1 (internal/policy/rule_v1.schema.json): which calls a
 * rule applies to (`when`: request properties and models) and what it does with them (`deny`, or
 * time windows with their zone, a priority class and caps). The host's check on save is the
 * reference; the form only catches what it can say better at the field.
 */
export type RuleDoc = Record<string, unknown>;

export const FORMAT_VERSION = 1;

export type PropKind = 'true' | 'false' | 'number' | 'text';

export interface PropRow {
  key: number;
  name: string;
  kind: PropKind;
  value: string;
}

export interface WindowRow {
  key: number;
  start: string;
  end: string;
}

export type Priority = '' | 'interactive' | 'background';

export interface RuleValues {
  props: PropRow[];
  /** Model names, separated by commas; empty applies the rule to every model. */
  models: string;
  /** `deny` refuses every matching call; `shape` uses hours, a priority and caps. */
  effect: 'shape' | 'deny';
  useWindows: boolean;
  timeZone: string;
  windows: WindowRow[];
  priority: Priority;
  contextCap: string;
  maxOutputTokens: string;
  maxConcurrency: string;
}

let rowKey = 0;
export const nextKey = () => ++rowKey;

export function emptyRule(): RuleValues {
  return {
    props: [],
    models: '',
    effect: 'shape',
    useWindows: false,
    timeZone: '',
    windows: [],
    priority: '',
    contextCap: '',
    maxOutputTokens: '',
    maxConcurrency: '',
  };
}

const isObj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
const onlyKeys = (o: Record<string, unknown>, keys: string[]) => Object.keys(o).every((k) => keys.includes(k));
const isInt = (v: unknown): v is number => typeof v === 'number' && Number.isSafeInteger(v);

/**
 * The form values of a stored document, or null when this form cannot show all of it (another
 * format version, or a field it does not know); such a rule can still be switched on or off and
 * deleted, and its document is never rewritten by accident.
 */
export function valuesOfDoc(doc: RuleDoc): RuleValues | null {
  if (!isObj(doc) || doc.format_version !== FORMAT_VERSION) return null;
  if (!onlyKeys(doc, ['format_version', 'when', 'deny', 'time_zone', 'windows', 'priority', 'caps'])) return null;
  const v = emptyRule();
  if (doc.when !== undefined) {
    if (!isObj(doc.when) || !onlyKeys(doc.when, ['properties', 'models'])) return null;
    const props = doc.when.properties;
    if (props !== undefined) {
      if (!isObj(props)) return null;
      for (const [name, val] of Object.entries(props)) {
        if (val === true) v.props.push({ key: nextKey(), name, kind: 'true', value: '' });
        else if (val === false) v.props.push({ key: nextKey(), name, kind: 'false', value: '' });
        else if (isInt(val)) v.props.push({ key: nextKey(), name, kind: 'number', value: String(val) });
        else if (typeof val === 'string') v.props.push({ key: nextKey(), name, kind: 'text', value: val });
        else return null;
      }
    }
    const models = doc.when.models;
    if (models !== undefined) {
      if (!Array.isArray(models) || !models.every((m) => typeof m === 'string')) return null;
      v.models = models.join(', ');
    }
  }
  if (doc.deny !== undefined && typeof doc.deny !== 'boolean') return null;
  if (doc.deny === true) v.effect = 'deny';
  if (doc.time_zone !== undefined) {
    if (typeof doc.time_zone !== 'string') return null;
    v.timeZone = doc.time_zone;
  }
  if (doc.windows !== undefined) {
    if (!Array.isArray(doc.windows)) return null;
    for (const w of doc.windows) {
      if (!isObj(w) || !onlyKeys(w, ['start', 'end']) || typeof w.start !== 'string' || typeof w.end !== 'string') return null;
      v.windows.push({ key: nextKey(), start: w.start, end: w.end });
    }
    v.useWindows = true;
  }
  if (doc.priority !== undefined) {
    if (doc.priority !== 'interactive' && doc.priority !== 'background') return null;
    v.priority = doc.priority;
  }
  if (doc.caps !== undefined) {
    if (!isObj(doc.caps) || !onlyKeys(doc.caps, ['context_cap', 'max_output_tokens', 'max_concurrency'])) return null;
    const c = doc.caps;
    for (const [field, k] of [
      ['contextCap', 'context_cap'],
      ['maxOutputTokens', 'max_output_tokens'],
      ['maxConcurrency', 'max_concurrency'],
    ] as const) {
      if (c[k] === undefined) continue;
      if (!isInt(c[k])) return null;
      v[field] = String(c[k]);
    }
  }
  return v;
}

/** The shape of a request property name (RFC 9651 key), as the host checks it. */
const PROPERTY_NAME = /^[a-z*][a-z0-9_.*-]*$/;
const CLOCK = /^([01][0-9]|2[0-3]):[0-5][0-9]$/;
const PRINTABLE = /^[\x20-\x7e]*$/;
const PROPERTY_INT_MAX = 999_999_999_999_999;

/**
 * The document of the form values, or the problems that keep it from being sent, keyed by field
 * (`props.<i>.name`, `windows.<i>.start`, `timeZone`, `effect`, ...).
 */
export function docOfValues(v: RuleValues): { doc?: RuleDoc; problems: Record<string, string> } {
  const p: Record<string, string> = {};
  const doc: RuleDoc = { format_version: FORMAT_VERSION };

  const when: Record<string, unknown> = {};
  const props: Record<string, unknown> = {};
  v.props.forEach((r, i) => {
    const name = r.name.trim();
    if (!PROPERTY_NAME.test(name)) p[`props.${i}.name`] = 'Use lower-case letters, digits, _ . - or *, starting with a letter or *.';
    else if (name in props) p[`props.${i}.name`] = 'This request type is listed twice.';
    switch (r.kind) {
      case 'true':
        props[name] = true;
        break;
      case 'false':
        props[name] = false;
        break;
      case 'number': {
        const t = r.value.trim();
        const n = Number(t);
        if (!/^-?\d+$/.test(t) || Math.abs(n) > PROPERTY_INT_MAX) p[`props.${i}.value`] = 'Write a whole number.';
        props[name] = n;
        break;
      }
      case 'text':
        if (!PRINTABLE.test(r.value)) p[`props.${i}.value`] = 'Use printable ASCII characters only, as in the header.';
        props[name] = r.value;
        break;
    }
  });
  if (v.props.length > 0) when.properties = props;
  const models = listFrom(v.models);
  if (models.length > 0) when.models = models;
  if (Object.keys(when).length > 0) doc.when = when;

  if (v.effect === 'deny') {
    doc.deny = true;
  } else {
    let effects = 0;
    if (v.useWindows) {
      effects++;
      if (!v.timeZone.trim()) p.timeZone = 'Give the time zone the hours are read in.';
      if (v.windows.length === 0) p.windows = 'Add at least one window, or switch the hours off.';
      v.windows.forEach((w, i) => {
        if (!CLOCK.test(w.start)) p[`windows.${i}.start`] = 'Write HH:MM, 24 hours.';
        if (!CLOCK.test(w.end)) p[`windows.${i}.end`] = 'Write HH:MM, 24 hours.';
        else if (w.start === w.end) p[`windows.${i}.end`] = 'The end must differ from the start.';
      });
      doc.time_zone = v.timeZone.trim();
      doc.windows = v.windows.map((w) => ({ start: w.start, end: w.end }));
    }
    if (v.priority) {
      effects++;
      doc.priority = v.priority;
    }
    const caps: Record<string, number> = {};
    for (const [field, k] of [
      ['contextCap', 'context_cap'],
      ['maxOutputTokens', 'max_output_tokens'],
      ['maxConcurrency', 'max_concurrency'],
    ] as const) {
      const n = wholeFrom(v[field], 1);
      if (n.error) p[field] = n.error;
      if (n.value !== undefined) caps[k] = n.value;
    }
    if (Object.keys(caps).length > 0) {
      effects++;
      doc.caps = caps;
    }
    if (effects === 0) p.effect = 'Choose what the rule does: background hours, a priority or a limit, or refuse the calls.';
  }
  return Object.keys(p).length > 0 ? { problems: p } : { doc, problems: p };
}

function propText(name: string, v: unknown): string {
  if (v === true) return name;
  if (typeof v === 'string') return `${name} = "${v}"`;
  return `${name} = ${String(v)}`;
}

/**
 * What a rule does, in words: `Calls with background: only 00:00-07:00 (Local); priority
 * background`. Null when the document is not one this page can read.
 */
export function describeRule(doc: RuleDoc): string | null {
  const v = valuesOfDoc(doc);
  if (!v) return null;
  const when = isObj(doc.when) ? doc.when : {};
  const parts: string[] = [];
  const props = isObj(when.properties) ? Object.entries(when.properties).map(([k, x]) => propText(k, x)) : [];
  const models = Array.isArray(when.models) ? (when.models as string[]) : [];
  let scope = 'Every call';
  if (props.length) scope = `Calls with ${props.join(' and ')}`;
  if (models.length) scope = `${props.length ? `${scope},` : 'Calls'} to ${models.join(' or ')}`;
  if (v.effect === 'deny') return `${scope}: refused`;
  if (v.useWindows) parts.push(`only ${v.windows.map((w) => `${w.start}-${w.end}`).join(', ')} (${v.timeZone})`);
  if (v.priority) parts.push(`priority ${v.priority}`);
  if (v.contextCap) parts.push(`context at most ${v.contextCap} tokens`);
  if (v.maxOutputTokens) parts.push(`at most ${v.maxOutputTokens} output tokens`);
  if (v.maxConcurrency) parts.push(`at most ${v.maxConcurrency} at once`);
  return `${scope}: ${parts.join('; ') || 'no effect'}`;
}

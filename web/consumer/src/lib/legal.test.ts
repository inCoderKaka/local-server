import { describe, expect, it } from 'vitest';
import redeemGo from '../../../../internal/consumerauth/redeem.go?raw';
import { privacy, terms, versionOf } from './legal';

function goConst(name: string): string {
  const m = new RegExp(`\\b${name}\\s*=\\s*"([^"]+)"`).exec(redeemGo);
  if (!m) throw new Error(`no ${name} in redeem.go`);
  return m[1]!;
}

describe('legal texts', () => {
  // The page sends the versions of the texts it shows; the host accepts only the versions in
  // force. Both come from docs/legal, so they must agree.
  it('names the versions the host requires', () => {
    expect(terms.version).toBe(goConst('TermsVersion'));
    expect(privacy.version).toBe(goConst('PrivacyVersion'));
  });

  it('reads the version line and refuses a text without one', () => {
    expect(versionOf('# T\n\n**Version:** terms-2030-01-v2\n\ntext', 't')).toBe('terms-2030-01-v2');
    expect(() => versionOf('# T\n\nno version', 't.md')).toThrow(/t.md has no \*\*Version:\*\* line/);
  });
});

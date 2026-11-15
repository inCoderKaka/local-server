import privacyText from '../../../../docs/legal/privacy-consent.md?raw';
import termsText from '../../../../docs/legal/terms-of-use.md?raw';

/**
 * The Terms of Use and the Privacy and consent notice a person accepts at sign-up (req_001 D-44,
 * D-45). Their text is docs/legal, built into the page as it is, and their version ids are the
 * `**Version:**` lines of those files, the same ids the host requires (internal/consumerauth
 * TermsVersion and PrivacyVersion; a test checks they agree). The page shows the text it sends
 * the version of, so a consent record never names a text the person was not shown.
 */
export interface LegalText {
  /** The document's name in the page. */
  title: string;
  /** The version id the consent names, for example `terms-2026-11-v1`. */
  version: string;
  /** The whole text, shown as plain text. */
  text: string;
}

/** The version id of a legal text: its `**Version:**` line. A text without one is a build mistake. */
export function versionOf(text: string, name: string): string {
  const m = /^\*\*Version:\*\*\s*(\S+)\s*$/m.exec(text);
  if (!m) throw new Error(`${name} has no **Version:** line`);
  return m[1]!;
}

export const terms: LegalText = { title: 'Terms of Use', version: versionOf(termsText, 'terms-of-use.md'), text: termsText };

export const privacy: LegalText = {
  title: 'Privacy and consent notice',
  version: versionOf(privacyText, 'privacy-consent.md'),
  text: privacyText,
};

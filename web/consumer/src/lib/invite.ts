/**
 * Invite links. The host's admin UI hands out `<page address>/invite#<token>` (the
 * invites.link_base_url setting); the token is after `#`, so it never reaches the host in a
 * request line or a log.
 */

/** The invite token in what a person pasted: a whole invite link (the part after `#`) or the code itself. */
export function inviteTokenFrom(input: string): string {
  const s = input.trim();
  const i = s.indexOf('#');
  const t = i >= 0 ? s.slice(i + 1) : s;
  try {
    return decodeURIComponent(t).trim();
  } catch {
    return t.trim();
  }
}

/**
 * Takes the token of an opened invite link out of the address. On `/invite` it replaces the
 * address with the sign-up route (`/#/signup`), so the token stays out of the address bar and the
 * browser history, and returns the token (null when the link had none). On any other path it
 * changes nothing and returns null.
 */
export function takeInviteFromAddress(loc: Pick<Location, 'pathname' | 'hash'>, history: Pick<History, 'replaceState'>): string | null {
  if (loc.pathname.replace(/\/+$/, '') !== '/invite') return null;
  const hash = loc.hash.startsWith('#') ? loc.hash.slice(1) : loc.hash;
  history.replaceState(null, '', '/#/signup');
  if (hash === '' || hash.startsWith('/')) return null;
  return inviteTokenFrom(hash) || null;
}

import { describe, expect, it, vi } from 'vitest';
import { inviteTokenFrom, takeInviteFromAddress } from './invite';

describe('invite links', () => {
  it('reads the token of a pasted link or code', () => {
    expect(inviteTokenFrom('  ls_invite_a_b  ')).toBe('ls_invite_a_b');
    expect(inviteTokenFrom('https://host.example.invalid/invite#ls_invite_a_b')).toBe('ls_invite_a_b');
    expect(inviteTokenFrom('/invite#ls_invite_a%5Fb')).toBe('ls_invite_a_b');
    expect(inviteTokenFrom('')).toBe('');
  });

  it('takes the token out of an opened invite link and goes to the sign-up route', () => {
    const history = { replaceState: vi.fn() };
    expect(takeInviteFromAddress({ pathname: '/invite', hash: '#ls_invite_a_b' }, history)).toBe('ls_invite_a_b');
    expect(history.replaceState).toHaveBeenCalledWith(null, '', '/#/signup');
    expect(takeInviteFromAddress({ pathname: '/invite/', hash: '' }, history)).toBeNull();
    expect(history.replaceState).toHaveBeenCalledTimes(2);
  });

  it('changes nothing on any other address', () => {
    const history = { replaceState: vi.fn() };
    expect(takeInviteFromAddress({ pathname: '/', hash: '#/signup' }, history)).toBeNull();
    expect(takeInviteFromAddress({ pathname: '/other', hash: '#ls_invite_a_b' }, history)).toBeNull();
    expect(history.replaceState).not.toHaveBeenCalled();
  });
});

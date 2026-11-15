import { describe, expect, it } from 'vitest';
import { bytes, text } from '../test/passkeys';
import { assertionJSON, creationOptions, fromBase64url, PasskeyError, passkeysSupported, requestOptions, toBase64url } from './webauthn';

describe('base64url', () => {
  it('round-trips bytes without padding and reads padded input', () => {
    for (const s of ['', 'a', 'ab', 'abc', 'abcd', 'ÿþý']) {
      const b = new Uint8Array([...s].map((c) => c.charCodeAt(0)));
      const enc = toBase64url(b);
      expect(enc).not.toMatch(/[+/=]/);
      expect([...new Uint8Array(fromBase64url(enc))]).toEqual([...b]);
    }
    expect(toBase64url(new Uint8Array([0xfb, 0xff]))).toBe('-_8');
    expect(text(fromBase64url('Y3JlZA=='))).toBe('cred');
    expect(toBase64url(bytes('cred'))).toBe('Y3JlZA');
  });
});

describe('options', () => {
  it('turns the binary fields of creation options into bytes and keeps the rest', () => {
    const o = creationOptions({
      challenge: 'Y2hhbGxlbmdl',
      user: { id: 'dXNlcg', name: 'n', displayName: 'n' },
      excludeCredentials: [{ type: 'public-key', id: 'Y3JlZA', transports: ['usb'] }],
      timeout: 1234,
    });
    expect(text(o.challenge)).toBe('challenge');
    expect(text(o.user.id)).toBe('user');
    expect(text(o.excludeCredentials![0]!.id)).toBe('cred');
    expect(o.excludeCredentials![0]!.transports).toEqual(['usb']);
    expect(o.timeout).toBe(1234);
  });

  it('accepts options wrapped in publicKey', () => {
    const o = requestOptions({ publicKey: { challenge: 'Y2hhbGxlbmdl', allowCredentials: [{ type: 'public-key', id: 'Y3JlZA' }] } });
    expect(text(o.challenge)).toBe('challenge');
    expect(text(o.allowCredentials![0]!.id)).toBe('cred');
  });

  it('refuses options it cannot read with a plain message', () => {
    expect(() => creationOptions({ challenge: 'Y2hhbGxlbmdl' })).toThrow(PasskeyError);
    expect(() => requestOptions({ challenge: 42 })).toThrow(/cannot read/);
    expect(() => requestOptions({ challenge: 'Y2hh', allowCredentials: 'x' })).toThrow(PasskeyError);
  });
});

describe('answers', () => {
  it('leaves out a missing user handle', () => {
    const json = assertionJSON({
      id: 'Y3JlZA',
      rawId: bytes('cred'),
      type: 'public-key',
      authenticatorAttachment: null,
      response: { clientDataJSON: bytes('c'), authenticatorData: bytes('a'), signature: bytes('s'), userHandle: null },
      getClientExtensionResults: () => ({}),
    } as unknown as PublicKeyCredential);
    expect(json.response).toEqual({ clientDataJSON: 'Yw', authenticatorData: 'YQ', signature: 'cw' });
    expect('authenticatorAttachment' in json).toBe(false);
  });

  it('knows a browser without WebAuthn', () => {
    expect(passkeysSupported()).toBe(false);
  });
});

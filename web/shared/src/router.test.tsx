import { act, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Link, matchPath, matchRoute, navigate, parseHash, useHashLocation } from './router';

afterEach(() => {
  window.location.hash = '';
});

describe('hash router', () => {
  it('parses a hash into a path and a query', () => {
    expect(parseHash('')).toMatchObject({ path: '/' });
    expect(parseHash('#')).toMatchObject({ path: '/' });
    expect(parseHash('#/consumers')).toMatchObject({ path: '/consumers' });
    const l = parseHash('#/usage?from=a&to=b');
    expect(l.path).toBe('/usage');
    expect(l.query.get('from')).toBe('a');
  });

  it('matches patterns with params', () => {
    expect(matchPath('/', '/')).toEqual({});
    expect(matchPath('/consumers', '/consumers/')).toEqual({});
    expect(matchPath('/consumers/:id', '/consumers/a%20b')).toEqual({ id: 'a b' });
    expect(matchPath('/consumers/:id', '/consumers')).toBeNull();
    expect(matchPath('/consumers/:id', '/consumers/%E0%A4%A')).toBeNull();
    expect(matchRoute([{ path: '/a' }, { path: '/b/:x' }], '/b/1')).toEqual({ route: { path: '/b/:x' }, params: { x: '1' } });
    expect(matchRoute([{ path: '/a' }], '/c')).toBeNull();
  });

  it('follows hash changes and marks the current link', async () => {
    function Here() {
      const { path } = useHashLocation();
      return (
        <>
          <p>at {path}</p>
          <Link to="/consumers">Consumers</Link>
        </>
      );
    }
    render(<Here />);
    expect(screen.getByText('at /')).toBeTruthy();
    await act(async () => {
      navigate('/consumers');
      window.dispatchEvent(new HashChangeEvent('hashchange'));
    });
    expect(screen.getByText('at /consumers')).toBeTruthy();
    const link = screen.getByRole('link', { name: 'Consumers' });
    expect(link.getAttribute('href')).toBe('#/consumers');
    expect(link.getAttribute('aria-current')).toBe('page');
  });
});

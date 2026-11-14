import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { UserText } from './UserText';

describe('UserText', () => {
  it('renders markup as text, never as HTML', () => {
    const evil = '<img src=x onerror="alert(1)"><b>bold</b>';
    const { container } = render(<UserText text={evil} />);
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('b')).toBeNull();
    expect(container.textContent).toBe(evil);
  });

  it('keeps line breaks and wraps long words', () => {
    const { container } = render(<UserText text={'line one\nline two'} />);
    const span = container.querySelector('span')!;
    expect(span.textContent).toBe('line one\nline two');
    expect(span.className).toContain('whitespace-pre-wrap');
    expect(span.className).toContain('break-words');
  });

  it('renders nothing for a missing text', () => {
    const { container } = render(<UserText text={null} />);
    expect(container.childNodes.length).toBe(0);
  });
});

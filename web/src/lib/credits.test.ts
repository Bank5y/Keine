import { describe, expect, it } from 'vitest';
import { credits, quota, rangeOf, tokens } from '@/lib/credits';

describe('credits', () => {
  it('shows at most two places, and none that are zero', () => {
    expect(credits(6.2)).toBe('6.2');
    expect(credits(20)).toBe('20');
    expect(credits(0.016)).toBe('0.02');
    expect(credits(-3.2)).toBe('-3.2');
    expect(credits(1234.5)).toBe('1,234.5');
  });

  it('shows a daily quota, or 不限 for none', () => {
    expect(quota(20)).toBe('20');
    expect(quota(null)).toBe('不限');
  });
});

describe('tokens', () => {
  it('shortens to k and M', () => {
    expect(tokens(950)).toBe('950');
    expect(tokens(412_000)).toBe('412k');
    expect(tokens(1_310_000)).toBe('1.31M');
    expect(tokens(12_500)).toBe('12.5k');
  });
});

describe('rangeOf', () => {
  const today = new Date(2026, 10, 21, 15, 30);
  it('counts today in each preset', () => {
    expect(rangeOf('today', today)).toEqual({ from: '2026-11-21', to: '2026-11-21' });
    expect(rangeOf('7d', today)).toEqual({ from: '2026-11-15', to: '2026-11-21' });
    expect(rangeOf('30d', today)).toEqual({ from: '2026-10-23', to: '2026-11-21' });
  });
});

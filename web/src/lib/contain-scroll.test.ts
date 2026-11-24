import { describe, expect, it } from 'vitest';
import { takesScroll, type Scroller } from '@/lib/contain-scroll';

// a box that scrolls down: `top` of `range` scrolled
const box = (top: number, range: number): Scroller => ({ start: top, size: 100, content: 100 + range, scrolls: true });
const still: Scroller = { start: 0, size: 100, content: 100, scrolls: false };

describe('takesScroll', () => {
  it('lets a box scroll while it has room that way', () => {
    expect(takesScroll([box(0, 200)], 40)).toBe(true);
    expect(takesScroll([box(120, 200)], -40)).toBe(true);
  });

  it('stops the scroll once the box is at its end that way', () => {
    expect(takesScroll([box(200, 200)], 40)).toBe(false);
    expect(takesScroll([box(0, 200)], -40)).toBe(false);
  });

  it('stops the scroll over content that does not need to scroll', () => {
    expect(takesScroll([still], 40)).toBe(false);
    expect(takesScroll([], 40)).toBe(false);
  });

  it('lets an outer box take what an inner one at its end cannot', () => {
    expect(takesScroll([box(200, 200), box(10, 50)], 40)).toBe(true);
    expect(takesScroll([box(200, 200), box(50, 50)], 40)).toBe(false);
  });

  it('ignores a box that overflows but does not scroll (overflow hidden)', () => {
    expect(takesScroll([{ start: 0, size: 100, content: 300, scrolls: false }], 40)).toBe(false);
  });

  it('counts a box a fraction of a pixel short of its end as at the end', () => {
    expect(takesScroll([box(199.5, 200)], 40)).toBe(false);
  });

  it('lets no movement through untouched', () => {
    expect(takesScroll([still], 0)).toBe(true);
  });
});

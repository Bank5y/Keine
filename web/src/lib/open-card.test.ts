import { describe, expect, it } from 'vitest';
import { placeOpenCard, scrolledAway, SCROLL_AWAY } from '@/lib/open-card';

// a 1000px-high screen with a 64px bar on top and 16px kept free at the bottom
const screen = { viewport: 1000, top: 64, margin: 16 };

describe('placeOpenCard', () => {
  it('keeps a card that fits level with its text, as tall as its content', () => {
    expect(placeOpenCard({ ...screen, anchor: 200, content: 300 })).toEqual({ top: 200, height: 300 });
  });

  it('caps the card at 60% of the screen, the rest scrolling inside it', () => {
    expect(placeOpenCard({ ...screen, anchor: 100, content: 2000 })).toEqual({ top: 100, height: 600 });
  });

  it('moves a card near the bottom of the screen up until all of it shows', () => {
    // 300 high from 800 would end at 1100; it ends at 984 instead
    expect(placeOpenCard({ ...screen, anchor: 800, content: 300 })).toEqual({ top: 684, height: 300 });
  });

  it('moves a card whose text is below the screen up into it', () => {
    expect(placeOpenCard({ ...screen, anchor: 1200, content: 200 })).toEqual({ top: 784, height: 200 });
  });

  it('keeps a card whose text scrolled under the top bar below the bar', () => {
    expect(placeOpenCard({ ...screen, anchor: -50, content: 200 })).toEqual({ top: 64, height: 200 });
  });

  it('shortens the card on a screen too low for 60% between the bar and the bottom', () => {
    // 400 high: 60% is 240, but only 400 - 64 - 16 = 320 is free, so 240
    expect(placeOpenCard({ viewport: 400, top: 64, margin: 16, anchor: 300, content: 500 })).toEqual({ top: 144, height: 240 });
    // 300 high: 60% is 180, the free space 184
    expect(placeOpenCard({ viewport: 300, top: 100, margin: 16, anchor: 280, content: 500 })).toEqual({ top: 104, height: 180 });
    // 200 high: 60% is 120, the free space only 84
    expect(placeOpenCard({ viewport: 200, top: 100, margin: 16, anchor: 150, content: 500 })).toEqual({ top: 100, height: 84 });
  });
});

describe('scrolledAway', () => {
  it('keeps the card open while the page moves a little either way', () => {
    expect(scrolledAway(1000, 1000 + SCROLL_AWAY)).toBe(false);
    expect(scrolledAway(1000, 1000 - SCROLL_AWAY)).toBe(false);
  });

  it('folds the card once the page has moved past the threshold, down or up', () => {
    expect(scrolledAway(1000, 1000 + SCROLL_AWAY + 1)).toBe(true);
    expect(scrolledAway(1000, 1000 - SCROLL_AWAY - 1)).toBe(true);
  });
});

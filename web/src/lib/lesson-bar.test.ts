import { describe, expect, it } from 'vitest';
import { initialBar, nextBar, type BarEvent, type TapOn } from '@/lib/lesson-bar';

const shownAfter = (...events: BarEvent[]) => events.reduce(nextBar, initialBar).shown;
// on a page that scrolls 2000px at most
const scroll = (y: number, max = 2000): BarEvent => ({ type: 'scroll', y, max });
// a tap on the page itself, at a height on a 390×844 phone screen
const tap = (y: number, on: TapOn = 'page', selecting = false): BarEvent => ({ type: 'tap', y, height: 844, on, selecting });

describe('the lesson top bar', () => {
  it('hides while scrolling down', () => {
    expect(shownAfter(scroll(300), scroll(600))).toBe(false);
  });

  it('stays hidden on a small scroll up, and comes back on a deliberate one', () => {
    expect(shownAfter(scroll(300), scroll(600), scroll(590))).toBe(false);
    expect(shownAfter(scroll(300), scroll(600), scroll(536))).toBe(true);
  });

  it('stays hidden as the page bounces back from past its end', () => {
    expect(shownAfter(scroll(1900), scroll(2000), scroll(2090), scroll(2000))).toBe(false);
  });

  it('counts a scroll up afresh after scrolling down again', () => {
    expect(shownAfter(scroll(300), scroll(600), scroll(560), scroll(580), scroll(530))).toBe(false);
  });

  it('always shows at the top of the page', () => {
    expect(shownAfter()).toBe(true);
    expect(shownAfter(scroll(600), scroll(0))).toBe(true);
    // a bounce or a nudge right at the top does not hide it
    expect(shownAfter(scroll(0), scroll(10))).toBe(true);
  });

  it('toggles on a tap in the middle third of the screen', () => {
    const hidden = [scroll(300), scroll(600)];
    expect(shownAfter(...hidden, tap(422))).toBe(true);
    expect(shownAfter(...hidden, tap(300), tap(540))).toBe(false);
  });

  it('stays as it is on a tap near the top or bottom edge', () => {
    const hidden = [scroll(300), scroll(600)];
    expect(shownAfter(...hidden, tap(100))).toBe(false);
    expect(shownAfter(...hidden, tap(800))).toBe(false);
    expect(shownAfter(scroll(600), scroll(500), tap(800))).toBe(true);
  });

  it('ignores a tap that means something else: a link, a button, a video, an image, an annotation', () => {
    for (const on of ['link', 'button', 'video', 'image', 'annotation'] as const) {
      expect(shownAfter(scroll(300), scroll(600), tap(422, on))).toBe(false);
      expect(shownAfter(scroll(600), scroll(500), tap(422, on))).toBe(true);
    }
  });

  it('ignores a tap while text is selected, as for an annotation', () => {
    expect(shownAfter(scroll(300), scroll(600), tap(422, 'page', true))).toBe(false);
  });

  it('ignores a tap that began on a selection, though the selection is gone by the tap', () => {
    const press = (selecting: boolean): BarEvent => ({ type: 'press', selecting });
    expect(shownAfter(scroll(300), scroll(600), press(true), tap(422))).toBe(false);
    // the next plain tap is the reader's again
    expect(shownAfter(scroll(300), scroll(600), press(true), tap(422), press(false), tap(422))).toBe(true);
  });

  it('stays at the top of the page whatever the tap', () => {
    expect(shownAfter(tap(422))).toBe(true);
  });
});

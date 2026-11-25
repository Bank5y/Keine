import { describe, expect, it } from 'vitest';
import { closedSheet, nextSheet, type SheetEvent, type SheetState } from '@/lib/exploration-sheet';

const after = (...events: SheetEvent[]): SheetState => events.reduce((s, e) => nextSheet(s, e).state, closedSheet);
// a tap on the passage of exploration id, with the page scrolled to y
const pinned = (id: number, y = 1000): SheetEvent => ({ type: 'tapPassage', id, y });
const scroll = (y: number): SheetEvent => ({ type: 'scroll', y });
const shown = (s: SheetState) => (s.detent === 'closed' ? null : s.body);

describe('the exploration sheet on a narrow screen', () => {
  it('starts closed', () => {
    expect(shown(after())).toBeNull();
  });

  it('raises the card of a pinned passage tapped in the text', () => {
    expect(after(pinned(7))).toMatchObject({ detent: 'peek', body: { kind: 'exploration', id: 7 } });
  });

  it('swaps the card straight to another pinned passage tapped while it is up', () => {
    expect(after(pinned(7), pinned(9))).toMatchObject({ detent: 'peek', body: { kind: 'exploration', id: 9 } });
  });

  it('closes on a tap on the text elsewhere', () => {
    expect(shown(after(pinned(7), { type: 'tapText' }))).toBeNull();
  });

  it('stays up while the text is read on a little, and goes once it is read on past 120px', () => {
    expect(shown(after(pinned(7, 1000), scroll(1060), scroll(1120)))).not.toBeNull();
    expect(shown(after(pinned(7, 1000), scroll(1060), scroll(1121)))).toBeNull();
  });

  it('measures reading on from the highest the page went after the card came up', () => {
    // back up 200px moves the start up with it
    expect(shown(after(pinned(7, 1000), scroll(800), scroll(920)))).not.toBeNull();
    expect(shown(after(pinned(7, 1000), scroll(800), scroll(921)))).toBeNull();
    // reading down, glancing back and reading on counts where the page is,
    // not how far it travelled
    expect(shown(after(pinned(7, 1000), scroll(1100), scroll(1050), scroll(1110)))).not.toBeNull();
    expect(shown(after(pinned(7, 1000), scroll(1100), scroll(1050), scroll(1121)))).toBeNull();
  });

  it('measures afresh from where the page is when another passage swaps the card', () => {
    expect(shown(after(pinned(7, 1000), scroll(1100), pinned(9, 1100), scroll(1200)))).not.toBeNull();
  });

  it('closes when the card is pulled down far enough, and settles back on a short pull', () => {
    const pull = (dy: number): SheetEvent => ({ type: 'release', dy });
    expect(shown(after(pinned(7), pull(61)))).toBeNull();
    expect(after(pinned(7), pull(40))).toMatchObject({ detent: 'peek', body: { id: 7 } });
  });

  it('keeps the card and the history as they are on the way', () => {
    const events: SheetEvent[] = [pinned(7), pinned(9), scroll(1050), { type: 'release', dy: 20 }, { type: 'tapText' }];
    let s = closedSheet;
    for (const e of events) { const step = nextSheet(s, e); expect(step.history).toBeNull(); s = step.state; }
  });

  it('stays closed whatever the scrolling', () => {
    expect(shown(after(scroll(300), scroll(0)))).toBeNull();
  });
});

describe('the whole-screen exploration on a narrow screen', () => {
  const expand: SheetEvent = { type: 'expand' };
  const back: SheetEvent = { type: 'back' };
  const close: SheetEvent = { type: 'close' };

  it('opens straight from the card on 展开探索, and adds a history entry for it', () => {
    const step = nextSheet(after(pinned(7)), expand);
    expect(step.state).toMatchObject({ detent: 'full', body: { kind: 'exploration', id: 7 } });
    expect(step.history).toBe('push');
  });

  it('opens straight from the card pulled up far enough; a short pull settles back', () => {
    const up = nextSheet(after(pinned(7)), { type: 'release', dy: -61 });
    expect(up.state).toMatchObject({ detent: 'full', body: { id: 7 } });
    expect(up.history).toBe('push');
    expect(after(pinned(7), { type: 'release', dy: -40 })).toMatchObject({ detent: 'peek' });
  });

  it('closes on the system back, card and all, back to the text', () => {
    const step = nextSheet(after(pinned(7), expand), back);
    expect(shown(step.state)).toBeNull();
    expect(step.history).toBeNull();
  });

  it('closes on ✕ the way the system back does: by going back through the history', () => {
    const open = after(pinned(7), expand);
    const step = nextSheet(open, close);
    expect(step.history).toBe('back');
    // the back that follows closes it
    expect(shown(nextSheet(step.state, back).state)).toBeNull();
  });

  it('stays open whatever the page under it does', () => {
    const open = after(pinned(7), expand);
    expect(after(pinned(7), expand, scroll(5000), { type: 'tapText' })).toEqual(open);
  });

  it('leaves the history alone for a back it did not ask for', () => {
    expect(nextSheet(after(pinned(7)), back)).toEqual({ state: after(pinned(7)), history: null });
    expect(nextSheet(closedSheet, close).history).toBeNull();
  });
});

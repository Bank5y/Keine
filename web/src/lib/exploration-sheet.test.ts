import { describe, expect, it } from 'vitest';
import { closedSheet, nextSheet, swipeAxis, swipeStarts, type SheetEvent, type SheetState } from '@/lib/exploration-sheet';

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

describe('the node tree drawer on the whole-screen exploration', () => {
  const expand: SheetEvent = { type: 'expand' };
  const back: SheetEvent = { type: 'back' };
  const swipe = (dx: number): SheetEvent => ({ type: 'swipe', dx });
  const full = () => after(pinned(7), expand);

  it('comes out on a swipe right past 60px, and settles back on a shorter one', () => {
    expect(after(pinned(7), expand, swipe(61)).tree).toBe(true);
    expect(after(pinned(7), expand, swipe(40)).tree).toBe(false);
  });

  it('goes on a swipe left past 60px, a tap beside it, or a node picked in it', () => {
    expect(after(pinned(7), expand, swipe(80), swipe(-61)).tree).toBe(false);
    expect(after(pinned(7), expand, swipe(80), swipe(-40)).tree).toBe(true);
    expect(after(pinned(7), expand, swipe(80), { type: 'tapBeside' }).tree).toBe(false);
    expect(after(pinned(7), expand, swipe(80), { type: 'pickNode' }).tree).toBe(false);
  });

  it('keeps no history entry of its own: opening it and switching nodes add none', () => {
    expect(nextSheet(full(), swipe(80)).history).toBeNull();
    expect(nextSheet(after(pinned(7), expand, swipe(80)), { type: 'pickNode' }).history).toBeNull();
  });

  it('goes first on the system back, putting back the entry the back took; the next back closes the page', () => {
    const open = after(pinned(7), expand, swipe(80));
    const first = nextSheet(open, back);
    expect(first.state).toMatchObject({ detent: 'full', tree: false });
    expect(first.history).toBe('push');
    expect(shown(nextSheet(first.state, back).state)).toBeNull();
  });

  it('is put away with the page, and is not out when the page opens again', () => {
    const closed = after(pinned(7), expand, swipe(80), back, back);
    expect(after(pinned(7), expand, swipe(80), back, back, pinned(7), expand).tree).toBe(false);
    expect(shown(closed)).toBeNull();
  });
});

describe('where a swipe for the node tree may start', () => {
  it('is anywhere plain on the page, past the 20px at the screen’s left edge kept for the system back', () => {
    expect(swipeStarts({ x: 21, on: 'page', tree: false })).toBe(true);
    expect(swipeStarts({ x: 20, on: 'page', tree: false })).toBe(false);
    expect(swipeStarts({ x: 4, on: 'page', tree: true })).toBe(true);
  });

  it('is not in what scrolls sideways (code, tables), in a reply’s text, or on a row of the tree', () => {
    for (const on of ['sideways', 'reply', 'row'] as const) expect(swipeStarts({ x: 200, on, tree: false })).toBe(false);
    expect(swipeStarts({ x: 200, on: 'row', tree: true })).toBe(false);
  });

  it('is told from a scroll by its first 10px: sideways moves swipe, up and down ones scroll', () => {
    expect(swipeAxis(6, 6)).toBeNull();
    expect(swipeAxis(12, 3)).toBe('x');
    expect(swipeAxis(-3, 12)).toBe('y');
  });
});

describe('the report page and deleting on the whole-screen exploration', () => {
  const expand: SheetEvent = { type: 'expand' };
  const back: SheetEvent = { type: 'back' };
  const report: SheetEvent = { type: 'openReport' };

  it('pushes the report page in with a history entry of its own', () => {
    const step = nextSheet(after(pinned(7), expand), report);
    expect(step.state).toMatchObject({ detent: 'full', report: true });
    expect(step.history).toBe('push');
  });

  it('goes back the way it came: the report page, the drawer, the page, then the text', () => {
    // the drawer's own back pushes the page's entry back, so each back here
    // is one the browser really took
    let s = after(pinned(7), expand, { type: 'swipe', dx: 80 }, report);
    const steps: string[] = [];
    for (let i = 0; i < 3; i++) {
      s = nextSheet(s, back).state;
      steps.push(s.detent === 'closed' ? 'text' : s.tree ? 'drawer' : s.report ? 'report' : 'page');
    }
    expect(steps).toEqual(['drawer', 'page', 'text']);
  });

  it('closes the report page on its own back button as the system back does', () => {
    const step = nextSheet(after(pinned(7), expand, report), { type: 'close' });
    expect(step.history).toBe('back');
    expect(nextSheet(step.state, back).state).toMatchObject({ detent: 'full', report: false });
  });

  it('closes, card and all, once the exploration is deleted, taking the page’s entry with it', () => {
    const step = nextSheet(after(pinned(7), expand), { type: 'deleted' });
    expect(shown(step.state)).toBeNull();
    expect(step.history).toBe('back');
  });
});

describe('analysis and the lesson’s list on a narrow screen', () => {
  const analyze: SheetEvent = { type: 'analyze' };
  const list: SheetEvent = { type: 'openList' };

  it('takes half the screen for an analysis, and for the list of the lesson’s explorations', () => {
    expect(after(analyze)).toMatchObject({ detent: 'half', body: { kind: 'analysis' } });
    expect(after(pinned(7), list)).toMatchObject({ detent: 'half', body: { kind: 'list' } });
  });

  it('folds into the new exploration’s card once the analysis is pinned', () => {
    const step = nextSheet(after(analyze), { type: 'pinned', id: 12, y: 400 });
    expect(step.state).toMatchObject({ detent: 'peek', body: { kind: 'exploration', id: 12 } });
    expect(step.history).toBeNull();
    // reading on is measured from where the page was
    expect(shown(after(analyze, { type: 'pinned', id: 12, y: 400 }, scroll(520)))).not.toBeNull();
    expect(shown(after(analyze, { type: 'pinned', id: 12, y: 400 }, scroll(521)))).toBeNull();
  });

  it('opens the card of an exploration picked in the list', () => {
    expect(after(list, { type: 'pickExploration', id: 9, y: 300 })).toMatchObject({ detent: 'peek', body: { kind: 'exploration', id: 9 } });
  });

  it('closes the half screen on a tap beside it, or pulled down far enough; the page under it does not move it', () => {
    expect(shown(after(list, { type: 'tapBeside' }))).toBeNull();
    expect(shown(after(list, { type: 'release', dy: 61 }))).toBeNull();
    expect(after(list, { type: 'release', dy: 30 })).toMatchObject({ detent: 'half' });
    expect(after(list, scroll(5000))).toMatchObject({ detent: 'half' });
  });

  it('adds no history entry for the half screen', () => {
    expect(nextSheet(closedSheet, analyze).history).toBeNull();
    expect(nextSheet(closedSheet, list).history).toBeNull();
  });
});

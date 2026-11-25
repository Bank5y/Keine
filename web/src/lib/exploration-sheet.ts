// On a narrow screen the lesson's explorations come up from the foot of the
// screen in a sheet, over the text. Where the sheet stands and what it
// holds follow from the reader's taps, the page's scrolling and drags on
// the sheet, one event at a time.

// reading on past this much of the text, from the highest the page went
// since the card came up, puts the card away
const CLOSE_AFTER = 120;

// a drag on the sheet let go past this many px moves it on a detent;
// a shorter one lets it settle back
const DRAG_COMMIT = 60;

// how far the sheet's top edge stands from the top of the screen, as a
// share of the screen's height: closed, the card at the foot (peek), half
// the screen, the whole screen. An exploration only takes the card and the
// whole screen; the lesson's list of explorations and an analysis take half.
export type Detent = 'closed' | 'peek' | 'half' | 'full';
export const DETENT_TOP: Record<Detent, number> = { closed: 1, peek: 0.76, half: 0.36, full: 0 };

// what the sheet holds
export type SheetBody = { kind: 'exploration'; id: number };

// top: the highest the page has been scrolled to (the least scrollY) since
// the card came up, where reading on is measured from. tree: the node tree
// drawer is out over the whole-screen exploration.
export interface SheetState { detent: Detent; body: SheetBody | null; top: number; tree: boolean }

// tapPassage: a pinned passage tapped in the text, the page scrolled to y.
// tapText: a tap on the text away from any pinned passage. scroll: the
// page scrolled to y. release: a drag on the sheet let go dy px down from
// where it began (up is negative). expand: 展开探索 on the card. close: ✕
// on the whole-screen exploration. back: the system's back (the browser
// went back an entry). swipe: a sideways swipe on the whole-screen
// exploration let go dx px right of where it began (left is negative).
// tapBeside: a tap beside the node tree drawer. pickNode: a node picked in
// the drawer.
export type SheetEvent =
  | { type: 'tapPassage'; id: number; y: number }
  | { type: 'tapText' }
  | { type: 'scroll'; y: number }
  | { type: 'release'; dy: number }
  | { type: 'expand' }
  | { type: 'close' }
  | { type: 'back' }
  | { type: 'swipe'; dx: number }
  | { type: 'tapBeside' }
  | { type: 'pickNode' };
// the next state, and what the step does to the browser's history: push
// an entry (so the system's back undoes the step), go back one (the back
// that follows takes the step), or nothing
export interface SheetStep { state: SheetState; history: 'push' | 'back' | null }

export const closedSheet: SheetState = { detent: 'closed', body: null, top: 0, tree: false };

export function nextSheet(s: SheetState, e: SheetEvent): SheetStep {
  const stay = (state: SheetState): SheetStep => ({ state, history: null });
  // the whole screen opens at once from the card, over the page and its
  // bars, with an entry of its own in the history
  const full = (): SheetStep => ({ state: { ...s, detent: 'full' }, history: 'push' });
  if (s.detent === 'full') {
    const tree = (on: boolean) => stay({ ...s, tree: on });
    switch (e.type) {
      // ✕ goes the system back's way, so the entry the page pushed goes too
      case 'close': return { state: s, history: 'back' };
      // the drawer has no entry of its own: a back puts it away and puts
      // back the page's entry that the back took
      case 'back': return s.tree ? { state: { ...s, tree: false }, history: 'push' } : stay(closedSheet);
      case 'swipe':
        if (!s.tree && e.dx > DRAG_COMMIT) return tree(true);
        if (s.tree && e.dx < -DRAG_COMMIT) return tree(false);
        return stay(s);
      case 'tapBeside':
      case 'pickNode': return tree(false);
      // nothing under the page can be reached while it is up
      default: return stay(s);
    }
  }
  switch (e.type) {
    case 'tapPassage': return stay({ ...closedSheet, detent: 'peek', body: { kind: 'exploration', id: e.id }, top: e.y });
    case 'tapText': return stay(closedSheet);
    case 'scroll':
      if (s.detent !== 'peek') return stay(s);
      if (e.y - s.top > CLOSE_AFTER) return stay(closedSheet);
      return stay(e.y < s.top ? { ...s, top: e.y } : s);
    case 'release':
      if (s.detent !== 'peek') return stay(s);
      if (e.dy > DRAG_COMMIT) return stay(closedSheet);
      if (e.dy < -DRAG_COMMIT) return full();
      return stay(s);
    case 'expand': return s.detent === 'peek' ? full() : stay(s);
    case 'close':
    case 'back':
    case 'swipe':
    case 'tapBeside':
    case 'pickNode': return stay(s);
  }
}

// ---- the swipe that pulls out the node tree ----

// the strip at the screen's left edge where the system keeps its own back
const EDGE = 20;
// how far a press moves before it is told a swipe or a scroll
const DECIDE = 10;

// what a press starts on: something that scrolls sideways (a code block,
// a table, the path in the top bar), a reply's text (where a long press
// selects), a row of the node tree (whose own swipe is to delete it), or
// anywhere else on the page
export type SwipeOn = 'page' | 'sideways' | 'reply' | 'row';

// swipeStarts says whether a press at x px from the screen's left, on
// what it is on, may swipe the node tree out or away.
export function swipeStarts({ x, on, tree }: { x: number; on: SwipeOn; tree: boolean }): boolean {
  if (on !== 'page') return false;
  return tree || x > EDGE;
}

// swipeAxis tells a press that moved dx, dy px a sideways swipe (x) or a
// scroll (y), once it has moved far enough to tell; null until then.
export function swipeAxis(dx: number, dy: number): 'x' | 'y' | null {
  if (Math.hypot(dx, dy) <= DECIDE) return null;
  return Math.abs(dx) > Math.abs(dy) ? 'x' : 'y';
}

// On a narrow screen the lesson's explorations come up from the foot of the
// screen in a sheet, over the text. Where the sheet stands and what it
// holds follow from the reader's taps, the page's scrolling and drags on
// the sheet, one event at a time.

// how far the sheet's top edge stands from the top of the screen, as a
// share of the screen's height: closed, the card at the foot (peek), half
// the screen, the whole screen. An exploration only takes the card and the
// whole screen; the lesson's list of explorations and an analysis take half.
// reading on past this much of the text, from the highest the page went
// since the card came up, puts the card away
export const CLOSE_AFTER = 120;

// a drag on the sheet let go past this many px moves it on a detent;
// a shorter one lets it settle back
export const DRAG_COMMIT = 60;

export type Detent = 'closed' | 'peek' | 'half' | 'full';
export const DETENT_TOP: Record<Detent, number> = { closed: 1, peek: 0.76, half: 0.36, full: 0 };

// what the sheet holds
export type SheetBody = { kind: 'exploration'; id: number };

// top: the highest the page has been scrolled to (the least scrollY) since
// the card came up, where reading on is measured from
export interface SheetState { detent: Detent; body: SheetBody | null; top: number }

export type SheetEvent =
  | { type: 'pinned'; id: number; y: number }
  | { type: 'page' }
  | { type: 'scroll'; y: number }
  | { type: 'release'; dy: number };

// pinned: a pinned passage tapped in the text, the page scrolled to y.
// page: a tap on the text away from any pinned passage. scroll: the page
// scrolled to y. release: a drag on the sheet let go dy px down from where
// it began (up is negative).

// the next state, and whether the step adds an entry to the browser's
// history (so the system's back undoes it)
export interface SheetStep { state: SheetState; push: boolean }

export const closedSheet: SheetState = { detent: 'closed', body: null, top: 0 };

export function nextSheet(s: SheetState, e: SheetEvent): SheetStep {
  const stay = (state: SheetState): SheetStep => ({ state, push: false });
  switch (e.type) {
    case 'pinned': return stay({ detent: 'peek', body: { kind: 'exploration', id: e.id }, top: e.y });
    case 'page': return stay(closedSheet);
    case 'scroll':
      if (s.detent !== 'peek') return stay(s);
      if (e.y - s.top > CLOSE_AFTER) return stay(closedSheet);
      return stay(e.y < s.top ? { ...s, top: e.y } : s);
    case 'release':
      return stay(s.detent === 'peek' && e.dy > DRAG_COMMIT ? closedSheet : s);
  }
}

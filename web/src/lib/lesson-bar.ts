// The lesson's top bar on a narrow screen gives the screen over to the text
// while reading, the way reading apps do. Whether it shows follows from the
// page's scrolling and the reader's taps, one event at a time.

export interface BarState { shown: boolean; y: number }

// what a tap landed on
export type TapOn = 'page' | 'link' | 'button' | 'video' | 'image' | 'annotation';

// scroll: the page's new scroll position. tap: where on the screen (y of
// height, both in px), on what, and whether text is selected at the time.
export type BarEvent =
  | { type: 'scroll'; y: number }
  | { type: 'tap'; y: number; height: number; on: TapOn; selecting: boolean };

export const initialBar: BarState = { shown: true, y: 0 };

// within this much of the top the bar stays: hiding it there would only
// uncover the page's top padding
const TOP = 56;

export function nextBar(s: BarState, e: BarEvent): BarState {
  if (e.type === 'scroll') {
    if (e.y <= TOP) return { shown: true, y: e.y };
    return { shown: e.y === s.y ? s.shown : e.y < s.y, y: e.y };
  }
  // a tap on something that answers it, or one that ends or clears a
  // selection being annotated, is not meant for the bar
  if (s.y <= TOP || e.on !== 'page' || e.selecting) return s;
  const middle = e.y >= e.height / 3 && e.y < (e.height * 2) / 3;
  return middle ? { ...s, shown: !s.shown } : s;
}

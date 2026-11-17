// The lesson's top bar on a narrow screen gives the screen over to the text
// while reading, the way reading apps do. Whether it shows follows from the
// page's scrolling and the reader's taps, one event at a time.

// pressedOnSelection: text was selected when the current gesture began
export interface BarState { shown: boolean; y: number; pressedOnSelection: boolean }

// what a tap landed on
export type TapOn = 'page' | 'link' | 'button' | 'video' | 'image' | 'annotation';

// scroll: the page's new scroll position. press: a gesture begins
// (pointerdown), with or without text selected. tap: where on the screen (y
// of height, both in px), on what, and whether text is selected at the time.
// On touch, the tap that clears a selection may collapse it before the tap
// arrives, so the press says what was selected when the finger went down.
export type BarEvent =
  | { type: 'scroll'; y: number }
  | { type: 'press'; selecting: boolean }
  | { type: 'tap'; y: number; height: number; on: TapOn; selecting: boolean };

export const initialBar: BarState = { shown: true, y: 0, pressedOnSelection: false };

// within this much of the top the bar stays: hiding it there would only
// uncover the page's top padding
const TOP = 56;

export function nextBar(s: BarState, e: BarEvent): BarState {
  if (e.type === 'scroll') {
    if (e.y <= TOP) return { ...s, shown: true, y: e.y };
    return { ...s, shown: e.y === s.y ? s.shown : e.y < s.y, y: e.y };
  }
  if (e.type === 'press') return { ...s, pressedOnSelection: e.selecting };
  // a tap on something that answers it, or one that ends or clears a
  // selection being annotated, is not meant for the bar
  if (s.y <= TOP || e.on !== 'page' || e.selecting || s.pressedOnSelection) return s;
  const middle = e.y >= e.height / 3 && e.y < (e.height * 2) / 3;
  return middle ? { ...s, shown: !s.shown } : s;
}

// Where an exploration's card opens in the rail (#180), in screen
// coordinates: level with its text, as tall as its content but no taller
// than 60% of the screen, the rest scrolling inside it; moved up as far as
// it takes to show all of it, never under the top bar.
export interface OpenCardSpace {
  // the top of the card's text on the screen
  anchor: number;
  // the height the card's content would take, unbounded
  content: number;
  viewport: number;
  // the top bar's bottom edge, and the room kept free at the screen's bottom
  top: number;
  margin: number;
}

export function placeOpenCard({ anchor, content, viewport, top, margin }: OpenCardSpace): { top: number; height: number } {
  const bottom = viewport - margin;
  const height = Math.max(0, Math.min(content, viewport * 0.6, bottom - top));
  return { top: Math.max(top, Math.min(anchor, bottom - height)), height };
}

// How far the page may scroll under an open card before it folds itself
// away (#180): past this the reader has moved on from its passage.
export const SCROLL_AWAY = 200;

// scrolledAway says whether the page has scrolled far enough from where
// it was when the card opened for the card to fold.
export function scrolledAway(from: number, now: number): boolean {
  return Math.abs(now - from) > SCROLL_AWAY;
}

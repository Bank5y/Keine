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

// touchesEdge says whether an open card, as the page scrolls under it, has
// reached the top bar or the screen's bottom (#180): there it folds itself
// away, the reader having moved on from its passage. All in screen
// coordinates; a card placed against an edge has not reached it yet.
export function touchesEdge(card: { top: number; bottom: number }, screen: { top: number; viewport: number }): boolean {
  return card.top < screen.top - 0.5 || card.bottom > screen.viewport + 0.5;
}

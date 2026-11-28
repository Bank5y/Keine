// What happens to a selection, as far as offering a bar under it goes:
// a pointer pressed (on the bar itself or elsewhere), lifted or taken over
// by the browser (a phone's long press cancels it, and no lift follows),
// and the selection changing (barTapped: a touch on the bar began a moment
// ago, which may clear the selection before the tap lands).
export type SelectionEvent =
  | { type: 'down'; onBar: boolean }
  | { type: 'up' }
  | { type: 'change'; barTapped: boolean };

// What to do after an event: read the selection now, read it once it has
// settled (keys extending it), hold (drop any read waiting: the selection
// is still being made), or nothing (null) changes.
export type SelectionStep = 'read' | 'settle' | 'hold' | null;

// selectionTiming tells when a selection is finished: once the pointer
// that makes it lifts, or once keys stop extending it; never in the middle
// of a drag, so a bar under it does not chase the pointer.
export function selectionTiming() {
  let down = false;
  return {
    on(e: SelectionEvent): SelectionStep {
      switch (e.type) {
        case 'down':
          if (!e.onBar) down = true;
          return null;
        case 'up':
          if (!down) return null;
          down = false;
          return 'read';
        case 'change':
          return down || e.barTapped ? 'hold' : 'settle';
      }
    },
  };
}

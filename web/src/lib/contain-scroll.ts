import { useEffect, type RefObject } from 'react';

// Scrolling inside a box over the page (the analysis popover, an
// exploration's card, the dialogue) stays inside it (#181).
// overscroll-behavior: contain stops a scroll from carrying on to the page
// once a box is at its end, but not a scroll over content that does not
// scroll at all; for that the box stops the wheel and the touch itself
// whenever nothing in it can take the movement.

// Scroller is one box on the way from where the pointer is out to the box
// that contains the scroll, along the axis of the movement: how far it is
// scrolled, how much shows, how long its content is, and whether it
// scrolls at all (overflow auto or scroll).
export interface Scroller { start: number; size: number; content: number; scrolls: boolean }

// takesScroll says whether one of the boxes, innermost first, can move by
// delta (positive is down or right); when none can, the movement is
// stopped before it reaches the page.
export function takesScroll(chain: Scroller[], delta: number): boolean {
  if (delta === 0) return true;
  return chain.some((s) => {
    if (!s.scrolls) return false;
    return delta > 0 ? s.start + s.size < s.content - 1 : s.start > 1;
  });
}

const scrolling = (v: string) => v === 'auto' || v === 'scroll' || v === 'overlay';

// chain is every box from el out to root, inclusive, along one axis
function chain(el: Element | null, root: HTMLElement, x: boolean): Scroller[] {
  const out: Scroller[] = [];
  for (let n = el; n; n = n.parentElement) {
    if (n instanceof HTMLElement) {
      const css = getComputedStyle(n);
      out.push(x
        ? { start: n.scrollLeft, size: n.clientWidth, content: n.scrollWidth, scrolls: scrolling(css.overflowX) && n.scrollWidth > n.clientWidth }
        : { start: n.scrollTop, size: n.clientHeight, content: n.scrollHeight, scrolls: scrolling(css.overflowY) && n.scrollHeight > n.clientHeight });
    }
    if (n === root) break;
  }
  return out;
}

// containScroll keeps the wheel and touch scrolls over root inside it; it
// returns what undoes it.
export function containScroll(root: HTMLElement): () => void {
  const stop = (e: Event, dx: number, dy: number) => {
    const x = Math.abs(dx) > Math.abs(dy);
    if (!takesScroll(chain(e.target as Element, root, x), x ? dx : dy) && e.cancelable) e.preventDefault();
  };
  const wheel = (e: WheelEvent) => { if (!e.ctrlKey) stop(e, e.deltaX, e.deltaY); }; // ctrl is a pinch
  let last: { x: number; y: number } | null = null;
  const touchstart = (e: TouchEvent) => { last = e.touches.length === 1 ? { x: e.touches[0].clientX, y: e.touches[0].clientY } : null; };
  const touchmove = (e: TouchEvent) => {
    if (!last || e.touches.length !== 1) return;
    const t = e.touches[0];
    // the finger moving up scrolls the content down
    stop(e, last.x - t.clientX, last.y - t.clientY);
    last = { x: t.clientX, y: t.clientY };
  };
  root.addEventListener('wheel', wheel, { passive: false });
  root.addEventListener('touchstart', touchstart, { passive: true });
  root.addEventListener('touchmove', touchmove, { passive: false });
  return () => {
    root.removeEventListener('wheel', wheel);
    root.removeEventListener('touchstart', touchstart);
    root.removeEventListener('touchmove', touchmove);
  };
}

// useContainScroll keeps the scrolls over the box inside it while it is
// mounted.
export function useContainScroll(box: RefObject<HTMLElement | null>, on = true) {
  useEffect(() => {
    const el = box.current;
    if (!el || !on) return undefined;
    return containScroll(el);
  }, [box, on]);
}

let locks = 0;
// useScrollLock holds the page still while a modal is open: the keys
// cannot scroll it either. The page keeps its scroll position, and its
// scrollbar's room (scrollbar-gutter: stable), so nothing moves.
export function useScrollLock(on = true) {
  useEffect(() => {
    if (!on) return undefined;
    const html = document.documentElement;
    if (locks++ === 0) html.style.overflow = 'hidden';
    return () => { if (--locks === 0) html.style.overflow = ''; };
  }, [on]);
}

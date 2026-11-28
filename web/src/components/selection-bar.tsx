import { useEffect, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import { selectionTiming, type SelectionStep } from '@/lib/finished-selection';
import { useKeyboardInset } from '@/lib/use-keyboard-inset';
import { useMedia } from '@/lib/use-media';
import { cn } from '@/lib/utils';

// A tap on a bar may clear the selection on a touch screen before the tap
// lands, and a bar that follows the selection would go before its button
// is pressed. barTaps listens for touches on the bar: recent() tells whether
// one began within TAP_GRACE ms, so the bar stays for it; stop() ends it.
const TAP_GRACE = 600;
export function barTaps(bar: RefObject<HTMLElement | null>) {
  let at = -Infinity;
  const onDown = (e: PointerEvent) => { if (e.pointerType !== 'mouse' && bar.current?.contains(e.target as Node)) at = performance.now(); };
  document.addEventListener('pointerdown', onDown, true);
  return { recent: () => performance.now() - at < TAP_GRACE, stop: () => document.removeEventListener('pointerdown', onDown, true) };
}

// a spot on the page, in document coordinates, that a floating bar points at
export interface Spot { x: number; top: number; bottom: number }
export const spotOf = (r: DOMRect): Spot => ({ x: r.left + r.width / 2 + scrollX, top: r.top + scrollY, bottom: r.bottom + scrollY });

// how long a selection made with keys rests before it counts as finished
const SETTLE_MS = 120;

// useFinishedSelection calls read once a text selection on the page is finished
// (see selectionTiming): when the pointer making it lifts, or when keys stop
// extending it; not while it is being dragged. bar is the bar the selection
// offers, whose taps do not count as making a selection. Off (on false), it
// listens to nothing.
export function useFinishedSelection(bar: RefObject<HTMLElement | null>, read: () => void, on = true) {
  const latest = useRef(read);
  useEffect(() => { latest.current = read; });
  useEffect(() => {
    if (!on) return undefined;
    const timing = selectionTiming();
    const taps = barTaps(bar);
    let t = 0;
    const step = (s: SelectionStep) => {
      if (!s) return;
      clearTimeout(t);
      if (s !== 'hold') t = window.setTimeout(() => latest.current(), s === 'read' ? 0 : SETTLE_MS);
    };
    const onChange = () => step(timing.on({ type: 'change', barTapped: taps.recent() }));
    const onDown = (e: PointerEvent) => step(timing.on({ type: 'down', onBar: !!bar.current?.contains(e.target as Node) }));
    const onUp = () => step(timing.on({ type: 'up' }));
    document.addEventListener('selectionchange', onChange);
    document.addEventListener('pointerdown', onDown);
    document.addEventListener('pointerup', onUp);
    // a phone takes a long press over for its own selection and cancels the
    // pointer: no pointerup follows, so the cancel ends the press instead
    document.addEventListener('pointercancel', onUp);
    return () => {
      clearTimeout(t);
      taps.stop();
      document.removeEventListener('selectionchange', onChange);
      document.removeEventListener('pointerdown', onDown);
      document.removeEventListener('pointerup', onUp);
      document.removeEventListener('pointercancel', onUp);
    };
  }, [bar, on]);
}

// Touch screens dock the bar to the bottom of the screen: there the
// browser's own menu for a selection (copy, share, select all…) takes the
// space above it, where a floating bar would go, and covers it.
const DOCK_MEDIA = '(pointer: coarse)';

// Floating sits above the spot, or below it when there is no room above; on
// a touch screen it docks to the bottom of the screen instead, above the
// keyboard while one is up and above the page's bottom bar, if any.
export function Floating({ spot, ref, label, stacked, children }: { spot: Spot; ref: RefObject<HTMLDivElement | null>; label: string; stacked?: boolean; children: ReactNode }) {
  const docked = useMedia(DOCK_MEDIA);
  const keyboard = useKeyboardInset(docked);
  const [below, setBelow] = useState(false);
  const [shift, setShift] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el || docked) return;
    const h = el.offsetHeight, w = el.offsetWidth;
    setBelow(spot.top - scrollY - h - 10 < 64);
    const left = spot.x - scrollX - w / 2;
    setShift(left < 8 ? 8 - left : (left + w > innerWidth - 8 ? innerWidth - 8 - (left + w) : 0));
  }, [spot, ref, children, docked]);
  const style: CSSProperties = docked
    ? (keyboard ? { '--keyboard': `${keyboard}px` } as CSSProperties : {})
    : { left: spot.x + shift, top: below ? spot.bottom + 8 : spot.top - 8, transform: below ? 'translateX(-50%)' : 'translate(-50%, -100%)' };
  return createPortal(
    // clicks keep the selection, except in the note's own field
    <div ref={ref} role="toolbar" aria-label={label} className={cn('ann-bar', stacked && 'stacked', docked && 'docked', docked && keyboard > 0 && 'over-keyboard')}
      onMouseDown={(e) => { if (!(e.target instanceof HTMLTextAreaElement)) e.preventDefault(); }} style={style}>
      {children}
    </div>,
    // in the shell, a docked bar reads the height of the page's bottom bar
    (docked && document.querySelector('.shell')) || document.body,
  );
}

// SelectionBar offers a bar under a finished text selection (useFinishedSelection),
// the same bar a lesson's text offers: above the selection on a wide
// screen, docked to the bottom of the screen on a touch one. read turns the
// selection's range into what the bar acts on, or null where it offers
// nothing; children draw the bar's buttons, and done() puts the selection
// and the bar away. Inside a scroller of its own, the bar is placed on the
// page, so it follows the selection as the scroller moves (a reader's
// scroll, or a reply streaming in below), and goes once the selection
// leaves the scroller's view.
export function SelectionBar<T>({ read, label, scroller, children }: {
  read: (range: Range) => T | null; label: string; scroller?: RefObject<HTMLElement | null>; children: (value: T, done: () => void) => ReactNode;
}) {
  const bar = useRef<HTMLDivElement>(null);
  const [pick, setPick] = useState<{ value: T; range: Range; spot: Spot } | null>(null);
  useFinishedSelection(bar, () => {
    const s = getSelection();
    const range = s && !s.isCollapsed && s.rangeCount ? s.getRangeAt(0).cloneRange() : null;
    const value = range ? read(range) : null;
    setPick(range && value != null ? { value, range, spot: spotOf(range.getBoundingClientRect()) } : null);
  });
  useEffect(() => {
    const el = scroller?.current;
    if (!el || !pick) return undefined;
    const follow = () => {
      const r = pick.range.getBoundingClientRect();
      const box = el.getBoundingClientRect();
      setPick(r.bottom < box.top || r.top > box.bottom ? null : { ...pick, spot: spotOf(r) });
    };
    el.addEventListener('scroll', follow, { passive: true });
    return () => el.removeEventListener('scroll', follow);
  }, [scroller, pick]);
  if (!pick) return null;
  const done = () => { getSelection()?.removeAllRanges(); setPick(null); };
  return <Floating spot={pick.spot} ref={bar} label={label}>{children(pick.value, done)}</Floating>;
}

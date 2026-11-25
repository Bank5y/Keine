import { useCallback, useEffect, useRef, useState, type CSSProperties, type PointerEvent } from 'react';
import { createPortal } from 'react-dom';
import { Telescope } from 'lucide-react';
import { ExplorationPage } from '@/components/exploration-page';
import { useContainScroll, useScrollLock } from '@/lib/contain-scroll';
import { closedSheet, DETENT_TOP, nextSheet, type SheetEvent, type SheetState } from '@/lib/exploration-sheet';
import type { Exploration, ExplorationDetail } from '@/lib/explorations';
import { cn } from '@/lib/utils';

// a press that moves this far is a drag, not a tap
const DRAG_START = 6;

// the history entry the whole-screen exploration stands on
const ENTRY = { keineSheet: true };

// useSheet keeps a narrow screen's sheet of explorations (see
// exploration-sheet.ts) and does to the browser's history what each step
// asks: the whole-screen exploration gets an entry of its own, so the
// system's back closes it rather than leaving the lesson, and a back while
// it is up arrives as a back event.
export function useSheet(): [SheetState, (e: SheetEvent) => void] {
  const [sheet, setSheet] = useState(closedSheet);
  const cur = useRef(sheet);
  const send = useCallback((e: SheetEvent) => {
    const step = nextSheet(cur.current, e);
    cur.current = step.state;
    setSheet(step.state);
    if (step.history === 'push') history.pushState(ENTRY, '');
    else if (step.history === 'back') history.back();
  }, []);
  const full = sheet.detent === 'full';
  useEffect(() => {
    if (!full) return undefined;
    const onPop = () => send({ type: 'back' });
    addEventListener('popstate', onPop);
    return () => removeEventListener('popstate', onPop);
  }, [full, send]);
  return [sheet, send];
}

// ExplorationSheet is the sheet a narrow screen's explorations come up in,
// from the foot of the screen (see exploration-sheet.ts). As the card
// (peek) it shows the exploration's title and concept and 展开探索; the
// card follows a finger dragging it and tells onRelease how far down it
// was let go. On the whole screen it is the exploration's dialogue
// (ExplorationPage). Closed, it keeps showing what it held as it slides
// away.
export function ExplorationSheet({ detent, exploration, onRelease, onExpand, onClose, onDetail }: {
  detent: SheetState['detent']; exploration: Exploration | null; onRelease: (dy: number) => void;
  onExpand: () => void; onClose: () => void; onDetail: (d: ExplorationDetail) => void;
}) {
  const [shown, setShown] = useState(exploration);
  if (exploration && exploration !== shown) setShown(exploration);
  const [dy, setDy] = useState<number | null>(null);
  const press = useRef<{ id: number; y: number; dragging: boolean } | null>(null);
  const box = useRef<HTMLDivElement>(null);
  const full = detent === 'full';
  // the whole-screen page stays on as it slides away, closed
  const [paged, setPaged] = useState(full);
  if (full && !paged) setPaged(true);
  if (detent === 'peek' && paged) setPaged(false);
  useScrollLock(full);
  useContainScroll(box, full);

  const onDown = (e: PointerEvent<HTMLDivElement>) => {
    if (detent !== 'peek') return;
    press.current = { id: e.pointerId, y: e.clientY, dragging: false };
  };
  const onMove = (e: PointerEvent<HTMLDivElement>) => {
    const p = press.current;
    if (!p || p.id !== e.pointerId) return;
    const d = e.clientY - p.y;
    if (!p.dragging && Math.abs(d) < DRAG_START) return;
    if (!p.dragging) { p.dragging = true; e.currentTarget.setPointerCapture(e.pointerId); }
    // up as far as the top of the screen
    setDy(Math.max(d, -DETENT_TOP.peek * innerHeight));
  };
  const onUp = (e: PointerEvent<HTMLDivElement>) => {
    const p = press.current;
    press.current = null;
    if (!p?.dragging || p.id !== e.pointerId) return;
    setDy(null);
    onRelease(e.clientY - p.y);
  };

  const open = detent !== 'closed';
  const style = { transform: `translateY(calc(${DETENT_TOP[detent] * 100}% + ${dy ?? 0}px))` } as CSSProperties;
  return createPortal(
    <div ref={box} className={cn('exp-sheet', `at-${detent}`, paged && 'paged', dy != null && 'dragging')} style={style}
      role="dialog" aria-modal={full} aria-label={shown ? `探索：${shown.title}` : '探索'} aria-hidden={!open} inert={!open}
      onPointerDown={onDown} onPointerMove={onMove} onPointerUp={onUp} onPointerCancel={onUp}>
      {(full || paged) && shown ? (
        <ExplorationPage key={shown.id} id={shown.id} onClose={onClose} onDetail={onDetail} />
      ) : (
        <>
          <span className="exp-sheet-grip" aria-hidden />
          {shown ? (
            <div className="exp-sheet-body">
              <div className="exp-sheet-head">
                <h2 className="exp-sheet-title"><Telescope aria-hidden /><span>{shown.title}</span></h2>
                <button type="button" className="exp-sheet-expand" onClick={onExpand}><Telescope aria-hidden />展开探索</button>
              </div>
              <p className="exp-sheet-concept">{shown.concept}</p>
            </div>
          ) : null}
        </>
      )}
    </div>,
    document.querySelector('.shell') || document.body,
  );
}

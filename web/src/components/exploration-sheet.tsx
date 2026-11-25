import { useCallback, useEffect, useRef, useState, type CSSProperties, type PointerEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { Telescope } from 'lucide-react';
import { ExplorationPage } from '@/components/exploration-page';
import { useContainScroll, useScrollLock } from '@/lib/contain-scroll';
import { closedSheet, DETENT_TOP, DRAG_COMMIT, nextSheet, type SheetEvent, type SheetState } from '@/lib/exploration-sheet';
import type { Exploration, ExplorationDetail } from '@/lib/explorations';
import { cn } from '@/lib/utils';

// a press that moves this far is a drag, not a tap
const DRAG_START = 6;

// the history entry the whole-screen exploration stands on
export const ENTRY = { keineSheet: true };

// useSheet keeps a narrow screen's sheet of explorations (see
// exploration-sheet.ts) and does to the browser's history what each step
// asks: the whole-screen exploration and the half screen get an entry of
// their own, so the system's back closes them rather than leaving the
// lesson, and a back while one is up arrives as a back event. heldBack,
// when it returns true, has taken a back itself (and put the entry back).
export function useSheet(heldBack?: () => boolean): [SheetState, (e: SheetEvent) => void] {
  const [sheet, setSheet] = useState(closedSheet);
  const cur = useRef(sheet);
  const send = useCallback((e: SheetEvent) => {
    const step = nextSheet(cur.current, e);
    cur.current = step.state;
    setSheet(step.state);
    if (step.history === 'push') history.pushState(ENTRY, '');
    else if (step.history === 'back') history.back();
  }, []);
  const held = useRef(heldBack);
  held.current = heldBack;
  const entry = sheet.detent === 'full' || sheet.detent === 'half';
  useEffect(() => {
    if (!entry) return undefined;
    const onPop = () => { if (!held.current?.()) send({ type: 'back' }); };
    addEventListener('popstate', onPop);
    return () => removeEventListener('popstate', onPop);
  }, [entry, send]);
  return [sheet, send];
}

// ExplorationSheet is the sheet a narrow screen's explorations come up in,
// from the foot of the screen (see exploration-sheet.ts). As the card
// (peek) it shows the exploration's title and concept and 展开探索; the
// card follows a finger dragging it and, let go, tells send how far. On
// the whole screen it is the exploration's dialogue (ExplorationPage).
// Half the screen holds half (an analysis, the lesson's list), over a
// scrim, dragged by its grip; a tap on the scrim or a pull down asks
// onDismiss to close it. Closed, it keeps showing the exploration it held
// as it slides away.
export function ExplorationSheet({ sheet, exploration, half, send, onDismiss, onDetail, onDeleted }: {
  sheet: SheetState; exploration: Exploration | null; half: ReactNode; send: (e: SheetEvent) => void; onDismiss: () => void;
  onDetail: (d: ExplorationDetail) => void; onDeleted: (id: number) => void;
}) {
  const { detent } = sheet;
  const [shown, setShown] = useState(exploration);
  if (exploration && exploration !== shown) setShown(exploration);
  // what the sheet held last, kept as it slides away
  const body = sheet.body?.kind ?? null;
  const [held, setHeld] = useState(body);
  if (body && body !== held) setHeld(body);
  const [dy, setDy] = useState<number | null>(null);
  const press = useRef<{ id: number; y: number; dragging: boolean } | null>(null);
  const box = useRef<HTMLDivElement>(null);
  const full = detent === 'full';
  // the whole-screen page stays on as it slides away, closed
  const [paged, setPaged] = useState(full);
  if (full && !paged) setPaged(true);
  if (detent === 'peek' && paged) setPaged(false);
  useScrollLock(full || detent === 'half');
  useContainScroll(box, full || detent === 'half');

  const onDown = (e: PointerEvent<HTMLDivElement>) => {
    // the card drags from anywhere; the half screen, whose content scrolls, by its grip
    if (detent !== 'peek' && !(detent === 'half' && (e.target as Element).closest('.exp-sheet-grip'))) return;
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
    const dy = e.clientY - p.y;
    if (detent === 'half' && dy > DRAG_COMMIT) onDismiss();
    else send({ type: 'release', dy });
  };

  const open = detent !== 'closed';
  const style = { transform: `translateY(calc(${DETENT_TOP[detent] * 100}% + ${dy ?? 0}px))` } as CSSProperties;
  return createPortal(
    <>
    <div className={cn('exp-sheet-scrim', detent === 'half' && 'on')} onClick={onDismiss} aria-hidden />
    <div ref={box} className={cn('exp-sheet', `at-${detent}`, paged && 'paged', dy != null && 'dragging')} style={style}
      role="dialog" aria-modal={full} aria-label={shown ? `探索：${shown.title}` : '探索'} aria-hidden={!open} inert={!open}
      onPointerDown={onDown} onPointerMove={onMove} onPointerUp={onUp} onPointerCancel={onUp}>
      {(full || paged) && shown ? (
        <ExplorationPage key={shown.id} id={shown.id} tree={sheet.tree} report={sheet.report} send={send} onDetail={onDetail} onDeleted={onDeleted} />
      ) : (
        held !== 'exploration' ? (
          <>
            <span className="exp-sheet-grip" aria-hidden />
            {/* an analysis left running unseen would spend the reader's credits: what
                half holds goes as the sheet leaves the half screen */}
            <div className="exp-sheet-half">{detent === 'half' ? half : null}</div>
          </>
        ) : <>
          <span className="exp-sheet-grip" aria-hidden />
          {shown ? (
            <div className="exp-sheet-body">
              <div className="exp-sheet-head">
                <h2 className="exp-sheet-title"><Telescope aria-hidden /><span>{shown.title}</span></h2>
                <button type="button" className="exp-sheet-expand" onClick={() => send({ type: 'expand' })}><Telescope aria-hidden />展开探索</button>
              </div>
              <p className="exp-sheet-concept">{shown.concept}</p>
            </div>
          ) : null}
        </>
      )}
    </div>
    </>,
    document.querySelector('.shell') || document.body,
  );
}

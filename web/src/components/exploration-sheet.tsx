import { useRef, useState, type CSSProperties, type PointerEvent } from 'react';
import { createPortal } from 'react-dom';
import { Telescope } from 'lucide-react';
import { DETENT_TOP, type Detent } from '@/lib/exploration-sheet';
import type { Exploration } from '@/lib/explorations';
import { cn } from '@/lib/utils';

// a press that moves this far is a drag, not a tap
const DRAG_START = 6;

// ExplorationSheet is the sheet a narrow screen's explorations come up in,
// from the foot of the screen (see exploration-sheet.ts). As the card
// (peek) it shows the exploration's title and concept and 展开探索, which
// is to open the whole-screen exploration (#200). It follows a finger
// dragging it and tells onRelease how far down it was let go. Closed, it
// keeps showing what it held as it slides away.
export function ExplorationSheet({ detent, exploration, onRelease }: {
  detent: Detent; exploration: Exploration | null; onRelease: (dy: number) => void;
}) {
  const [shown, setShown] = useState(exploration);
  if (exploration && exploration !== shown) setShown(exploration);
  const [dy, setDy] = useState<number | null>(null);
  const press = useRef<{ id: number; y: number; dragging: boolean } | null>(null);

  const onDown = (e: PointerEvent<HTMLDivElement>) => { press.current = { id: e.pointerId, y: e.clientY, dragging: false }; };
  const onMove = (e: PointerEvent<HTMLDivElement>) => {
    const p = press.current;
    if (!p || p.id !== e.pointerId) return;
    const d = e.clientY - p.y;
    if (!p.dragging && Math.abs(d) < DRAG_START) return;
    if (!p.dragging) { p.dragging = true; e.currentTarget.setPointerCapture(e.pointerId); }
    // the card only goes down for now: pulled up, it opens the exploration (#200)
    setDy(Math.max(0, d));
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
    <div className={cn('exp-sheet', `at-${detent}`, dy != null && 'dragging')} style={style}
      role="dialog" aria-modal="false" aria-label={shown ? `探索：${shown.title}` : '探索'} aria-hidden={!open} inert={!open}
      onPointerDown={onDown} onPointerMove={onMove} onPointerUp={onUp} onPointerCancel={onUp}>
      <span className="exp-sheet-grip" aria-hidden />
      {shown ? (
        <div className="exp-sheet-body">
          <div className="exp-sheet-head">
            <h2 className="exp-sheet-title"><Telescope aria-hidden /><span>{shown.title}</span></h2>
            <button type="button" className="exp-sheet-expand"><Telescope aria-hidden />展开探索</button>
          </div>
          <p className="exp-sheet-concept">{shown.concept}</p>
        </div>
      ) : null}
    </div>,
    document.querySelector('.shell') || document.body,
  );
}

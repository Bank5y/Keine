import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import { Dialog as D } from 'radix-ui';
import type { Drawn } from '@/components/mermaid';
import { fitView, zoomAt, type View } from '@/lib/pan-zoom';

// ImageZoom shows one image over the page; click anywhere or press Esc to close.
export function ImageZoom({ src, onClose }: { src: string | null; onClose: () => void }) {
  return (
    <D.Root open={!!src} onOpenChange={(v) => { if (!v) onClose(); }}>
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-40 bg-black/75 data-[state=open]:animate-in data-[state=open]:fade-in-0" />
        <D.Content aria-describedby={undefined} className="fixed inset-0 z-50 flex cursor-zoom-out items-center justify-center p-6 outline-none" onClick={onClose}>
          <D.Title className="sr-only">查看图片</D.Title>
          {src ? <img src={src} alt="" className="max-h-full max-w-full rounded-[2px] bg-white object-contain shadow-2xl" /> : null}
        </D.Content>
      </D.Portal>
    </D.Root>
  );
}

const PAD = 24;
const LIMITS = { min: 0.1, max: 10 };
// CLICK_SLOP is how far a press may move and still be a click, not a drag.
const CLICK_SLOP = 4;

// DiagramZoom shows a diagram over the page (#184), fitted to the screen
// first. The wheel or a pinch zooms about the pointer, a drag pans; Esc or
// a click on the backdrop closes it.
export function DiagramZoom({ picture, onClose }: { picture: Drawn; onClose: () => void }) {
  // the stage, once the portal has put it on the page
  const [stage, setStage] = useState<HTMLDivElement | null>(null);
  const [view, setView] = useState<View>(() => fitView(picture, { w: innerWidth, h: innerHeight }, PAD));
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const moved = useRef(0);
  // whether the press began on the diagram: the stage captures the
  // pointer, so the release always lands on the stage
  const onPicture = useRef(false);

  // the wheel listens itself, since React's wheel handler cannot prevent
  // the page from scrolling
  useEffect(() => {
    const el = stage;
    if (!el) return undefined;
    const wheel = (e: WheelEvent) => {
      e.preventDefault();
      // a trackpad pinch comes as a wheel with ctrl, in small steps
      const factor = Math.exp(-e.deltaY * (e.ctrlKey ? 0.01 : 0.0015));
      setView((v) => zoomAt(v, factor, { x: e.clientX, y: e.clientY }, LIMITS));
    };
    el.addEventListener('wheel', wheel, { passive: false });
    return () => el.removeEventListener('wheel', wheel);
  }, [stage]);

  const down = (e: ReactPointerEvent) => {
    e.currentTarget.setPointerCapture(e.pointerId);
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (pointers.current.size === 1) { moved.current = 0; onPicture.current = !!(e.target as Element).closest('img'); }
  };
  const move = (e: ReactPointerEvent) => {
    const ps = pointers.current;
    const was = ps.get(e.pointerId);
    if (!was) return;
    const now = { x: e.clientX, y: e.clientY };
    if (ps.size === 1) {
      moved.current += Math.abs(now.x - was.x) + Math.abs(now.y - was.y);
      setView((v) => ({ ...v, x: v.x + now.x - was.x, y: v.y + now.y - was.y }));
    } else if (ps.size === 2) {
      // a pinch: zoom by the change in the fingers' distance, about their middle
      const other = [...ps].find(([id]) => id !== e.pointerId)![1];
      const before = Math.hypot(was.x - other.x, was.y - other.y);
      const after = Math.hypot(now.x - other.x, now.y - other.y);
      moved.current += CLICK_SLOP;
      if (before > 0) setView((v) => zoomAt(v, after / before, { x: (now.x + other.x) / 2, y: (now.y + other.y) / 2 }, LIMITS));
    }
    ps.set(e.pointerId, now);
  };
  const up = (e: ReactPointerEvent) => {
    pointers.current.delete(e.pointerId);
    // a click on the backdrop, not on the diagram and not the end of a drag
    if (!pointers.current.size && moved.current < CLICK_SLOP && !onPicture.current) onClose();
  };
  const cancel = (e: ReactPointerEvent) => { pointers.current.delete(e.pointerId); };

  return (
    <D.Root open onOpenChange={(v) => { if (!v) onClose(); }}>
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-[70] bg-black/75 data-[state=open]:animate-in data-[state=open]:fade-in-0" />
        <D.Content aria-describedby={undefined} data-diagram-zoom="" className="diagram-zoom fixed inset-0 z-[70] outline-none"
          onPointerDownOutside={(e) => e.preventDefault()}>
          <D.Title className="sr-only">查看图表</D.Title>
          <div ref={setStage} className="diagram-zoom-stage" onPointerDown={down} onPointerMove={move} onPointerUp={up} onPointerCancel={cancel}>
            <img src={picture.src} alt="图表" draggable={false} width={picture.w} height={picture.h}
              style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }} />
          </div>
        </D.Content>
      </D.Portal>
    </D.Root>
  );
}

import { useEffect, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import { X } from 'lucide-react';
import { Dialog as D } from 'radix-ui';
import { MAX_FIT, fitView, zoomAt, type Size, type View } from '@/lib/pan-zoom';

// Picture is what the lightbox shows: an image's address and its own size,
// and whether it is drawn as vectors (a diagram, an SVG), crisp at any size.
export interface Picture extends Size { src: string; vector?: boolean }

const PAD = 24;
const LIMITS = { min: 0.1, max: 10 };
// CLICK_SLOP is how far a press may move and still be a click, not a drag.
const CLICK_SLOP = 4;

// Lightbox shows a picture over the page (#184, #187), fitted to the screen
// first. The wheel or a pinch zooms about the pointer, a drag pans; Esc,
// the × fixed at the top right, or a click on the backdrop closes it, a
// click on the picture does not. A vector picture (a diagram) is enlarged
// to fit; a bitmap no further than its own size, lest it blur — zooming by
// hand is not held back.
export function Lightbox({ picture, label = '图片', onClose }: { picture: Picture; label?: string; onClose: () => void }) {
  // the stage, once the portal has put it on the page
  const [stage, setStage] = useState<HTMLDivElement | null>(null);
  const fit = (box: Size) => fitView(picture, box, PAD, picture.vector ? MAX_FIT : 1);
  const [view, setView] = useState<View>(() => fit({ w: innerWidth, h: innerHeight }));
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const moved = useRef(0);
  // whether the press began on the picture: the stage captures the
  // pointer, so the release always lands on the stage
  const onPicture = useRef(false);

  // fit to the stage itself, before it is first painted: it stops short of
  // the scrollbar gutter the page keeps, so the window is too wide. Only
  // once: a re-render with a fresh picture object must not undo a zoom.
  useLayoutEffect(() => {
    if (stage) setView(fit({ w: stage.clientWidth, h: stage.clientHeight }));
  }, [stage]); // eslint-disable-line react-hooks/exhaustive-deps

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
    // a click on the backdrop, not on the picture and not the end of a drag
    if (!pointers.current.size && moved.current < CLICK_SLOP && !onPicture.current) onClose();
  };
  const cancel = (e: ReactPointerEvent) => { pointers.current.delete(e.pointerId); };

  return (
    <D.Root open onOpenChange={(v) => { if (!v) onClose(); }}>
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-[70] bg-black/75 motion-safe:data-[state=open]:animate-in motion-safe:data-[state=open]:fade-in-0" />
        <D.Content aria-describedby={undefined} data-lightbox="" className="lightbox fixed inset-0 z-[70] outline-none"
          onPointerDownOutside={(e) => e.preventDefault()}>
          <D.Title className="sr-only">查看{label}</D.Title>
          <div ref={setStage} className="lightbox-stage" onPointerDown={down} onPointerMove={move} onPointerUp={up} onPointerCancel={cancel}>
            <img src={picture.src} alt={label} draggable={false} width={picture.w} height={picture.h}
              style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }} />
          </div>
          <D.Close className="lightbox-close" aria-label="关闭"><X aria-hidden /></D.Close>
        </D.Content>
      </D.Portal>
    </D.Root>
  );
}

import { useEffect, useRef, type RefObject } from 'react';

const FADE = '28px';

// useScrollFade fades the top and bottom edges of a scrolling box, each only
// while there is more to scroll that way (the box needs the .side-fade class).
// onChange runs after every scroll or size change, for whatever else depends
// on what is in view.
export function useScrollFade(box: RefObject<HTMLElement | null>, onChange?: () => void) {
  const cb = useRef(onChange);
  cb.current = onChange;

  useEffect(() => {
    const el = box.current;
    if (!el) return undefined;
    let frame = 0;
    const update = () => {
      frame = 0;
      const top = el.scrollTop > 1;
      const bottom = el.scrollTop + el.clientHeight < el.scrollHeight - 1;
      el.style.setProperty('--fade-top', top ? FADE : '0px');
      el.style.setProperty('--fade-bottom', bottom ? FADE : '0px');
      cb.current?.();
    };
    const schedule = () => { if (!frame) frame = requestAnimationFrame(update); };
    // the content grows and shrinks as chapters open, close and load
    const ro = new ResizeObserver(schedule);
    const watch = () => { ro.disconnect(); ro.observe(el); for (const c of el.children) ro.observe(c); };
    const mo = new MutationObserver(() => { watch(); schedule(); });
    watch();
    mo.observe(el, { childList: true });
    el.addEventListener('scroll', schedule, { passive: true });
    update();
    return () => { ro.disconnect(); mo.disconnect(); el.removeEventListener('scroll', schedule); cancelAnimationFrame(frame); };
  }, [box]);
}

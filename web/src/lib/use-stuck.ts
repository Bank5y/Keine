import { useEffect, type RefObject } from 'react';

// useStuck marks a position: sticky element with the .stuck class while it
// is held at its sticky top, so it can show an edge only then. An element
// that is not sticky at the current width is never stuck.
export function useStuck(target: RefObject<HTMLElement | null>) {
  useEffect(() => {
    const el = target.current;
    if (!el) return undefined;
    let frame = 0;
    const update = () => {
      frame = 0;
      const css = getComputedStyle(el);
      const top = parseFloat(css.top);
      const stuck = css.position === 'sticky' && !Number.isNaN(top) && window.scrollY > 0 && el.getBoundingClientRect().top <= top + 0.5;
      el.classList.toggle('stuck', stuck);
    };
    const schedule = () => { if (!frame) frame = requestAnimationFrame(update); };
    update();
    window.addEventListener('scroll', schedule, { passive: true });
    window.addEventListener('resize', schedule);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener('scroll', schedule);
      window.removeEventListener('resize', schedule);
    };
  }, [target]);
}

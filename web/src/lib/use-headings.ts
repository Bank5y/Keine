import { useCallback, useEffect, useRef, useState, type RefObject } from 'react';
import { pinHeading } from '@/lib/heading-jump';

export interface Heading { id: string; text: string; level: number }

// useHeadings collects the h2/h3 headings rendered inside root (the Markdown
// arrives asynchronously, so it watches for them) and tracks which one the
// reader is currently in. jump brings a heading to the top of the page and
// holds it there while the page settles; the heading it jumped to counts as
// current until the reader scrolls on their own, even where the end of the
// text keeps it from reaching the top.
export function useHeadings(root: RefObject<HTMLElement | null>, key: unknown) {
  const [headings, setHeadings] = useState<Heading[]>([]);
  const [active, setActive] = useState('');
  const [picked, setPicked] = useState('');
  const release = useRef<(() => void) | null>(null);

  const jump = useCallback((id: string, behavior: ScrollBehavior) => {
    const el = document.getElementById(id);
    if (!el) return;
    release.current?.();
    setPicked(id);
    release.current = pinHeading(el, behavior, () => { release.current = null; setPicked(''); });
  }, []);

  // a new lesson (or leaving) lets go of the last jump
  useEffect(() => () => { release.current?.(); release.current = null; setPicked(''); }, [key]);

  useEffect(() => {
    const el = root.current;
    if (!el) return undefined;
    const collect = () => {
      const hs = [...el.querySelectorAll<HTMLElement>('.reader-prose h2[id], .reader-prose h3[id]')];
      setHeadings((prev) => {
        const next = hs.map((h) => ({ id: h.id, text: h.textContent || '', level: h.tagName === 'H2' ? 2 : 3 }));
        return JSON.stringify(prev) === JSON.stringify(next) ? prev : next;
      });
    };
    collect();
    const mo = new MutationObserver(collect);
    mo.observe(el, { childList: true, subtree: true });
    return () => mo.disconnect();
  }, [root, key]);

  useEffect(() => {
    if (!headings.length) { setActive(''); return undefined; }
    const onScroll = () => {
      let cur = headings[0].id;
      for (const h of headings) {
        const node = document.getElementById(h.id);
        if (node && node.getBoundingClientRect().top < 120) cur = h.id;
      }
      setActive(cur);
    };
    onScroll();
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => window.removeEventListener('scroll', onScroll);
  }, [headings]);

  return { headings, active: picked || active, jump };
}

import { useEffect, useState, type RefObject } from 'react';

export interface Heading { id: string; text: string; level: number }

// useHeadings collects the h2/h3 headings rendered inside root (the Markdown
// arrives asynchronously, so it watches for them) and tracks which one the
// reader is currently in.
export function useHeadings(root: RefObject<HTMLElement | null>, key: unknown) {
  const [headings, setHeadings] = useState<Heading[]>([]);
  const [active, setActive] = useState('');

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

  return { headings, active };
}

import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { documentParts } from '@/components/analysis-popover';
import { ExplorationDialogue } from '@/components/exploration-dialogue';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { useContainScroll, useScrollLock } from '@/lib/contain-scroll';
import type { ExplorationDetail } from '@/lib/explorations';
import { cn } from '@/lib/utils';

const MS = 320;
const EASE = 'cubic-bezier(.2,.8,.2,1)';

export const reducedMotion = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

// the exploration's card open in the rail, where the dialogue grows from
const openCard = (id: number) => document.querySelector<HTMLElement>(`.exp-card-open[data-exploration="${id}"]`);

// flip plays a box's move from rect `from` to where it now is (FLIP): it
// is drawn at `from` first and let go to its own place.
function flip(el: HTMLElement, from: DOMRect) {
  const to = el.getBoundingClientRect();
  if (!to.width || !to.height) return;
  el.style.transition = 'none';
  el.style.transformOrigin = 'top left';
  el.style.transform = `translate(${from.left - to.left}px, ${from.top - to.top}px) scale(${from.width / to.width}, ${from.height / to.height})`;
  void el.offsetWidth;
  el.style.transition = `transform ${MS}ms ${EASE}`;
  el.style.transform = 'none';
}

// ExplorationZoom holds an exploration's dialogue over the page (#134):
// it grows out of the exploration's card open in the rail (#180) to most
// of the screen, the page darkened and still under it (#181). Esc or a
// press on the backdrop closes it, back to the card, still open. With
// reduced motion nothing moves.
export function ExplorationZoom({ detail, onClose, onDetail }: {
  detail: ExplorationDetail; onClose: () => void; onDetail: (d: ExplorationDetail) => void;
}) {
  const [d, setD] = useState(detail);
  const [closing, setClosing] = useState(false);
  const card = useRef<HTMLDivElement>(null);
  const root = useRef<HTMLDivElement>(null);
  useScrollLock();
  useContainScroll(root);

  useLayoutEffect(() => {
    const from = openCard(detail.id)?.getBoundingClientRect();
    if (card.current && from && !reducedMotion()) flip(card.current, from);
  }, [detail.id]);

  const close = () => {
    if (closing) return;
    const el = card.current;
    if (!el || reducedMotion()) { onClose(); return; }
    setClosing(true);
    el.style.transition = `opacity ${MS * 0.6}ms ${EASE}`;
    el.style.opacity = '0';
    window.setTimeout(onClose, MS * 0.6);
  };

  // Esc closes the zoom, unless the dialogue takes it (going up a node) or
  // a dialog above it is open
  const escTaken = useRef<() => boolean>(() => false);
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented || escTaken.current()) return;
      close();
    };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  });

  return createPortal(
    <div ref={root} className={cn('exp-zoom', closing && 'closing')} role="dialog" aria-modal="true" aria-label={`探索：${d.title}`}>
      <div className="exp-zoom-backdrop" onClick={close} />
      <div ref={card} className="exp-zoom-card">
        <ExplorationDialogue detail={d} onDetail={(x) => { setD(x); onDetail(x); }} onClose={close} escTaken={escTaken} />
      </div>
    </div>,
    document.body,
  );
}

// ExplorationDocument is the exploration document, read only: the passage
// it was made on, then its title, concept and concept explanation
// (GLOSSARY 探索文档).
export function ExplorationDocument({ d }: { d: ExplorationDetail }) {
  return (
    <div className="exp-doc">
      <blockquote className="exp-doc-quote">
        <span>{d.quote}</span>
        {d.lessonTitle ? <small>出自 {d.lessonTitle}</small> : null}
      </blockquote>
      <h2 className="exp-doc-title">{d.title}</h2>
      <p className="exp-doc-concept">{d.concept}</p>
      <hr />
      <div className="analysis-label">概念解释</div>
      <div className="analysis-explanation exp-doc-explanation"><LazyMarkdown text={d.explanation} components={documentParts} /></div>
    </div>
  );
}

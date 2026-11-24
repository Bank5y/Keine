import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Sparkles, Telescope, Trash2, X } from 'lucide-react';
import { toast } from 'sonner';
import { documentParts } from '@/components/analysis-popover';
import { Confirm } from '@/components/confirm';
import { ExplorationDialogue } from '@/components/exploration-dialogue';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { ApiError } from '@/lib/api';
import { useContainScroll, useScrollLock } from '@/lib/contain-scroll';
import { deleteExploration, getExploration, type ExplorationDetail } from '@/lib/explorations';
import { cn } from '@/lib/utils';

const MS = 320;
const EASE = 'cubic-bezier(.2,.8,.2,1)';

const reducedMotion = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

// the exploration's card in the rail, where the zoom flies from and back to
const railCard = (id: number) => document.querySelector<HTMLElement>(`.exp-card[data-exploration="${id}"]`);

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

// flyTo plays the box's move from where it is to rect `to`, then calls done.
function flyTo(el: HTMLElement, to: DOMRect, done: () => void) {
  const from = el.getBoundingClientRect();
  el.style.transformOrigin = 'top left';
  el.style.transition = `transform ${MS}ms ${EASE}, opacity ${MS}ms ${EASE}`;
  el.style.transform = `translate(${to.left - from.left}px, ${to.top - from.top}px) scale(${to.width / from.width}, ${to.height / from.height})`;
  el.style.opacity = '0.2';
  window.setTimeout(done, MS);
}

// ExplorationZoom opens one of the reader's explorations over the page
// (#134). Level 1 flies out of its rail card to the middle of the screen
// over a frosted page: the exploration document, read only, with 删除 and
// 展开探索. Level 2 grows to most of the screen with the page darkened
// further and holds the dialogue. Esc or a press on the backdrop closes:
// level 1 flies back to its card, level 2 goes straight back to the page.
// With reduced motion nothing moves; the levels just switch.
export function ExplorationZoom({ id, onClose, onDeleted, onNodes }: {
  id: number; onClose: () => void; onDeleted: (id: number) => void; onNodes?: (id: number, count: number) => void;
}) {
  const [d, setD] = useState<ExplorationDetail | null>(null);
  const [level, setLevel] = useState<1 | 2>(1);
  const [closing, setClosing] = useState(false);
  const [asking, setAsking] = useState(false); // the delete confirmation
  const card = useRef<HTMLDivElement>(null);
  const root = useRef<HTMLDivElement>(null);
  const opened = useRef(false);
  // the zoom is modal: the page stays still under it, the backdrop
  // included, and is where it was once it closes (#181)
  useScrollLock();
  useContainScroll(root);

  useEffect(() => {
    let alive = true;
    getExploration(id).then((x) => { if (alive) setD(x); }).catch((e) => {
      toast.error(e instanceof ApiError ? e.message : '探索打不开，请稍后再试');
      onClose();
    });
    return () => { alive = false; };
  }, [id]); // eslint-disable-line react-hooks/exhaustive-deps

  // fly out of the rail card once there is something to show
  useLayoutEffect(() => {
    if (!d || opened.current || !card.current) return;
    opened.current = true;
    const from = railCard(id)?.getBoundingClientRect();
    if (from && !reducedMotion()) flip(card.current, from);
  }, [d, id]);

  const close = () => {
    if (closing) return;
    const el = card.current;
    const to = railCard(id)?.getBoundingClientRect();
    if (!el || reducedMotion()) { onClose(); return; }
    setClosing(true);
    if (level === 1 && to) flyTo(el, to, onClose);
    else { el.style.transition = `opacity ${MS * 0.6}ms ${EASE}`; el.style.opacity = '0'; window.setTimeout(onClose, MS * 0.6); }
  };

  // switching level plays the card's change of size from where it was
  const toLevel = (to: 1 | 2) => {
    const el = card.current;
    const from = el?.getBoundingClientRect();
    setLevel(to);
    if (el && from && !reducedMotion()) requestAnimationFrame(() => flip(el, from));
  };
  const expand = () => toLevel(2);

  // Esc closes the zoom, unless the dialogue takes it (going up a node) or
  // a dialog above it is open
  const escTaken = useRef<() => boolean>(() => false);
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || asking || e.defaultPrevented) return;
      if (level === 2 && escTaken.current()) return;
      close();
    };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  });

  const remove = async () => {
    try {
      await deleteExploration(id);
    } catch (e) {
      if (!(e instanceof ApiError && e.status === 404)) { toast.error(e instanceof ApiError ? e.message : '删除失败，请稍后再试'); return; }
    }
    setAsking(false);
    onDeleted(id);
  };

  return createPortal(
    <div ref={root} className={cn('exp-zoom', `level-${level}`, closing && 'closing')} role="dialog" aria-modal="true" aria-label={d ? `探索：${d.title}` : '探索'}>
      <div className="exp-zoom-backdrop" onClick={close} />
      <div ref={card} className="exp-zoom-card">
        {!d ? <div className="exp-zoom-loading" /> : level === 1 ? (
          <>
            <header className="exp-zoom-head">
              <span className="exp-zoom-kind"><Sparkles aria-hidden />探索</span>
              <span className="grow" />
              <button type="button" className="exp-zoom-expand" onClick={expand}><Telescope aria-hidden />展开探索</button>
              <button type="button" className="exp-zoom-btn" onClick={() => setAsking(true)}><Trash2 aria-hidden />删除</button>
              <button type="button" className="exp-zoom-close" aria-label="关闭" onClick={close}><X /></button>
            </header>
            <ExplorationDocument d={d} />
          </>
        ) : (
          <ExplorationDialogue detail={d} onDetail={(x) => { setD(x); onNodes?.(x.id, x.nodes.length - 1); }}
            onClose={close} escTaken={escTaken} />
        )}
      </div>
      <Confirm open={asking} title="删除这个探索？" confirm="删除" onConfirm={remove} onClose={() => setAsking(false)}>
        探索文档和全部对话节点会一并删除，无法恢复。
      </Confirm>
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

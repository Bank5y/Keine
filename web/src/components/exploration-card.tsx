import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties } from 'react';
import { ChevronUp, Sparkles, Telescope, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import { Confirm } from '@/components/confirm';
import { ExplorationDocument, ExplorationZoom } from '@/components/exploration-zoom';
import { ApiError } from '@/lib/api';
import { useContainScroll } from '@/lib/contain-scroll';
import { deleteExploration, getExploration, type Exploration, type ExplorationDetail } from '@/lib/explorations';
import { placeOpenCard } from '@/lib/open-card';
import { cn, reducedMotion } from '@/lib/utils';

// the room kept free under the top bar and at the bottom of the screen
const MARGIN = 16;

// ExplorationCard is an exploration in the rail: its title on one line,
// its concept on two, cut off with an ellipsis when longer. Picked, it
// opens where it is (#180), over the cards below it: the exploration
// document, with 展开探索 (the dialogue over the page) and 删除; its title
// folds it again. anchor is where its text starts on the screen; busy
// onBusy tells the rail when a dialog of the card's is up, so a press or
// Esc there leaves the card open.
export function ExplorationCard({ e, active, open, anchor, onPick, onFold, onDeleted, onNodes, onBusy }: {
  e: Exploration; active: boolean; open: boolean; anchor: () => number | undefined;
  onPick: () => void; onFold: () => void; onDeleted: (id: number) => void; onNodes: (id: number, count: number) => void;
  onBusy: (on: boolean) => void;
}) {
  return (
    <>
      <div className={cn('exp-card', active && 'active')} data-exploration={e.id} role="button" tabIndex={0}
        aria-pressed={active} aria-expanded={open} aria-label={`探索：${e.title}`}
        onClick={onPick} onKeyDown={(ev) => { if (ev.key === 'Enter' && ev.target === ev.currentTarget) onPick(); }}>
        <div className="exp-card-title"><Sparkles aria-hidden /><span>{e.title}</span>{e.nodes > 0 ? <span className="exp-card-nodes" title="节点数（不含主节点）">节点 {e.nodes}</span> : null}</div>
        <p className="exp-card-concept">{e.concept}</p>
      </div>
      {open ? <OpenCard id={e.id} anchor={anchor} onFold={onFold} onDeleted={onDeleted} onNodes={onNodes} onBusy={onBusy} /> : null}
    </>
  );
}

function OpenCard({ id, anchor, onFold, onDeleted, onNodes, onBusy }: {
  id: number; anchor: () => number | undefined; onFold: () => void; onDeleted: (id: number) => void; onNodes: (id: number, count: number) => void;
  onBusy: (on: boolean) => void;
}) {
  const [d, setD] = useState<ExplorationDetail | null>(null);
  const [place, setPlace] = useState<{ offset: number; height: number; from: number } | null>(null);
  const [asking, setAsking] = useState(false); // the delete confirmation
  const [zoomed, setZoomed] = useState(false); // the dialogue over the page
  const box = useRef<HTMLDivElement>(null);
  const opened = useRef(false);
  // where the text starts, as the rail places it now
  const anchorRef = useRef(anchor);
  anchorRef.current = anchor;
  useContainScroll(box, !!place);

  // a dialog of the card's going away: the rail hears of it only after the
  // key or press that closed it has gone by; a card that goes away with a
  // dialog up lets the rail go at once
  const settling = useRef(0);
  const busy = (on: boolean) => {
    window.clearTimeout(settling.current);
    if (on) onBusy(true);
    else settling.current = window.setTimeout(() => onBusy(false), 0);
  };
  useEffect(() => () => { window.clearTimeout(settling.current); onBusy(false); }, []); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    let alive = true;
    getExploration(id).then((x) => { if (alive) setD(x); }).catch((ex: unknown) => {
      toast.error(ex instanceof ApiError ? ex.message : '探索打不开，请稍后再试');
      onFold();
    });
    return () => { alive = false; };
  }, [id]); // eslint-disable-line react-hooks/exhaustive-deps

  // place the card: level with its text, inside the screen; again as its
  // content grows (a diagram drawn) and the window resizes
  useLayoutEffect(() => {
    const el = box.current;
    if (!el || !d) return undefined;
    const measure = () => {
      const slot = el.parentElement!.getBoundingClientRect();
      const doc = el.querySelector<HTMLElement>('.exp-doc');
      const content = el.offsetHeight - (doc?.clientHeight ?? 0) + (doc?.scrollHeight ?? 0);
      const top = (document.querySelector('.masthead')?.getBoundingClientRect().bottom ?? 0) + MARGIN;
      const at = placeOpenCard({ anchor: anchorRef.current() ?? slot.top, content, viewport: innerHeight, top, margin: MARGIN });
      const card = el.previousElementSibling as HTMLElement | null;
      setPlace((p) => {
        const next = { offset: at.top - slot.top, height: at.height, from: card?.offsetHeight ?? 56 };
        return p && Math.abs(p.offset - next.offset) < 0.5 && Math.abs(p.height - next.height) < 0.5 ? p : next;
      });
    };
    measure();
    // the document's parts grow as they load (the Markdown, a diagram)
    const doc = el.querySelector('.exp-doc');
    const ro = new ResizeObserver(measure);
    const watch = () => { ro.disconnect(); for (const c of doc?.querySelectorAll('*') ?? []) ro.observe(c); };
    const mo = new MutationObserver(() => { watch(); measure(); });
    watch();
    if (doc) mo.observe(doc, { childList: true, subtree: true });
    window.addEventListener('resize', measure);
    return () => { ro.disconnect(); mo.disconnect(); window.removeEventListener('resize', measure); };
  }, [d]); // eslint-disable-line react-hooks/exhaustive-deps

  // the opening plays once, when the card first shows
  const opening = !!place && !opened.current && !reducedMotion();
  useEffect(() => { if (place) opened.current = true; }, [place]);

  const ask = (on: boolean) => { setAsking(on); busy(on); };
  const zoom = (on: boolean) => { setZoomed(on); busy(on); };

  const remove = async () => {
    try {
      await deleteExploration(id);
    } catch (ex) {
      if (!(ex instanceof ApiError && ex.status === 404)) { toast.error(ex instanceof ApiError ? ex.message : '删除失败，请稍后再试'); return; }
    }
    setAsking(false);
    onDeleted(id);
  };

  if (!d) return null;
  const style = (place
    ? { top: place.offset, height: place.height, '--from': `${place.from}px`, '--lift': `${-place.offset}px` }
    : { top: 0 }) as CSSProperties;
  return (
    <>
      <div ref={box} className={cn('exp-card-open', !place && 'measuring', opening && 'opening')} data-exploration={id} style={style}
        role="region" aria-label={`探索：${d.title}`}>
        <button type="button" className="exp-open-head" aria-expanded="true" onClick={onFold} title="收起">
          <Sparkles aria-hidden /><span>{d.title}</span><ChevronUp aria-hidden />
        </button>
        <ExplorationDocument d={d} />
        <div className="exp-open-foot">
          <button type="button" className="exp-open-expand" onClick={() => zoom(true)}><Telescope aria-hidden />展开探索</button>
          <span className="grow" />
          <button type="button" className="exp-open-btn" onClick={() => ask(true)}><Trash2 aria-hidden />删除</button>
        </div>
      </div>
      {zoomed ? <ExplorationZoom detail={d} onClose={() => zoom(false)} onDetail={(x) => { setD(x); onNodes(x.id, x.nodes.length - 1); }} /> : null}
      <Confirm open={asking} title="删除这个探索？" confirm="删除" onConfirm={remove} onClose={() => ask(false)}>
        探索文档和全部对话节点会一并删除，无法恢复。
      </Confirm>
    </>
  );
}

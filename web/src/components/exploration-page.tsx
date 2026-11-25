import { useEffect, useLayoutEffect, useMemo, useRef, useState, type MouseEvent, type PointerEvent } from 'react';
import { ArrowUp, ChevronLeft, Copy, RotateCcw, Square, Telescope, X } from 'lucide-react';
import { toast } from 'sonner';
import { documentParts } from '@/components/analysis-popover';
import { CitedMarkdown } from '@/components/cited-markdown';
import { Asked, Asking, CourseLinks, CreditLine, Dot, ForkHead, Refusal, useRuns, withTurn } from '@/components/exploration-dialogue';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { ApiError } from '@/lib/api';
import { citationTable } from '@/lib/citations';
import { askTurn, carried, forksAt, INTERRUPTED, pathTo, retryTurn } from '@/lib/dialogue';
import { swipeAxis, swipeStarts, type SheetEvent, type SwipeOn } from '@/lib/exploration-sheet';
import { getExploration, type DialogueNode, type ExplorationDetail, type Question, type Turn } from '@/lib/explorations';
import { navigate } from '@/lib/router';
import { useKeyboardInset } from '@/lib/use-keyboard-inset';
import { cn } from '@/lib/utils';

// ExplorationPage is an exploration over the whole of a narrow screen
// (#200): its dialogue, one node at a time. The top bar holds the current
// node's path, each step a way back to it, and ✕. The main node opens on
// the passage and the exploration document; each node reads its turns and
// takes the next question in the box at the foot. A swipe right pulls the
// node tree out from the left (tree), where a tap switches nodes. send
// hears what the reader does to the sheet (see exploration-sheet.ts);
// onDetail hears every change to the exploration.
export function ExplorationPage({ id, tree, send, onDetail }: {
  id: number; tree: boolean; send: (e: SheetEvent) => void; onDetail: (d: ExplorationDetail) => void;
}) {
  const onClose = () => send({ type: 'close' });
  const [d, setD] = useState<ExplorationDetail | null>(null);
  useEffect(() => {
    let alive = true;
    getExploration(id).then((x) => { if (alive) setD(x); }).catch((ex: unknown) => {
      if (!alive) return;
      toast.error(ex instanceof ApiError ? ex.message : '探索打不开，请稍后再试');
      onClose();
    });
    return () => { alive = false; };
  }, [id]); // eslint-disable-line react-hooks/exhaustive-deps
  const change = (x: ExplorationDetail) => { setD(x); onDetail(x); };

  // a link out to another lesson takes the page's place in the history,
  // so the back from there returns to the lesson, not to this page
  const onLink = (e: MouseEvent) => {
    const a = (e.target as Element).closest('a');
    const href = a?.getAttribute('href');
    if (!a || !href?.startsWith('/') || a.target || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    if (new URL(href, location.href).pathname === location.pathname) return;
    e.preventDefault();
    e.stopPropagation();
    navigate(href, { replace: true });
  };

  return (
    <div className="exp-page" onClickCapture={onLink}>
      {d ? <Dialogue d={d} tree={tree} send={send} onDetail={change} /> : (
        <>
          <header className="exp-page-head">
            <Telescope className="exp-page-mark" aria-hidden />
            <span className="exp-page-crumbs" />
            <button type="button" className="exp-page-icon exp-page-close" aria-label="关闭" onClick={onClose}><X /></button>
          </header>
          <p className="analysis-wait exp-page-loading">正在加载…</p>
        </>
      )}
    </div>
  );
}

// the node tree drawer's width: most of the screen, some of the page
// left showing beside it
const drawerWidth = () => Math.min(296, innerWidth - 56);

// swipeOn is what a press on target starts on, for the swipe that pulls
// out the node tree (see swipeStarts)
function swipeOn(target: EventTarget | null): SwipeOn {
  const t = target instanceof Element ? target : null;
  if (t?.closest('.exp-tree-row')) return 'row';
  if (t?.closest('pre, table, .exp-page-crumbs')) return 'sideways';
  if (t?.closest('.dlg-reply')) return 'reply';
  return 'page';
}

function Dialogue({ d, tree, send, onDetail }: { d: ExplorationDetail; tree: boolean; send: (e: SheetEvent) => void; onDetail: (d: ExplorationDetail) => void }) {
  const onClose = () => send({ type: 'close' });
  const main = d.nodes.find((n) => n.kind === 'main')!;
  const [current, setCurrent] = useState(main.id);
  const { pending, refused, setRefused, go } = useRuns();
  // the node shown may be gone (deleted with its parent)
  const node = d.nodes.find((n) => n.id === current) ?? main;
  const path = pathTo(d.nodes, node.id);
  const open = (to: number) => { setRefused(null); setCurrent(to); };

  // the drawer follows a finger swiping it: shown is how much of it shows
  const [drag, setDrag] = useState<number | null>(null);
  const press = useRef<{ id: number; x: number; y: number; axis: 'x' | 'y' | null } | null>(null);
  const swiped = useRef(false);
  const W = drawerWidth();
  const shown = drag ?? (tree ? W : 0);
  const onDown = (e: PointerEvent<HTMLDivElement>) => {
    swiped.current = false;
    const left = e.clientX - e.currentTarget.getBoundingClientRect().left;
    press.current = (e.pointerType !== 'mouse' || e.button === 0) && swipeStarts({ x: left, on: swipeOn(e.target), tree })
      ? { id: e.pointerId, x: e.clientX, y: e.clientY, axis: null } : null;
  };
  const onMove = (e: PointerEvent<HTMLDivElement>) => {
    const p = press.current;
    if (!p || p.id !== e.pointerId) return;
    const dx = e.clientX - p.x;
    if (!p.axis) {
      p.axis = swipeAxis(dx, e.clientY - p.y);
      if (p.axis !== 'x') { if (p.axis) press.current = null; return; }
      e.currentTarget.setPointerCapture(e.pointerId);
      getSelection()?.removeAllRanges();
    }
    setDrag(Math.max(0, Math.min(W, (tree ? W : 0) + dx)));
  };
  const onUp = (e: PointerEvent<HTMLDivElement>) => {
    const p = press.current;
    press.current = null;
    if (!p || p.id !== e.pointerId || p.axis !== 'x') return;
    swiped.current = true;
    setDrag(null);
    if (e.type === 'pointerup') send({ type: 'swipe', dx: e.clientX - p.x });
  };

  const ask = async (question: Question) => {
    const last = node.turns[node.turns.length - 1];
    const t = await go(String(node.id), question, (on, s) => askTurn(d.id, last.id, question, on, s));
    if (t) onDetail(withTurn(d, node.id, t as Turn));
  };
  const retry = async (t: Turn) => {
    const got = await go(String(node.id), t.question, (on, s) => retryTurn(d.id, t.id, on, s), t.id);
    if (got) onDetail(withTurn(d, node.id, got as Turn));
  };

  const scroller = useRef<HTMLDivElement>(null);
  // the path shows its end, the current node, however long it runs
  const crumbs = useRef<HTMLElement>(null);
  useLayoutEffect(() => { if (crumbs.current) crumbs.current.scrollLeft = crumbs.current.scrollWidth; }, [node.id]);
  // a node opens at its top; stay at the end as a reply streams in
  useLayoutEffect(() => { if (scroller.current) scroller.current.scrollTop = 0; }, [node.id]);
  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && pending) el.scrollTop = el.scrollHeight;
  }, [pending?.run.text, pending?.run.activities.length, node.turns.length]); // eslint-disable-line react-hooks/exhaustive-deps

  const mine = pending?.layer === String(node.id) ? pending : null;
  const stopped = refused?.layer === String(node.id) ? refused : null;
  const last = node.turns[node.turns.length - 1];
  const failed = last.meta.interrupted === 'failed';
  const hint = carried(node);
  return (
    <div className={cn('exp-page-dlg', drag != null && 'swiping')}
      onPointerDown={onDown} onPointerMove={onMove} onPointerUp={onUp} onPointerCancel={onUp}
      // the click that ends a swipe is not a tap on what it ended over
      onClickCapture={(e) => { if (swiped.current) { swiped.current = false; e.preventDefault(); e.stopPropagation(); } }}>
      <header className="exp-page-head">
        {path.length > 1
          ? <button type="button" className="exp-page-icon" aria-label="回到上一层" onClick={() => open(path[path.length - 2].id)}><ChevronLeft /></button>
          : <Telescope className="exp-page-mark" aria-hidden />}
        <nav className="exp-page-crumbs" aria-label="节点路径" ref={crumbs}>
          {path.map((n, i) => (
            <button type="button" key={n.id} className={cn('exp-page-crumb', `kind-${n.kind}`, i === path.length - 1 && 'current')}
              aria-current={i === path.length - 1 ? 'location' : undefined} onClick={() => open(n.id)}>
              {i ? <span className="exp-page-sep" aria-hidden>›</span> : null}
              <Dot kind={n.kind} /><span>{n.kind === 'main' ? d.title : n.name}</span>
            </button>
          ))}
        </nav>
        <button type="button" className="exp-page-icon exp-page-close" aria-label="关闭" onClick={onClose}><X /></button>
      </header>
      <div className="exp-page-scroll" ref={scroller}>
        {node.kind === 'main' ? <blockquote className="exp-page-quote">{d.quote}</blockquote> : <ForkHead d={d} node={node} />}
        {hint ? <p className="dlg-carried">{hint}</p> : null}
        {node.turns.map((t, i) => (
          mine?.retry === t.id ? <Asking key={t.id} question={mine.question} run={mine.run} kind={node.kind} /> : (
            <TurnView key={t.id} d={d} node={node} turn={t} first={node.kind === 'main' && i === 0} last={i === node.turns.length - 1}
              busy={!!pending} onRetry={retry} />
          )
        ))}
        {mine && mine.retry == null ? <Asking question={mine.question} run={mine.run} kind={node.kind} /> : null}
        {stopped?.message ? <Refusal refused={stopped} /> : null}
      </div>
      <Composer busy={!!pending}
        disabled={failed && !pending ? '重试这一轮后才能接着问' : stopped?.code === 'context_too_long' ? '这个节点已经太长，从更早的一轮开节点继续' : ''}
        onAsk={(text) => void ask({ text, attachments: [] })} />
      {/* the left edge hints at the drawer */}
      <span className="exp-page-edge" aria-hidden style={{ opacity: shown ? 0 : 1 }} />
      <div className={cn('exp-tree-scrim', drag == null && 'settled')} style={{ opacity: (shown / W) * 0.32, pointerEvents: tree ? 'auto' : 'none' }}
        onClick={() => send({ type: 'tapBeside' })} aria-hidden />
      <aside className={cn('exp-tree', drag == null && 'settled', shown > 0 && 'out')} style={{ width: W, transform: `translateX(${shown - W}px)` }}
        aria-label="节点树" aria-hidden={!tree} inert={!tree}>
        <div className="exp-tree-head"><b>节点树</b><span>{d.nodes.length} 个节点</span></div>
        <Tree nodes={d.nodes} parent={null} current={node.id} path={path.map((n) => n.id)}
          onPick={(n) => { open(n.id); send({ type: 'pickNode' }); }} />
      </aside>
    </div>
  );
}

// Tree is the nodes under parent (the main node, under none), each with
// how many turns it has and the nodes under it: the current node marked,
// the nodes on the path to it darker.
function Tree({ nodes, parent, current, path, onPick }: {
  nodes: DialogueNode[]; parent: number | null; current: number; path: number[]; onPick: (n: DialogueNode) => void;
}) {
  const kids = nodes.filter((n) => n.parentId === parent);
  if (!kids.length) return null;
  return (
    <ul className={cn('exp-tree-list', parent == null && 'root')}>
      {kids.map((n) => (
        <li key={n.id}>
          <button type="button" className={cn('exp-tree-row', n.id === current && 'current', path.includes(n.id) && 'on-path')}
            aria-current={n.id === current ? 'location' : undefined} onClick={() => onPick(n)}>
            <Dot kind={n.kind} /><span className="exp-tree-name">{n.kind === 'main' ? '主节点' : n.name}</span><small>{n.turns.length} 轮</small>
          </button>
          <Tree nodes={nodes} parent={n.id} current={current} path={path} onPick={onPick} />
        </li>
      ))}
    </ul>
  );
}

// TurnView is a question and its reply, with 复制 under the reply and, on
// a reply cut short, why and 重试 (none once the credits ran out). The
// main node's first turn is the question the analysis stands for and the
// exploration document.
function TurnView({ d, node, turn: t, first, last, busy, onRetry }: {
  d: ExplorationDetail; node: DialogueNode; turn: Turn; first: boolean; last: boolean; busy: boolean; onRetry: (t: Turn) => void;
}) {
  // what the reply may cite: the sources of its chain (ADR 0013)
  const table = useMemo(() => citationTable(d.nodes, t.id), [d.nodes, t.id]);
  const stop = t.meta.interrupted ? INTERRUPTED[t.meta.interrupted] : null;
  const retriable = !!t.meta.interrupted && t.meta.interrupted !== 'credits_exhausted' && last && !forksAt(d.nodes, t.id).length && !first;
  if (first) {
    return (
      <div className="dlg-turn" data-turn={t.id}>
        <Asked question={{ text: d.firstQuestion, attachments: [] }} kind={node.kind} />
        <div className="dlg-reply dlg-analysis">
          <h3 className="analysis-title">{d.title}</h3>
          <p className="analysis-concept">{d.concept}</p>
          <div className="analysis-label">概念解释</div>
          <div className="analysis-explanation"><LazyMarkdown text={d.explanation} components={documentParts} /></div>
        </div>
      </div>
    );
  }
  return (
    <div className="dlg-turn" data-turn={t.id}>
      <Asked question={t.question} kind={node.kind} />
      {t.reply ? (
        <div className="dlg-reply prose-wrap">
          <CitedMarkdown text={t.reply} table={table} components={documentParts} />
        </div>
      ) : null}
      <CourseLinks courseId={d.courseId} links={t.courseLinks} />
      {stop ? (
        <div className={cn('dlg-stop', t.meta.interrupted === 'failed' && 'failed')} role="status">
          <div><b>{stop.title}</b><span>{stop.body}</span></div>
          {retriable ? <button type="button" disabled={busy} onClick={() => onRetry(t)}><RotateCcw />重试</button> : null}
        </div>
      ) : null}
      {t.reply ? (
        <div className="exp-page-acts">
          <button type="button" aria-label="复制" title="复制" onClick={() => { void navigator.clipboard?.writeText(t.reply); toast.success('已复制'); }}><Copy /></button>
        </div>
      ) : null}
    </div>
  );
}

// Composer is the box at the foot to ask the next question in the current
// node, lifted over the on-screen keyboard, with the credit line.
function Composer({ busy, disabled, onAsk }: { busy: boolean; disabled: string; onAsk: (text: string) => void }) {
  const [text, setText] = useState('');
  const keyboard = useKeyboardInset(true);
  const send = () => {
    const q = text.trim();
    if (!q || busy || disabled) return;
    setText('');
    onAsk(q);
  };
  return (
    <div className="exp-page-foot" style={keyboard ? { paddingBottom: keyboard } : undefined}>
      <div className={cn('dlg-box exp-page-box', (disabled || busy) && 'off')}>
        <textarea value={text} rows={1} placeholder={disabled || '接着问…'} disabled={!!disabled} aria-label="接着问"
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); send(); } }} />
        <button type="button" className={cn('dlg-send', busy && 'stop')} aria-label={busy ? '回答中' : '发送'} disabled={busy || !!disabled || !text.trim()} onClick={send}>
          {busy ? <Square /> : <ArrowUp />}
        </button>
      </div>
      <CreditLine />
    </div>
  );
}

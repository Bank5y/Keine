import { useEffect, useLayoutEffect, useMemo, useRef, useState, type MouseEvent, type PointerEvent, type ReactNode, type RefObject } from 'react';
import { ArrowUp, ChevronLeft, Copy, FileText, GitBranch, Plus, RotateCcw, Square, Telescope, TextQuote, Trash2, X } from 'lucide-react';
import { toast } from 'sonner';
import { documentParts } from '@/components/analysis-popover';
import { CitedMarkdown } from '@/components/cited-markdown';
import { Confirm } from '@/components/confirm';
import { Asking, CourseLinks, CreditLine, deleteText, Dot, ForkHead, Refusal, useRuns, withTurn } from '@/components/exploration-dialogue';
import { ReportView } from '@/components/exploration-report';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { api, ApiError } from '@/lib/api';
import { citationTable } from '@/lib/citations';
import {
  askTurn, carried, forksAt, FULL_TEXT, INTERRUPTED, KINDS, MAX_NODES, nextQuestion, noCarry, openNode, pathTo, retryTurn, stepCarry,
  type Carry, type CarryEvent,
} from '@/lib/dialogue';
import { swipeAxis, swipeStarts, type SheetEvent, type SwipeOn } from '@/lib/exploration-sheet';
import { deleteExploration, getExploration, type DialogueNode, type ExplorationDetail, type NodeKind, type Question, type Turn } from '@/lib/explorations';
import { generateReport, refusalTitle } from '@/lib/reports';
import { navigate } from '@/lib/router';
import { useKeyboardInset } from '@/lib/use-keyboard-inset';
import { supported } from '@/components/annotator';
import { cn } from '@/lib/utils';

// ExplorationPage is an exploration over the whole of a narrow screen
// (#200): its dialogue, one node at a time. The top bar holds the current
// node's path, each step a way back to it, and ✕. The main node opens on
// the passage and the exploration document; each node reads its turns and
// takes the next question in the box at the foot. A swipe right pulls the
// node tree out from the left (tree), where a tap switches nodes. The top
// bar also writes a new version of the exploration report, read on a page
// pushed in from the right (report), and deletes the exploration. send
// hears what the reader does to the sheet (see exploration-sheet.ts);
// onDetail hears every change to the exploration, onDeleted that it went.
export function ExplorationPage({ id, tree, report, send, onDetail, onDeleted }: {
  id: number; tree: boolean; report: boolean; send: (e: SheetEvent) => void; onDetail: (d: ExplorationDetail) => void; onDeleted: (id: number) => void;
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
      {d ? <Dialogue d={d} tree={tree} send={send} onDetail={change} onDeleted={() => onDeleted(d.id)} /> : (
        <>
          <header className="exp-page-head">
            <Telescope className="exp-page-mark" aria-hidden />
            <span className="exp-page-crumbs" />
            <button type="button" className="exp-page-icon exp-page-close" aria-label="关闭" onClick={onClose}><X /></button>
          </header>
          <p className="analysis-wait exp-page-loading">正在加载…</p>
        </>
      )}
      {d && report ? (
        <div className="exp-page-report">
          <ReportView exp={d.id} onBack={onClose} />
        </div>
      ) : null}
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
  if (t?.closest('.exp-tree-row.deletable')) return 'row';
  if (t?.closest('pre, table, .exp-page-crumbs')) return 'sideways';
  if (t?.closest('.dlg-reply')) return 'reply';
  return 'page';
}

// useDrawerSwipe is the swipe that pulls the node tree out and puts it
// away (see exploration-sheet.ts): handlers for the page, how much of the
// drawer shows (following the finger), and whether a swipe is under way.
function useDrawerSwipe(tree: boolean, send: (e: SheetEvent) => void, W: number) {
  const [drag, setDrag] = useState<number | null>(null);
  const press = useRef<{ id: number; x: number; y: number; axis: 'x' | 'y' | null } | null>(null);
  const swiped = useRef(false);
  const handlers = {
    onPointerDown: (e: PointerEvent<HTMLDivElement>) => {
      swiped.current = false;
      const left = e.clientX - e.currentTarget.getBoundingClientRect().left;
      press.current = (e.pointerType !== 'mouse' || e.button === 0) && swipeStarts({ x: left, on: swipeOn(e.target), tree })
        ? { id: e.pointerId, x: e.clientX, y: e.clientY, axis: null } : null;
    },
    onPointerMove: (e: PointerEvent<HTMLDivElement>) => {
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
    },
    onPointerUp: (e: PointerEvent<HTMLDivElement>) => {
      const p = press.current;
      press.current = null;
      if (!p || p.id !== e.pointerId || p.axis !== 'x') return;
      swiped.current = true;
      setDrag(null);
      if (e.type === 'pointerup') send({ type: 'swipe', dx: e.clientX - p.x });
    },
    // the click that ends a swipe is not a tap on what it ended over
    onClickCapture: (e: MouseEvent) => { if (swiped.current) { swiped.current = false; e.preventDefault(); e.stopPropagation(); } },
  };
  return { handlers: { ...handlers, onPointerCancel: handlers.onPointerUp }, shown: drag ?? (tree ? W : 0), swiping: drag != null };
}

// a selection painted grey in its reply while the box carries it
const CARRIED = 'exp-carried';

// a report being written from the top bar: under way, written (the
// version kept), or stopped (why)
type Writing = { status: 'idle' } | { status: 'running' } | { status: 'done'; version: number } | { status: 'refused'; code: string; message: string };

function Dialogue({ d, tree, send, onDetail, onDeleted }: {
  d: ExplorationDetail; tree: boolean; send: (e: SheetEvent) => void; onDetail: (d: ExplorationDetail) => void; onDeleted: () => void;
}) {
  const onClose = () => send({ type: 'close' });
  const main = d.nodes.find((n) => n.kind === 'main')!;
  const [current, setCurrent] = useState(main.id);
  const { pending, refused, setRefused, go } = useRuns();
  // the node shown may be gone (deleted with its parent)
  const node = d.nodes.find((n) => n.id === current) ?? main;
  const path = pathTo(d.nodes, node.id);
  // what the next question carries, the words in the box, and whether the
  // kinds of node are offered over it
  const [carry, setCarry] = useState<Carry>(noCarry);
  const [text, setText] = useState('');
  const [menu, setMenu] = useState(false);
  const [carriedRange, setCarriedRange] = useState<Range | null>(null);
  const carryDo = (e: CarryEvent) => {
    setCarry((c) => stepCarry(c, e));
    if (e.type === 'unselect' || e.type === 'sent') setCarriedRange(null);
  };
  const open = (to: number) => { setRefused(null); setCurrent(to); carryDo({ type: 'sent' }); setMenu(false); };

  const W = drawerWidth();
  const { handlers, shown, swiping } = useDrawerSwipe(tree, send, W);

  // 生成探索报告 writes a new version; 查看 pushes its page in
  const [writing, setWriting] = useState<Writing>({ status: 'idle' });
  const reportRun = useRef<AbortController | null>(null);
  useEffect(() => () => reportRun.current?.abort(), []);
  const writeReport = async () => {
    reportRun.current?.abort();
    const ac = new AbortController();
    reportRun.current = ac;
    setWriting({ status: 'running' });
    const end = await generateReport(d.id, () => {}, ac.signal);
    if (ac.signal.aborted) return;
    setWriting(end.ok ? { status: 'done', version: end.report.version } : { status: 'refused', code: end.code, message: end.message });
  };

  // 删除探索, asked first
  const [dropping, setDropping] = useState(false);
  const dropExploration = async () => {
    try {
      await deleteExploration(d.id);
    } catch (ex) {
      if (!(ex instanceof ApiError && ex.status === 404)) { toast.error(ex instanceof ApiError ? ex.message : '删除失败，请稍后再试'); return; }
    }
    setDropping(false);
    send({ type: 'deleted' });
    onDeleted();
  };

  // a node being deleted, asked first; the node and those under it go,
  // and the reader on its path goes back to its parent
  const [deleting, setDeleting] = useState<DialogueNode | null>(null);
  const removeNode = async () => {
    if (!deleting) return;
    try {
      await api('DELETE', `/api/explorations/${d.id}/nodes/${deleting.id}`);
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '删除失败，请稍后再试');
      return;
    }
    const gone = new Set([deleting.id]);
    for (let grew = true; grew;) {
      grew = false;
      for (const n of d.nodes) if (n.parentId != null && gone.has(n.parentId) && !gone.has(n.id)) { gone.add(n.id); grew = true; }
    }
    if (deleting.parentId != null && path.some((n) => gone.has(n.id))) open(deleting.parentId);
    onDetail({ ...d, nodes: d.nodes.filter((n) => !gone.has(n.id)) });
    setDeleting(null);
  };

  // the selection carried shows grey in its reply
  useEffect(() => {
    if (!supported() || !carriedRange) return undefined;
    CSS.highlights.set(CARRIED, new Highlight(carriedRange));
    return () => { CSS.highlights.delete(CARRIED); };
  }, [carriedRange]);

  const retry = async (t: Turn) => {
    const got = await go(String(node.id), t.question, (on, s) => retryTurn(d.id, t.id, on, s), t.id);
    if (got) onDetail(withTurn(d, node.id, got as Turn));
  };

  const mine = pending?.layer === String(node.id) ? pending : null;
  const stopped = refused?.layer === String(node.id) ? refused : null;
  const full = d.nodes.length >= MAX_NODES;
  const next = nextQuestion(node, d.nodes, carry, text);
  const tooLong = next.to === 'turn' && stopped?.code === 'context_too_long' ? '这个节点已经太长，从更早的一轮开节点继续' : '';
  const blocked = next.to === 'none' ? next.reason : tooLong;
  // the box sends: on in this node, or a new node, opened only now its
  // first question goes, and gone to once it is kept
  const submit = async () => {
    if (pending || next.to === 'none' || tooLong) return;
    const at = node.id;
    setText('');
    carryDo({ type: 'sent' });
    setMenu(false);
    if (next.to === 'turn') {
      const t = await go(String(at), next.question, (on, s) => askTurn(d.id, next.after, next.question, on, s));
      if (t) onDetail(withTurn(d, at, t as Turn));
      return;
    }
    const n = await go(String(at), next.question, (on, s) => openNode(d.id, next.fork, next.kind, next.question, on, s));
    if (!n) return;
    onDetail({ ...d, nodes: [...d.nodes, n as DialogueNode] });
    setCurrent((n as DialogueNode).id);
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

  // the turn a new node would fork from, marked in the dialogue
  const forkAt = carry.fork ?? (carry.kind ? carry.selection?.turn ?? null : null);
  const turnNo = (id: number) => node.turns.findIndex((t) => t.id === id) + 1;
  const hint = carried(node);
  return (
    <div className={cn('exp-page-dlg', swiping && 'swiping')} {...handlers}>
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
        <button type="button" className={cn('exp-page-icon', writing.status === 'running' && 'busy')} aria-label="生成探索报告"
          title={pending ? '这一轮回答完以后才能生成' : '生成探索报告'} disabled={!!pending || writing.status === 'running'} onClick={() => void writeReport()}><FileText /></button>
        <button type="button" className="exp-page-icon" aria-label="删除探索" onClick={() => setDropping(true)}><Trash2 /></button>
        {/* the most used button kept clear of the dangerous one */}
        <button type="button" className="exp-page-icon exp-page-close" aria-label="关闭" onClick={onClose}><X /></button>
      </header>
      {writing.status === 'running' ? <div className="exp-page-banner" role="status"><span className="exp-page-pulse">探索报告生成中…</span></div> : null}
      {writing.status === 'done' ? (
        <div className="exp-page-banner" role="status">
          <span>已生成 · 第 {writing.version} 版</span>
          <button type="button" onClick={() => send({ type: 'openReport' })}>查看</button>
          <button type="button" className="exp-page-banner-x" aria-label="收起" onClick={() => setWriting({ status: 'idle' })}><X /></button>
        </div>
      ) : null}
      {writing.status === 'refused' ? (
        <div className="exp-page-banner refused" role="alert">
          <span><b>{refusalTitle(writing.code)}</b>{writing.message}</span>
          <button type="button" className="exp-page-banner-x" aria-label="收起" onClick={() => setWriting({ status: 'idle' })}><X /></button>
        </div>
      ) : null}
      <div className="exp-page-scroll" ref={scroller}>
        {node.kind === 'main' ? <blockquote className="exp-page-quote">{d.quote}</blockquote> : <ForkHead d={d} node={node} />}
        {hint ? <p className="dlg-carried">{hint}</p> : null}
        {node.turns.map((t, i) => (
          mine?.retry === t.id ? <Asking key={t.id} question={mine.question} run={mine.run} kind={node.kind} asked={<Asked question={mine.question} />} /> : (
            <TurnView key={t.id} d={d} turn={t} first={node.kind === 'main' && i === 0} last={i === node.turns.length - 1}
              busy={!!pending} full={full} forking={forkAt === t.id} selected={carry.selection?.turn === t.id}
              onRetry={retry} onOpen={open} onFork={() => { carryDo({ type: 'fork', turn: t.id }); setMenu(true); }} />
          )
        ))}
        {mine && mine.retry == null ? <Asking question={mine.question} run={mine.run} kind={node.kind} asked={<Asked question={mine.question} />} /> : null}
        {stopped?.message ? <Refusal refused={stopped} /> : null}
        <TakeSelection scroller={scroller} onTake={(turn, sel, range) => { carryDo({ type: 'select', selection: { turn, text: sel } }); setCarriedRange(range); }} />
      </div>
      <Composer text={text} onText={setText} busy={!!pending} blocked={blocked} ready={next.to !== 'none' && !tooLong}
        placeholder={carry.kind ? `问点什么，开一个新的${KINDS[carry.kind].label}…` : carry.selection ? '就这段接着问…' : '接着问…'}
        menu={menu} onMenu={setMenu} full={full}
        menuHead={`开一个新节点 · ${forkAt != null && forkAt !== node.turns[node.turns.length - 1].id ? `从第 ${turnNo(forkAt)} 轮岔出` : '从最后一轮岔出'}`}
        tags={(
          <>
            {carry.kind ? <Tag className={`kind-${carry.kind}`} label={`去掉${KINDS[carry.kind].label}`} onX={() => carryDo({ type: 'unkind' })}><Dot kind={carry.kind} />{KINDS[carry.kind].label}</Tag> : null}
            {carry.fork != null ? <Tag className="fork" label="去掉岔出点" onX={() => carryDo({ type: 'unfork' })}><GitBranch />第 {turnNo(carry.fork)} 轮</Tag> : null}
            {carry.selection ? (
              <Tag className="selection" label="去掉选段" onX={() => carryDo({ type: 'unselect' })}>
                <TextQuote />{carry.selection.text.length > 10 ? `${carry.selection.text.slice(0, 10)}…` : carry.selection.text}
              </Tag>
            ) : null}
          </>
        )}
        onKind={(k) => { carryDo({ type: 'kind', kind: k }); setMenu(false); }} kind={carry.kind} onSend={() => void submit()} />
      {/* the left edge hints at the drawer */}
      <span className="exp-page-edge" aria-hidden style={{ opacity: shown ? 0 : 1 }} />
      <div className={cn('exp-tree-scrim', !swiping && 'settled')} style={{ opacity: (shown / W) * 0.32, pointerEvents: tree ? 'auto' : 'none' }}
        onClick={() => send({ type: 'tapBeside' })} aria-hidden />
      <aside className={cn('exp-tree', !swiping && 'settled', shown > 0 && 'out')} style={{ width: W, transform: `translateX(${shown - W}px)` }}
        aria-label="节点树" aria-hidden={!tree} inert={!tree}>
        <div className="exp-tree-head"><b>节点树</b><span>{d.nodes.length} 个节点</span></div>
        <Tree nodes={d.nodes} parent={null} current={node.id} path={path.map((n) => n.id)}
          onPick={(n) => { open(n.id); send({ type: 'pickNode' }); }} onDelete={setDeleting} />
        <p className="exp-tree-hint">左滑节点可以删除</p>
      </aside>
      <Confirm open={dropping} title="删除这个探索？" confirm="删除" onConfirm={dropExploration} onClose={() => setDropping(false)}>
        探索文档、整个探索对话和探索报告都会删除，无法恢复。
      </Confirm>
      <Confirm open={!!deleting} title={deleting ? `删除节点「${deleting.name}」？` : ''} confirm="删除" onConfirm={removeNode} onClose={() => setDeleting(null)}>
        {deleting ? deleteText(d.nodes, deleting) : null}
      </Confirm>
    </div>
  );
}

// Tree is the nodes under parent (the main node, under none), each with
// how many turns it has and the nodes under it: the current node marked,
// the nodes on the path to it darker. A row swiped left shows 删除 (none
// on the main node's), one row at a time.
function Tree({ nodes, parent, current, path, onPick, onDelete, swiped: shared }: {
  nodes: DialogueNode[]; parent: number | null; current: number; path: number[]; onPick: (n: DialogueNode) => void; onDelete: (n: DialogueNode) => void;
  swiped?: [number | null, (id: number | null) => void];
}) {
  const own = useState<number | null>(null);
  const [swiped, setSwiped] = shared ?? own;
  const kids = nodes.filter((n) => n.parentId === parent);
  if (!kids.length) return null;
  return (
    <ul className={cn('exp-tree-list', parent == null && 'root')}>
      {kids.map((n) => (
        <li key={n.id}>
          <TreeRow n={n} current={n.id === current} onPath={path.includes(n.id)} open={swiped === n.id}
            onOpen={(on) => setSwiped(on ? n.id : null)} onPick={() => onPick(n)} onDelete={() => { setSwiped(null); onDelete(n); }} />
          <Tree nodes={nodes} parent={n.id} current={current} path={path} onPick={onPick} onDelete={onDelete} swiped={[swiped, setSwiped]} />
        </li>
      ))}
    </ul>
  );
}

// how far a row slides left to show 删除
const ROW_REVEAL = 72;

// TreeRow is a node in the tree. Swiped left (any node but the main one)
// it slides over to show 删除, settling open past half way; a tap on a row
// slid open closes it rather than picking it.
function TreeRow({ n, current, onPath, open, onOpen, onPick, onDelete }: {
  n: DialogueNode; current: boolean; onPath: boolean; open: boolean; onOpen: (on: boolean) => void; onPick: () => void; onDelete: () => void;
}) {
  const can = n.kind !== 'main';
  const [drag, setDrag] = useState<number | null>(null);
  const press = useRef<{ id: number; x: number; y: number; axis: 'x' | 'y' | null } | null>(null);
  const moved = useRef(false);
  const at = drag ?? (open ? -ROW_REVEAL : 0);
  return (
    <div className={cn('exp-tree-slot', drag != null && 'dragging', open && 'open')}
      onPointerDown={(e) => { moved.current = false; press.current = can ? { id: e.pointerId, x: e.clientX, y: e.clientY, axis: null } : null; }}
      onPointerMove={(e) => {
        const p = press.current;
        if (!p || p.id !== e.pointerId) return;
        const dx = e.clientX - p.x;
        if (!p.axis) {
          p.axis = swipeAxis(dx, e.clientY - p.y);
          if (p.axis !== 'x') { if (p.axis) press.current = null; return; }
          e.currentTarget.setPointerCapture(e.pointerId);
        }
        setDrag(Math.max(-ROW_REVEAL, Math.min(0, (open ? -ROW_REVEAL : 0) + dx)));
      }}
      onPointerUp={(e) => {
        const p = press.current;
        press.current = null;
        if (!p || p.id !== e.pointerId || p.axis !== 'x') return;
        moved.current = true;
        onOpen((drag ?? 0) < -ROW_REVEAL / 2);
        setDrag(null);
      }}
      onPointerCancel={() => { press.current = null; setDrag(null); }}>
      {can ? <button type="button" className="exp-tree-delete" style={{ width: ROW_REVEAL }} tabIndex={open ? 0 : -1} onClick={onDelete}><Trash2 />删除</button> : null}
      <button type="button" className={cn('exp-tree-row', can && 'deletable', current && 'current', onPath && 'on-path')} style={{ transform: at ? `translateX(${at}px)` : undefined }}
        aria-current={current ? 'location' : undefined}
        onClick={() => { if (moved.current) { moved.current = false; return; } if (open) { onOpen(false); return; } onPick(); }}>
        <Dot kind={n.kind} /><span className="exp-tree-name">{n.kind === 'main' ? '主节点' : n.name}</span><small>{n.turns.length} 轮</small>
      </button>
    </div>
  );
}

// Asked is a question as asked, the selection it carried (if any) above
// it in grey.
function Asked({ question: q }: { question: Question }) {
  return (
    <div className="exp-page-asked">
      {q.attachments.map((a, i) => <div key={i} className="exp-page-asked-sel"><TextQuote aria-hidden /><span>{a.text}</span></div>)}
      {q.text ? <div className="dlg-asked-bubble">{q.text}</div> : null}
    </div>
  );
}

// TurnView is a question and its reply. Under the reply: 复制, and the
// icon to fork a node from the turn (greyed out once the dialogue is
// full); on a reply cut short, why and 重试 (none once the credits ran
// out); a chip for each node forked from it. The main node's first turn is
// the question the analysis stands for and the exploration document.
// forking: the turn is marked as where a new node forks from.
function TurnView({ d, turn: t, first, last, busy, full, forking, selected, onRetry, onOpen, onFork }: {
  d: ExplorationDetail; turn: Turn; first: boolean; last: boolean; busy: boolean; full: boolean;
  forking: boolean; selected: boolean; onRetry: (t: Turn) => void; onOpen: (id: number) => void; onFork: () => void;
}) {
  // what the reply may cite: the sources of its chain (ADR 0013)
  const table = useMemo(() => citationTable(d.nodes, t.id), [d.nodes, t.id]);
  const stop = t.meta.interrupted ? INTERRUPTED[t.meta.interrupted] : null;
  const forks = forksAt(d.nodes, t.id);
  const retriable = !!t.meta.interrupted && t.meta.interrupted !== 'credits_exhausted' && last && !forks.length && !first;
  const replied = first || !!t.reply;
  return (
    <div className={cn('dlg-turn exp-page-turn', forking && 'forking')} data-turn={t.id}>
      <Asked question={first ? { text: d.firstQuestion, attachments: [] } : t.question} />
      {forking ? <div className="exp-page-here"><GitBranch aria-hidden />从此处岔出{selected ? ' · 已带上一段' : ''}</div> : null}
      {first ? (
        <div className="dlg-reply dlg-analysis">
          <h3 className="analysis-title">{d.title}</h3>
          <p className="analysis-concept">{d.concept}</p>
          <div className="analysis-label">概念解释</div>
          <div className="analysis-explanation"><LazyMarkdown text={d.explanation} components={documentParts} /></div>
        </div>
      ) : t.reply ? (
        <div className="dlg-reply prose-wrap">
          <CitedMarkdown text={t.reply} table={table} components={documentParts} />
        </div>
      ) : null}
      {first ? null : <CourseLinks courseId={d.courseId} links={t.courseLinks} />}
      {stop ? (
        <div className={cn('dlg-stop', t.meta.interrupted === 'failed' && 'failed')} role="status">
          <div><b>{stop.title}</b><span>{stop.body}</span></div>
          {retriable ? <button type="button" disabled={busy} onClick={() => onRetry(t)}><RotateCcw />重试</button> : null}
        </div>
      ) : null}
      {replied ? (
        <div className="exp-page-acts">
          {first ? null : <button type="button" aria-label="复制" title="复制" onClick={() => { void navigator.clipboard?.writeText(t.reply); toast.success('已复制'); }}><Copy /></button>}
          <button type="button" className={cn(forking && 'on')} aria-label="从这一轮岔出" title={full ? FULL_TEXT : '从这一轮岔出'} disabled={full || busy} onClick={onFork}><GitBranch /></button>
        </div>
      ) : null}
      {forks.length ? (
        <div className="exp-page-forks">
          {forks.map((n) => (
            <button type="button" key={n.id} className={cn('dlg-chip', `kind-${n.kind}`)} onClick={() => onOpen(n.id)} title={`${KINDS[n.kind].label}：${n.name}`}>
              <Dot kind={n.kind} /><span>{n.name}</span><small>{n.turns.length} 轮</small>
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

// TakeSelection offers ＋ 带上 under a selection made in a reply of the
// node (a long press, on a phone), to carry it with the next question.
function TakeSelection({ scroller, onTake }: { scroller: RefObject<HTMLDivElement | null>; onTake: (turn: number, text: string, range: Range) => void }) {
  const [pick, setPick] = useState<{ turn: number; text: string; range: Range; top: number; left: number } | null>(null);
  useEffect(() => {
    const on = () => {
      const box = scroller.current;
      const sel = getSelection();
      const text = sel?.toString().trim() ?? '';
      const at = sel && sel.rangeCount ? sel.getRangeAt(0) : null;
      const reply = at ? (at.commonAncestorContainer instanceof Element ? at.commonAncestorContainer : at.commonAncestorContainer.parentElement)?.closest('.dlg-reply') : null;
      const turn = reply?.closest<HTMLElement>('[data-turn]');
      if (!box || !text || !at || !turn || !box.contains(turn)) { setPick(null); return; }
      const r = at.getBoundingClientRect();
      const b = box.getBoundingClientRect();
      setPick({ turn: Number(turn.dataset.turn), text: text.slice(0, 2000), range: at.cloneRange(),
        top: r.bottom - b.top + box.scrollTop + 8, left: Math.min(Math.max(r.left - b.left + r.width / 2 - 44, 8), b.width - 96) });
    };
    document.addEventListener('selectionchange', on);
    return () => document.removeEventListener('selectionchange', on);
  }, [scroller]);
  if (!pick) return null;
  return (
    <button type="button" className="exp-page-take" style={{ top: pick.top, left: pick.left }}
      // the press keeps the selection until the tap lands
      onPointerDown={(e) => e.preventDefault()} onMouseDown={(e) => e.preventDefault()}
      onClick={() => { onTake(pick.turn, pick.text, pick.range); getSelection()?.removeAllRanges(); setPick(null); }}>
      <Plus />带上
    </button>
  );
}

function Tag({ className, label, onX, children }: { className: string; label: string; onX: () => void; children: ReactNode }) {
  return <span className={cn('exp-page-tag', className)}>{children}<button type="button" aria-label={label} onClick={onX}><X /></button></span>;
}

const NEW_KINDS = ['child', 'branch', 'divergent'] as const;

// Composer is the box at the foot, lifted over the on-screen keyboard,
// with the credit line: ＋ on its left offers the kinds of node (menu),
// the tags show what the question carries, and blocked says why it cannot
// be sent, in place of the placeholder.
function Composer({ text, onText, busy, blocked, ready, placeholder, menu, onMenu, menuHead, full, kind, onKind, tags, onSend }: {
  text: string; onText: (t: string) => void; busy: boolean; blocked: string; ready: boolean; placeholder: string;
  menu: boolean; onMenu: (on: boolean) => void; menuHead: string; full: boolean; kind: Exclude<NodeKind, 'main'> | null;
  onKind: (k: Exclude<NodeKind, 'main'>) => void; tags: ReactNode; onSend: () => void;
}) {
  const keyboard = useKeyboardInset(true);
  const foot = useRef<HTMLDivElement>(null);
  // the kinds go on a tap anywhere else
  useEffect(() => {
    if (!menu) return undefined;
    const away = (e: Event) => { if (!foot.current?.contains(e.target as Node)) onMenu(false); };
    document.addEventListener('pointerdown', away);
    return () => document.removeEventListener('pointerdown', away);
  }, [menu, onMenu]);
  const send = () => { if (ready && !busy) onSend(); };
  return (
    <div className="exp-page-foot" ref={foot} style={keyboard ? { paddingBottom: keyboard } : undefined}>
      {menu ? (
        <div className="exp-page-menu" role="menu" aria-label="节点类型">
          <div className="exp-page-menu-head">{menuHead}</div>
          {NEW_KINDS.map((k) => (
            <button type="button" key={k} role="menuitem" disabled={full} className={cn(kind === k && 'on')} onClick={() => onKind(k)}>
              <Dot kind={k} /><span><b>{KINDS[k].label}</b><small>{KINDS[k].hint}</small></span>
            </button>
          ))}
          {full ? <div className="exp-page-menu-full">{FULL_TEXT}</div> : null}
        </div>
      ) : null}
      <div className={cn('dlg-box exp-page-box', busy && 'off')}>
        <button type="button" className={cn('exp-page-plus', menu && 'on')} aria-label="开节点" aria-expanded={menu} onClick={() => onMenu(!menu)}><Plus /></button>
        <div className="exp-page-field">
          {tags}
          <textarea value={text} rows={1} placeholder={blocked || placeholder} aria-label="接着问"
            onChange={(e) => onText(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); send(); } }} />
        </div>
        <button type="button" className={cn('dlg-send', busy && 'stop')} aria-label={busy ? '回答中' : '发送'} disabled={busy || !ready} onClick={send}>
          {busy ? <Square /> : <ArrowUp />}
        </button>
      </div>
      <CreditLine />
    </div>
  );
}

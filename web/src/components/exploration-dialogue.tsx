import { Fragment, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type MutableRefObject, type ReactNode, type RefObject } from 'react';
import { BookOpen, ChevronLeft, Copy, FileText, GitBranch, Menu, Plus, RotateCcw, Square, Telescope, TextQuote, Trash2, X, ArrowUp } from 'lucide-react';
import { toast } from 'sonner';
import { Credit, documentParts, Lookups, Notice, Ticker } from '@/components/analysis-popover';
import { Confirm } from '@/components/confirm';
import { ReportView } from '@/components/exploration-report';
import { CitedMarkdown } from '@/components/cited-markdown';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { QuotedWords, QuoteField, type FieldHandle, type TurnNo } from '@/components/quote-field';
import { SelectionBar } from '@/components/selection-bar';
import { ApiError } from '@/lib/api';
import { citationTable, type Cited } from '@/lib/citations';
import {
  askTurn, atEnd, carried, courseLinkItems, forksAt, FULL_TEXT, INTERRUPTED, KINDS, mapLayout, MAX_NODES, nextQuestion, noCarry, openNode, pathTo, prune, restack, retryTurn,
  OPENING, openingName, REPORT, rekey, seenBy, settle, spine, stepCarry, stepRun, turnAt, turnNoIn, withoutNode, NEW_KINDS, type Carry, type CarryEvent, type Ended, type Layer, type NewKind, type Next, type Opening, type Run,
} from '@/lib/dialogue';
import { deleteNode, type DialogueNode, type ExplorationDetail, type NodeKind, type Question, type Turn } from '@/lib/explorations';
import { readQuestion } from '@/lib/quotes';
import { Link } from '@/lib/router';
import { useUser } from '@/lib/session';
import { cn } from '@/lib/utils';

const SPINE = 56; // the width of each covered layer left showing
const SHOWN = 3; // how many covered layers keep a spine
const MS = 340;

// a turn being answered: on which node, after which turn (or as a node's
// first, or a retry in place), and what was asked
export type Pending = { layer: string; question: Question; run: Run; retry?: number };
// what stopped a question before it ran, shown where it was asked
export type Refused = { layer: string; code: string; message: string };

// useRuns runs the dialogue one turn at a time: go streams a run into
// pending, kept into the exploration however it ends, and returns what was
// kept, or null when nothing was (refused says why). A new run stops the
// one before; leaving stops it too. layer names where the run shows.
export function useRuns() {
  const [pending, setPending] = useState<Pending | null>(null);
  const [refused, setRefused] = useState<Refused | null>(null);
  const abort = useRef<AbortController | null>(null);
  useEffect(() => () => abort.current?.abort(), []);
  const go = async (layer: string, question: Question, start: (on: (e: any) => void, signal: AbortSignal) => Promise<Ended>, retry?: number) => {
    abort.current?.abort();
    const ac = new AbortController();
    abort.current = ac;
    setRefused(null);
    setPending({ layer, question, run: { text: '', activities: [], thinking: false }, retry });
    const end = await start((e) => setPending((p) => (p ? { ...p, run: stepRun(p.run, e) } : p)), ac.signal);
    if (ac.signal.aborted) return null;
    setPending(null);
    if (!end.result) {
      setRefused({ layer, code: end.ok ? '' : end.code, message: end.ok ? '' : end.message });
      return null;
    }
    return end.result;
  };
  return { pending, refused, setRefused, go };
}

const reduced = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

// ExplorationDialogue is the level-2 zoom (#136, #137): the exploration
// dialogue as a stack of drawers. The current node's path is the stack:
// a node's chip under the turn it forked from pushes its drawer in from
// the right, ‹ or Esc pops it. Each node's box opens a node of the kind
// picked with its ＋, from the turn marked, as a narrow screen's does: its
// drawer comes in as the question goes, the reply streaming in there, and
// is the node's once it is kept. Covered layers keep a spine on the left,
// three at most, older ones sliding off; the header's path reaches every
// one. A read-only map of the whole tree sits on the right.
export function ExplorationDialogue({ detail: d, onDetail, onClose, escTaken }: {
  detail: ExplorationDetail; onDetail: (d: ExplorationDetail) => void; onClose: () => void;
  escTaken: MutableRefObject<() => boolean>;
}) {
  const main = d.nodes.find((n) => n.kind === 'main')!;
  const [current, setCurrent] = useState(main.id);
  const { pending, refused, setRefused, go } = useRuns();
  const [deleting, setDeleting] = useState<DialogueNode | null>(null);
  // the report's drawer is open over the node (#160)
  const [reporting, setReporting] = useState(false);
  // a node being opened: its drawer is on top while its first reply runs;
  // the last one stays drawn as its drawer leaves (refused), and whether
  // the reader stayed at the reply's end, for where the node opens
  const [opening, setOpening] = useState<Opening | null>(null);
  const lastOpening = useRef<Opening | null>(null);
  if (opening) lastOpening.current = opening;
  const openingFollow = useRef(true);
  const [kept, setKept] = useState<{ id: number; atEnd: boolean } | null>(null);

  // the node shown may be gone (deleted with its parent)
  const node = d.nodes.find((n) => n.id === current) ?? main;
  const want = [...pathTo(d.nodes, node.id).map((n) => String(n.id)), ...(opening ? [OPENING] : []), ...(reporting ? [REPORT] : [])];
  const [layers, setLayers] = useState<Layer[]>(() => want.map((key) => ({ key, phase: 'in' })));
  const key = want.join('/');
  useEffect(() => {
    setLayers((cur) => restack(cur, want));
    if (reduced()) { setLayers((cur) => prune(settle(cur))); return undefined; }
    const raf = requestAnimationFrame(() => requestAnimationFrame(() => setLayers(settle)));
    const t = window.setTimeout(() => setLayers(prune), MS + 40);
    return () => { cancelAnimationFrame(raf); window.clearTimeout(t); };
  }, [key]); // eslint-disable-line react-hooks/exhaustive-deps

  // no going elsewhere while a node is being opened: its drawer stays on
  // top until its first reply is kept (or refused, back where it was asked)
  const open = (id: number) => { if (opening) return; setReporting(false); setRefused(null); setCurrent(id); };
  const pop = () => {
    if (reporting) { setReporting(false); return true; }
    // a node being opened stays until its first reply is kept
    if (opening) return true;
    if (node.parentId != null) { open(node.parentId); return true; }
    return false;
  };
  escTaken.current = () => pop();

  // what a node's box sends: on in the node, or a new node, opened only
  // now its first question goes, and gone to once it is kept
  const send = async (at: DialogueNode, next: Next) => {
    if (next.to === 'turn') {
      const t = await go(String(at.id), next.question, (on, s) => askTurn(d.id, next.after, next.question, on, s));
      if (t) onDetail(withTurn(d, at.id, t as Turn));
      return;
    }
    if (next.to !== 'node') return;
    setReporting(false);
    setOpening({ kind: next.kind, fork: next.fork });
    const n = await go(OPENING, next.question, (on, s) => openNode(d.id, next.fork, next.kind, next.question, on, s));
    setOpening(null);
    if (!n) {
      // nothing kept: back in the node it was asked from, saying why
      setRefused((r) => (r ? { ...r, layer: String(at.id) } : r));
      return;
    }
    const id = (n as DialogueNode).id;
    setKept({ id, atEnd: openingFollow.current });
    setLayers((l) => rekey(l, OPENING, String(id)));
    onDetail({ ...d, nodes: [...d.nodes, n as DialogueNode] });
    setCurrent(id);
  };

  const retry = async (t: Turn) => {
    const got = await go(String(node.id), t.question, (on, s) => retryTurn(d.id, t.id, on, s), t.id);
    if (got) onDetail(withTurn(d, node.id, got as Turn));
  };

  const removeNode = async () => {
    if (!deleting) return;
    try {
      await deleteNode(d.id, deleting.id);
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '删除失败，请稍后再试');
      return;
    }
    if (deleting.parentId != null) setCurrent(deleting.parentId);
    onDetail({ ...d, nodes: withoutNode(d.nodes, deleting.id) });
    setDeleting(null);
  };

  const live = layers.filter((l) => l.phase !== 'leave');
  const count = d.nodes.length - 1;
  return (
    <div className="dlg">
      <header className="dlg-head">
        <Telescope aria-hidden />
        <span className="dlg-head-title">展开探索</span>
        <span className="dlg-head-sub">· {d.title}</span>
        <Crumbs nodes={d.nodes} current={node.id} opening={opening?.kind ?? null} onOpen={open} />
        <span className="dlg-head-count">节点 {count}</span>
        <button type="button" className={cn('dlg-head-report', reporting && 'on')} disabled={!!pending} aria-pressed={reporting}
          title={pending ? '这一轮回答完以后才能生成' : '把整棵对话树整理成一份文档'}
          aria-label="生成探索报告" onClick={() => { setRefused(null); setReporting(true); }}><FileText /><span>生成探索报告</span></button>
        <button type="button" className="dlg-close" aria-label="关闭" onClick={onClose}><X /></button>
      </header>
      <div className="dlg-body">
        <div className="dlg-stack">
          {layers.map((l) => {
            const i = live.indexOf(l);
            const at = l.phase === 'leave' ? { left: Math.min(live.length - 1, SHOWN) * SPINE, covered: false, gone: false } : spine(i, live.length, SPINE, SHOWN);
            const n = l.key === REPORT || l.key === OPENING ? null : d.nodes.find((x) => String(x.id) === l.key) ?? null;
            if (l.key !== REPORT && l.key !== OPENING && !n) return null;
            const placed = { left: at.left, zIndex: l.phase === 'leave' ? 99 : i, ['--gone' as string]: `${-SPINE * Math.max(live.length - 1 - i - SHOWN, 0)}px` };
            return (
              <Fragment key={l.key}>
                {/* a covered layer is inert; its spine, drawn over it, stays usable */}
                <section className={cn('dlg-layer', i > 0 || l.phase === 'leave' ? 'drawer' : 'base', `phase-${l.phase}`, at.gone && 'gone')}
                  style={placed} inert={at.covered || l.phase === 'leave' || undefined}>
                  <div className={cn('dlg-layer-inner', at.covered && 'covered')}>
                    {l.key === REPORT ? (
                      <ReportView exp={d.id} onBack={() => setReporting(false)} />
                    ) : l.key === OPENING ? (
                      lastOpening.current ? <OpeningView d={d} opening={lastOpening.current} pending={pending?.layer === OPENING ? pending : null} follow={openingFollow} /> : null
                    ) : n ? (
                      <NodeView d={d} node={n} pending={pending?.layer === l.key ? pending : null} refused={refused?.layer === l.key ? refused : null}
                        busy={!!pending} atEndFirst={kept?.id === n.id && kept.atEnd} onOpen={open} onSend={(next) => void send(n, next)} onRetry={retry}
                        onBack={pop} onDelete={() => setDeleting(n)} />
                    ) : null}
                  </div>
                </section>
                <Spine node={n} shown={at.covered && !at.gone} gone={at.gone} style={placed} onClick={() => (n ? open(n.id) : undefined)} />
              </Fragment>
            );
          })}
        </div>
        <NodeMap nodes={d.nodes} current={node.id} />
      </div>
      <Confirm open={!!deleting} title={deleting ? `删除节点「${deleting.name}」？` : ''} confirm="删除" onConfirm={removeNode} onClose={() => setDeleting(null)}>
        {deleting ? deleteText(d.nodes, deleting) : null}
      </Confirm>
    </div>
  );
}

// withTurn puts a kept turn into its node: in place of one with its ID
// (a retry), or after the others.
export function withTurn(d: ExplorationDetail, nodeId: number, t: Turn): ExplorationDetail {
  return {
    ...d,
    nodes: d.nodes.map((n) => {
      if (n.id !== nodeId) return n;
      const i = n.turns.findIndex((x) => x.id === t.id);
      return { ...n, turns: i >= 0 ? n.turns.map((x) => (x.id === t.id ? t : x)) : [...n.turns, t] };
    }),
  };
}

export function deleteText(nodes: DialogueNode[], n: DialogueNode) {
  let under = 0;
  const walk = (id: number) => nodes.filter((x) => x.parentId === id).forEach((x) => { under++; walk(x.id); });
  walk(n.id);
  return under ? `它下面的 ${under} 个节点会一起删除，无法恢复。` : '这个节点的对话会一起删除，无法恢复。';
}

// ---- pieces ----

// a node as the reader names it: the main node is 主节点, others by name
export const nameOf = (n: DialogueNode) => (n.kind === 'main' ? '主节点' : n.name);

export function Dot({ kind }: { kind: NodeKind }) {
  return <span className={cn('dlg-dot', `kind-${kind}`)} aria-hidden />;
}

// Spine is a covered layer's edge: its kind and its name standing on the
// left; a press pops everything above it. It fades: the tint first, then
// the dot and the name a beat later.
function Spine({ node, shown, gone, style, onClick }: { node: DialogueNode | null; shown: boolean; gone: boolean; style: CSSProperties; onClick: () => void }) {
  return (
    <button type="button" className={cn('dlg-spine', `kind-${node?.kind ?? 'child'}`, shown && 'shown', gone && 'gone')} tabIndex={shown ? 0 : -1} style={style}
      title={node ? `回到「${nameOf(node)}」` : undefined} onClick={onClick} aria-hidden={!shown}>
      <span className="dlg-spine-tint" />
      <span className="dlg-spine-label"><Dot kind={node?.kind ?? 'child'} /><span>{node ? nameOf(node) : '新节点'}</span></span>
    </button>
  );
}

// Crumbs is the whole path in the header, the layers gone off the left
// too; a step with siblings lists them.
function Crumbs({ nodes, current, opening, onOpen }: { nodes: DialogueNode[]; current: number; opening: NewKind | null; onOpen: (id: number) => void }) {
  const p = pathTo(nodes, current);
  const [sib, setSib] = useState<number | null>(null);
  if (p.length < 2 && !opening) return <span className="dlg-crumbs" />;
  return (
    <nav className="dlg-crumbs" aria-label="节点路径">
      {p.map((n, i) => {
        const sibs = n.parentId == null ? [] : nodes.filter((x) => x.parentId === n.parentId);
        const last = i === p.length - 1 && !opening;
        return (
          <span key={n.id} className="dlg-crumb">
            {i ? <span className="dlg-crumb-sep">›</span> : null}
            <button type="button" className={cn('dlg-crumb-btn', last && 'current', `kind-${n.kind}`)} onClick={() => onOpen(n.id)}>
              <Dot kind={n.kind} /><span>{nameOf(n)}</span>
            </button>
            {sibs.length > 1 ? <button type="button" className="dlg-crumb-more" aria-label="同级节点" onClick={() => setSib(sib === n.id ? null : n.id)}>▾</button> : null}
            {sib === n.id ? (
              <div className="dlg-menu dlg-crumb-menu">
                {sibs.map((x) => <button type="button" key={x.id} className={cn(x.id === n.id && 'current')} onClick={() => { setSib(null); onOpen(x.id); }}><Dot kind={x.kind} />{x.name}</button>)}
              </div>
            ) : null}
          </span>
        );
      })}
      {opening ? <span className="dlg-crumb"><span className="dlg-crumb-sep">›</span><span className={cn('dlg-crumb-btn current', `kind-${opening}`)}><Dot kind={opening} /><span>{openingName(opening)}</span></span></span> : null}
    </nav>
  );
}

// OpeningHead says what a node being opened is and where it forks.
export function OpeningHead({ d, opening }: { d: ExplorationDetail; opening: Opening }) {
  const at = turnAt(d.nodes, opening.fork);
  const from = at ? (at.node.kind === 'main' ? '主节点' : `「${at.node.name}」`) : '';
  return <div className="dlg-forkhead-title"><Dot kind={opening.kind} /><b>{openingName(opening.kind)}</b><span>从{from}第 {(at?.index ?? 0) + 1} 轮岔出</span></div>;
}

// OpeningView is the drawer of a node being opened: what it is, and its
// first question being answered.
function OpeningView({ d, opening, pending, follow }: { d: ExplorationDetail; opening: Opening; pending: Pending | null; follow: MutableRefObject<boolean> }) {
  const scroller = useRef<HTMLDivElement>(null);
  const { onScroll } = useFollow(scroller, pending, null, follow);
  return (
    <div className="dlg-node">
      <div className="dlg-scroll" ref={scroller} onScroll={onScroll}>
        <div className="dlg-col">
          <div className="dlg-forkhead"><OpeningHead d={d} opening={opening} /></div>
          {pending ? <Asking question={pending.question} run={pending.run} kind={opening.kind} turnNo={turnNoIn(d.nodes)} /> : null}
        </div>
      </div>
      <CreditLine />
    </div>
  );
}

// useFollow keeps a scroller at its end as a reply streams in (and as
// grown changes), while the reader stays there; one who scrolled up is
// left where they are until they scroll back down. follow, when given,
// is where whether they stay is kept. It returns the scroller's onScroll
// and follow.
export function useFollow(scroller: RefObject<HTMLDivElement | null>, pending: Pending | null, grown: unknown, kept?: MutableRefObject<boolean>) {
  const own = useRef(true);
  const follow = kept ?? own;
  const running = !!pending;
  // a new run starts at the end
  useLayoutEffect(() => { if (running) follow.current = true; }, [running, follow]);
  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && pending && follow.current) el.scrollTop = el.scrollHeight;
  }, [pending?.run.text, pending?.run.activities.length, pending?.run.thinking, pending?.run.linking, grown]); // eslint-disable-line react-hooks/exhaustive-deps
  return { onScroll: () => { const el = scroller.current; if (el) follow.current = atEnd(el); }, follow };
}

// NodeView is one node's drawer: the way back, where it forked, its turns
// (with the nodes opened on each), a turn being answered, and the box to
// ask the next question. The box is the node's own, kept while the reader
// is in a node above it: what is typed (the passages quoted from replies
// among the words), the kind of node to open with it and the turn marked.
function NodeView({ d, node, pending, refused, busy, atEndFirst, onOpen, onSend, onRetry, onBack, onDelete }: {
  d: ExplorationDetail; node: DialogueNode; pending: Pending | null; refused: Refused | null; busy: boolean;
  // a node just opened from a reply the reader followed to its end opens at its end
  atEndFirst: boolean;
  onOpen: (id: number) => void; onSend: (next: Next) => void; onRetry: (t: Turn) => void;
  onBack: () => boolean; onDelete: () => void;
}) {
  const hint = carried(node);
  const parent = node.parentId == null ? null : d.nodes.find((n) => n.id === node.parentId) ?? null;
  const scroller = useRef<HTMLDivElement>(null);
  const field = useRef<FieldHandle>(null);
  const [carry, setCarry] = useState<Carry>(noCarry);
  const [asked, setAsked] = useState<Question>(NOTHING);
  const [menu, setMenu] = useState(false);
  const carryDo = (e: CarryEvent) => setCarry((c) => stepCarry(c, e));
  const { onScroll } = useFollow(scroller, pending, node.turns.length);
  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && atEndFirst) el.scrollTop = el.scrollHeight;
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const full = d.nodes.length >= MAX_NODES;
  const next = nextQuestion(node, d.nodes, carry, asked);
  const tooLong = next.to === 'turn' && refused?.code === 'context_too_long' ? '这个节点已经太长，从更早的一轮开节点继续' : '';
  const blocked = next.to === 'none' ? next.reason : tooLong;
  const submit = () => {
    if (busy || next.to === 'none' || tooLong) return;
    field.current?.clear();
    carryDo({ type: 'sent' });
    setMenu(false);
    onSend(next);
  };
  const last = node.turns[node.turns.length - 1];
  // the turn a new node would fork from, marked in the dialogue and shown
  // as a tag: the turn marked, else (a kind picked) where it will fork
  const forkAt = carry.fork ?? (carry.kind ? asked.attachments.find((a) => a.turn != null)?.turn ?? last.id : null);
  const turnNo: TurnNo = turnNoIn(d.nodes);
  return (
    <div className="dlg-node">
      {parent ? (
        <div className="dlg-node-bar">
          <button type="button" className="dlg-back" onClick={onBack}><ChevronLeft />{parent.kind === 'main' ? '主节点' : parent.name}</button>
          <span className="grow" />
          <button type="button" className="dlg-node-delete" onClick={onDelete}><Trash2 />删除节点</button>
        </div>
      ) : null}
      <div className="dlg-scroll" ref={scroller} onScroll={onScroll}>
        <div className="dlg-col">
          {hint ? <p className="dlg-carried">{hint}</p> : null}
          {parent ? <ForkHead d={d} node={node} /> : null}
          {node.turns.map((t, i) => (
            pending?.retry === t.id ? <Asking key={t.id} question={pending.question} run={pending.run} kind={node.kind} turnNo={turnNo} /> : (
              <TurnView key={t.id} d={d} node={node} turn={t} first={node.kind === 'main' && i === 0} last={i === node.turns.length - 1}
                busy={busy} full={full} forking={forkAt === t.id} turnNo={turnNo} onOpen={onOpen} onRetry={onRetry}
                onFork={() => { carryDo({ type: 'fork', turn: t.id }); setMenu(true); }} />
            )
          ))}
          {pending && pending.retry == null ? <Asking question={pending.question} run={pending.run} kind={node.kind} turnNo={turnNo} /> : null}
          {refused?.message ? <Refusal refused={refused} /> : null}
        </div>
        <TakeSelection scroller={scroller} label="引用" icon={<TextQuote />} onTake={(t) => field.current?.insert({ text: t.text, turn: t.turn, start: t.start, end: t.end })} />
      </div>
      <Composer field={field} busy={busy} blocked={blocked} ready={next.to !== 'none' && !tooLong} turnNo={turnNo}
        placeholder={carry.kind ? `问点什么，开一个新的${KINDS[carry.kind].label}…` : '接着问…（选中回复里的文字可以引用）'}
        menu={menu} onMenu={setMenu} full={full} kind={carry.kind}
        menuHead={`开一个新节点 · ${forkAt != null && forkAt !== last.id ? `从第 ${turnNo(forkAt)} 轮岔出` : '从最后一轮岔出'}`}
        tags={carry.kind || carry.fork != null ? (
          <>
            {carry.kind ? <Tag className={`kind-${carry.kind}`} label={`去掉${KINDS[carry.kind].label}`} onX={() => carryDo({ type: 'unkind' })}><Dot kind={carry.kind} />{KINDS[carry.kind].label}</Tag> : null}
            {forkAt != null ? (
              <Tag className="fork" label="去掉岔出点" onX={carry.fork != null ? () => carryDo({ type: 'unfork' }) : undefined}><GitBranch />从第 {turnNo(forkAt)} 轮岔出</Tag>
            ) : null}
          </>
        ) : null}
        onKind={(k) => { carryDo({ type: 'kind', kind: k }); setMenu(false); field.current?.focus(); }} onChange={setAsked} onSend={submit} />
    </div>
  );
}

const NOTHING: Question = { text: '', attachments: [] };

// ForkHead says where a node forked and what it reads: the turn it forked
// from, then a line: everything above it is inherited, nothing after.
export function ForkHead({ d, node }: { d: ExplorationDetail; node: DialogueNode }) {
  const at = node.forkTurnId == null ? null : turnAt(d.nodes, node.forkTurnId);
  const seen = node.forkTurnId == null ? 0 : seenBy(d.nodes, node.forkTurnId).length;
  if (!at) return null;
  const from = at.node.kind === 'main' ? '主节点' : `「${at.node.name}」`;
  const turn = at.node.turns[at.index];
  return (
    <div className="dlg-forkhead">
      <div className="dlg-forkhead-title"><Dot kind={node.kind} /><b>{node.name}</b><span>{KINDS[node.kind].label} · 从{from}第 {at.index + 1} 轮岔出</span></div>
      <div className="dlg-forkhead-turn">
        <small>岔出的那一轮</small>
        <p>{plain(at.node.kind === 'main' && at.index === 0 ? `${d.title}：${d.concept}` : turn.reply)}</p>
      </div>
      <div className="dlg-rule"><span>继承到这一轮为止 · 共 {seen} 轮 · 之后的内容和其他节点不带进来</span></div>
    </div>
  );
}

// plain is the start of a reply as text, for a one-line excerpt.
const plain = (md: string) => md.replace(/<[^>]+>/g, ' ').replace(/[#*`>|_-]+/g, ' ').replace(/\s+/g, ' ').trim().slice(0, 120);

// TurnView is a question and its reply. The main node's first turn is the
// question the analysis stands for (the server's, ADR 0011) and the
// analysis in full, as the analysis popover shows it. Under the reply:
// copy, the icon to fork a node from the turn (greyed out once the
// dialogue is full), try again when it failed, and a chip for each node
// opened on it. forking: the turn is marked as where a new node forks from.
function TurnView({ d, node, turn: t, first, last, busy, full, forking, turnNo, onOpen, onFork, onRetry }: {
  d: ExplorationDetail; node: DialogueNode; turn: Turn; first: boolean; last: boolean; busy: boolean; full: boolean; forking: boolean;
  turnNo: TurnNo; onOpen: (id: number) => void; onFork: () => void; onRetry: (t: Turn) => void;
}) {
  const forks = forksAt(d.nodes, t.id);
  // what the reply may cite: the sources of its chain (ADR 0013)
  const table = useMemo(() => citationTable(d.nodes, t.id), [d.nodes, t.id]);
  const stop = t.meta.interrupted ? INTERRUPTED[t.meta.interrupted] : null;
  const retriable = !!t.meta.interrupted && last && !forks.length && !first;
  return (
    <div className={cn('dlg-turn exp-page-turn', forking && 'forking')} data-turn={t.id}>
      <Asked question={first ? { text: d.firstQuestion, attachments: [] } : t.question} kind={node.kind} turnNo={turnNo} />
      {forking ? <div className="exp-page-here"><GitBranch aria-hidden />从此处岔出</div> : null}
      {first ? (
        <div className="dlg-reply dlg-analysis">
          <h3 className="analysis-title">{d.title}</h3>
          <p className="analysis-concept">{d.concept}</p>
          <div className="analysis-label">概念解释</div>
          <div className="analysis-explanation"><LazyMarkdown text={d.explanation} components={documentParts} /></div>
        </div>
      ) : (
        <>
          {t.reply ? (
            <div className="dlg-reply prose-wrap">
              <CitedMarkdown text={t.reply} table={table} components={documentParts} />
            </div>
          ) : null}
          <CourseLinks courseId={d.courseId} links={t.courseLinks} />
          {stop ? (
            <div className={cn('dlg-stop', t.meta.interrupted === 'failed' && 'failed')} role="status">
              <div><b>{stop.title}</b><span>{stop.body}</span></div>
              {retriable && t.meta.interrupted !== 'credits_exhausted' ? <button type="button" disabled={busy} onClick={() => onRetry(t)}><RotateCcw />重试</button> : null}
            </div>
          ) : null}
        </>
      )}
      {first || t.reply ? (
        <div className="exp-page-acts dlg-turn-acts">
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

// Asked is a question as asked: the selections it carried unquoted above
// it, the passages it quoted as tags among its words.
function Asked({ question: q, kind, turnNo }: { question: Question; kind: NodeKind; turnNo: TurnNo }) {
  const { above } = readQuestion(q);
  return (
    <div className="dlg-asked">
      <div className="dlg-asked-bubble">
        {above.map((a, i) => <div key={i} className={cn('dlg-fragment', `kind-${kind}`)}><small>选中片段</small>「{a.text}」</div>)}
        {q.text ? <span><QuotedWords question={q} turnNo={turnNo} /></span> : null}
      </div>
    </div>
  );
}

const NO_SOURCES = new Map<number, Cited>();

// Asking is a turn being answered: the question, the lookups (a list
// while nothing is written, one line after), and the reply as it streams.
// asked, when given, shows the question in place of the usual bubble.
export function Asking({ question, run, kind, asked, turnNo = () => null }: { question: Question; run: Run; kind: NodeKind; asked?: ReactNode; turnNo?: TurnNo }) {
  return (
    <div className="dlg-turn asking" aria-live="polite" aria-busy="true">
      {asked ?? <Asked question={question} kind={kind} turnNo={turnNo} />}
      {run.text ? <Lookups activities={run.activities} /> : run.activities.length ? <Ticker activities={run.activities} /> : null}
      {run.thinking && !run.text ? <p className="analysis-wait thinking"><span className="analysis-dot" aria-hidden />正在思考…</p> : null}
      {!run.text && !run.thinking && !run.activities.length ? <p className="analysis-wait">正在回答…</p> : null}
      {/* its sources are known once it is kept: until then a citation shows nothing */}
      {run.text ? <div className={cn('dlg-reply analysis-explanation', !run.linking && 'caret')}><CitedMarkdown text={run.text} table={NO_SOURCES} /></div> : null}
      {run.linking ? <p className="analysis-wait thinking dlg-linking"><span className="analysis-dot" aria-hidden />正在关联课程内容…</p> : null}
    </div>
  );
}

// CourseLinks is the end of a reply that links it to the course (ADR
// 0011): a line for each lesson and section, with how it relates; nothing
// when the turn linked none.
export function CourseLinks({ courseId, links }: { courseId: number; links: Turn['courseLinks'] }) {
  const items = courseLinkItems(courseId, links);
  if (!items.length) return null;
  return (
    <aside className="dlg-links" aria-label="课程里的相关内容">
      <div className="dlg-links-title"><BookOpen aria-hidden />课程里的相关内容</div>
      <ul>
        {items.map((l) => (
          <li key={l.href}>
            <Link href={l.href} className="dlg-links-place">{l.place}</Link>
            {l.note ? <span>{l.note}</span> : null}
          </li>
        ))}
      </ul>
    </aside>
  );
}

export function Refusal({ refused }: { refused: Refused }) {
  const title = refused.code === 'context_too_long' ? '对话太长，模型容纳不下' : refused.code === 'credits_insufficient' ? '积分不足' : '没能发出这一轮';
  return <div className="dlg-stop" role="alert"><div><b>{title}</b><span>{refused.message}</span></div></div>;
}

// Taken is a passage selected in a reply: its text, its turn, the range on
// the page, and where it sits in the reply's text.
export type Taken = { turn: number; text: string; range: Range; start: number; end: number };

// TakeSelection offers a button (label) under a selection made in a reply
// of the node, once the selection is finished (see SelectionBar), to take
// it into the next question: carried with it on a narrow screen, quoted in
// its words on a wide one.
export function TakeSelection({ scroller, label, icon, onTake }: {
  scroller: RefObject<HTMLDivElement | null>; label: string; icon: ReactNode; onTake: (t: Taken) => void;
}) {
  const read = (at: Range): Taken | null => {
    const box = scroller.current;
    const raw = at.toString();
    const text = raw.trim().slice(0, 2000);
    const reply = (at.commonAncestorContainer instanceof Element ? at.commonAncestorContainer : at.commonAncestorContainer.parentElement)?.closest('.dlg-reply');
    const turn = reply?.closest<HTMLElement>('[data-turn]');
    if (!box || !text || !reply || !turn || !box.contains(turn)) return null;
    // where it starts in the reply's text
    const before = document.createRange();
    before.selectNodeContents(reply);
    before.setEnd(at.startContainer, at.startOffset);
    const start = before.toString().length + raw.length - raw.trimStart().length;
    return { turn: Number(turn.dataset.turn), text, range: at.cloneRange(), start, end: start + text.length };
  };
  return (
    <SelectionBar read={read} label={label} scroller={scroller}>
      {(t, done) => <button type="button" className="ann-btn" onClick={() => { done(); onTake(t); }}>{icon}{label}</button>}
    </SelectionBar>
  );
}

// Tag is a tag in the box, with ✕ to take it off unless onX is absent
// (a tag that follows from the others)
export function Tag({ className, label, onX, children }: { className: string; label: string; onX?: () => void; children: ReactNode }) {
  return <span className={cn('exp-page-tag', className, !onX && 'fixed')}>{children}{onX ? <button type="button" aria-label={label} onClick={onX}><X /></button> : null}</span>;
}

// KindMenu offers the kinds of node to open with the next question, over
// the box; head says where it forks from, kind is the one picked.
export function KindMenu({ head, full, kind, onKind }: { head: string; full: boolean; kind: NewKind | null; onKind: (k: NewKind) => void }) {
  return (
    <div className="exp-page-menu" role="menu" aria-label="节点类型">
      <div className="exp-page-menu-head">{head}</div>
      {NEW_KINDS.map((k) => (
        <button type="button" key={k} role="menuitem" disabled={full} className={cn(kind === k && 'on')} onClick={() => onKind(k)}>
          <Dot kind={k} /><span><b>{KINDS[k].label}</b><small>{KINDS[k].hint}</small></span>
        </button>
      ))}
      {full ? <div className="exp-page-menu-full">{FULL_TEXT}</div> : null}
    </div>
  );
}

// Composer is the box to ask the next question, a card over the foot of
// the drawer, with the credit line under it: the tags of what it carries
// (the kind of node, the turn marked) on a line of their own at its top,
// the field with the quotes among the words, and under them ＋ for the
// kinds of node (menu), why it cannot be sent (blocked), and send.
function Composer({ field, busy, blocked, ready, placeholder, turnNo, menu, onMenu, menuHead, full, kind, onKind, tags, onChange, onSend }: {
  field: RefObject<FieldHandle | null>; busy: boolean; blocked: string; ready: boolean; placeholder: string; turnNo: TurnNo;
  menu: boolean; onMenu: (on: boolean) => void; menuHead: string; full: boolean; kind: NewKind | null;
  onKind: (k: NewKind) => void; tags: ReactNode; onChange: (q: Question) => void; onSend: () => void;
}) {
  const box = useRef<HTMLDivElement>(null);
  // the kinds go on a press anywhere else
  useEffect(() => {
    if (!menu) return undefined;
    const away = (e: Event) => { if (!box.current?.contains(e.target as Node)) onMenu(false); };
    document.addEventListener('pointerdown', away);
    return () => document.removeEventListener('pointerdown', away);
  }, [menu, onMenu]);
  return (
    <div className="dlg-composer">
      <div className={cn('dlg-box dlg-card', busy && 'off')} ref={box}>
        {menu ? <KindMenu head={menuHead} full={full} kind={kind} onKind={onKind} /> : null}
        {tags ? <div className="dlg-card-tags">{tags}</div> : null}
        <QuoteField handle={field} placeholder={placeholder} label="接着问" turnNo={turnNo} onChange={onChange} onEnter={() => { if (ready && !busy) onSend(); }} />
        <div className="dlg-card-foot">
          <button type="button" className={cn('exp-page-plus', menu && 'on')} aria-label="开节点" title="开一个新节点" aria-expanded={menu} onClick={() => onMenu(!menu)}><Plus /></button>
          <span className="dlg-card-note">{blocked}</span>
          <button type="button" className={cn('dlg-send', busy && 'stop')} aria-label={busy ? '回答中' : '发送'} disabled={busy || !ready} onClick={onSend}>
            {busy ? <Square /> : <ArrowUp />}
          </button>
        </div>
      </div>
      <CreditLine />
    </div>
  );
}

// CreditLine names the service that writes the replies; it opens what is
// sent to it. It stays at the foot of every drawer.
export function CreditLine() {
  const [notice, setNotice] = useState(false);
  const user = useUser();
  return (
    <div className="dlg-composer dlg-credit">
      <Credit provider={user?.aiProvider} own={user?.ownModels} open={notice} onToggle={() => setNotice((v) => !v)} />
      {notice ? <Notice search={user?.aiSearch} className="dlg-notice" /> : null}
    </div>
  );
}

// NodeMap is the whole tree drawn top down, read only: where the reader
// is (a halo) and the path there; numbers on the lines say which turn a
// node forked from. It folds away with its fixed ≡ button.
function NodeMap({ nodes, current }: { nodes: DialogueNode[]; current: number }) {
  const [open, setOpen] = useState(true);
  const W = 260;
  const m = useMemo(() => mapLayout(nodes, { col: 60, row: 62, top: 46, left: 30 }), [nodes]);
  const shift = Math.max(0, (W - m.width - 6) / 2);
  const onPath = new Set(pathTo(nodes, current).map((n) => n.id));
  const spots = [...m.at.values()];
  return (
    <aside className={cn('dlg-map', open && 'open')} aria-label="节点图">
      <button type="button" className="dlg-map-toggle" title={open ? '收起节点图' : '展开节点图'} aria-expanded={open} onClick={() => setOpen((v) => !v)}><Menu /></button>
      {!open ? <div className="dlg-map-folded">节点图</div> : null}
      <div className="dlg-map-inner">
        <div className="dlg-map-title">节点图 · {nodes.length - 1} 个节点</div>
        <div className="dlg-map-scroll">
          <svg width={Math.max(W, m.width)} height={m.height + 20} role="img" aria-label="节点图">
            {spots.filter((s) => s.node.parentId != null).map((s) => {
              const p = m.at.get(s.node.parentId!)!;
              const hot = onPath.has(s.node.id);
              const my = (p.y + s.y) / 2;
              return (
                <g key={s.node.id} className={cn('dlg-map-edge', `kind-${s.node.kind}`, hot && 'hot')}>
                  <path d={`M${p.x + shift},${p.y + 6} C${p.x + shift},${my} ${s.x + shift},${my} ${s.x + shift},${s.y - 6}`} />
                  <text x={s.x + shift + 5} y={s.y - 12}>{s.fork}</text>
                </g>
              );
            })}
            {spots.map((s) => {
              const cur = s.node.id === current;
              const name = s.node.kind === 'main' ? '主节点' : s.node.name.length > 5 ? `${s.node.name.slice(0, 4)}…` : s.node.name;
              return (
                <g key={s.node.id} className={cn('dlg-map-node', `kind-${s.node.kind}`, onPath.has(s.node.id) && 'hot', cur && 'current')}>
                  <title>{s.node.name}</title>
                  {cur ? <circle className="halo" cx={s.x + shift} cy={s.y} r={10} /> : null}
                  <circle className="dot" cx={s.x + shift} cy={s.y} r={cur ? 4.5 : 4} />
                  <text x={s.x + shift} y={s.node.parentId == null ? s.y - 18 : s.y + 24}>{name}</text>
                </g>
              );
            })}
          </svg>
        </div>
      </div>
    </aside>
  );
}

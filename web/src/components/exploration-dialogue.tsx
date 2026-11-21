import { Fragment, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type MutableRefObject } from 'react';
import { BookOpen, ChevronLeft, Copy, FileText, Menu, Plus, RotateCcw, Square, Telescope, Trash2, X, ArrowUp } from 'lucide-react';
import { toast } from 'sonner';
import { Credit, documentParts, Lookups, Notice, Progress } from '@/components/analysis-popover';
import { Confirm } from '@/components/confirm';
import { ReportView } from '@/components/exploration-report';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { ApiError, api } from '@/lib/api';
import {
  askTurn, carried, courseLinkItems, DEFAULT_QUESTION, forksAt, INTERRUPTED, KINDS, mapLayout, MAX_NODES, openNode, pathTo, prune, restack, retryTurn,
  seenBy, settle, spine, stepRun, turnAt, type Ended, type Layer, type Run,
} from '@/lib/dialogue';
import type { DialogueNode, ExplorationDetail, NodeKind, Question, Turn } from '@/lib/explorations';
import { Link } from '@/lib/router';
import { useUser } from '@/lib/session';
import { cn } from '@/lib/utils';

const SPINE = 56; // the width of each covered layer left showing
const SHOWN = 3; // how many covered layers keep a spine
const MS = 340;

type Kind = Exclude<NodeKind, 'main'>;
// a node being opened: its kind, the turn it forks from, the passage
// selected in that turn's reply
type Draft = { kind: Kind; turn: number; fragment?: string };
// a turn being answered: on which node, after which turn (or as a node's
// first, or a retry in place), and what was asked
type Pending = { layer: string; question: Question; run: Run; retry?: number };
// what stopped a question before it ran, shown where it was asked
type Refused = { layer: string; code: string; message: string };

const reduced = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

// ExplorationDialogue is the level-2 zoom (#136, #137): the exploration
// dialogue as a stack of drawers. The current node's path is the stack:
// a node's chip under the turn it forked from pushes its drawer in from
// the right, ‹ or Esc pops it. Covered layers keep a spine on the left,
// three at most, older ones sliding off; the header's path reaches every
// one. A read-only map of the whole tree sits on the right.
export function ExplorationDialogue({ detail: d, onDetail, onClose, escTaken }: {
  detail: ExplorationDetail; onDetail: (d: ExplorationDetail) => void; onClose: () => void;
  escTaken: MutableRefObject<() => boolean>;
}) {
  const main = d.nodes.find((n) => n.kind === 'main')!;
  const [current, setCurrent] = useState(main.id);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);
  const [refused, setRefused] = useState<Refused | null>(null);
  const [deleting, setDeleting] = useState<DialogueNode | null>(null);
  // the report's drawer is open over the node (#160)
  const [reporting, setReporting] = useState(false);
  const abort = useRef<AbortController | null>(null);
  useEffect(() => () => abort.current?.abort(), []);

  // the node shown may be gone (deleted with its parent)
  const node = d.nodes.find((n) => n.id === current) ?? main;
  const want = [...pathTo(d.nodes, node.id).map((n) => String(n.id)), ...(draft ? ['draft'] : []), ...(reporting ? ['report'] : [])];
  const [layers, setLayers] = useState<Layer[]>(() => want.map((key) => ({ key, phase: 'in' })));
  const key = want.join('/');
  useEffect(() => {
    setLayers((cur) => restack(cur, want));
    if (reduced()) { setLayers((cur) => prune(settle(cur))); return undefined; }
    const raf = requestAnimationFrame(() => requestAnimationFrame(() => setLayers(settle)));
    const t = window.setTimeout(() => setLayers(prune), MS + 40);
    return () => { cancelAnimationFrame(raf); window.clearTimeout(t); };
  }, [key]); // eslint-disable-line react-hooks/exhaustive-deps

  const open = (id: number) => { setDraft(null); setReporting(false); setRefused(null); setCurrent(id); };
  const pop = () => {
    if (reporting) { setReporting(false); return true; }
    if (draft) { setDraft(null); return true; }
    if (node.parentId != null) { open(node.parentId); return true; }
    return false;
  };
  escTaken.current = () => pop();

  // a run of the dialogue, kept into the exploration however it ends
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

  const ask = async (question: Question) => {
    const last = node.turns[node.turns.length - 1];
    const t = await go(String(node.id), question, (on, s) => askTurn(d.id, last.id, question, on, s));
    if (t) onDetail(withTurn(d, node.id, t as Turn));
  };

  const retry = async (t: Turn) => {
    const got = await go(String(node.id), t.question, (on, s) => retryTurn(d.id, t.id, on, s), t.id);
    if (got) onDetail(withTurn(d, node.id, got as Turn));
  };

  const start = async (question: Question) => {
    if (!draft) return;
    const { kind, turn } = draft;
    const n = await go('draft', question, (on, s) => openNode(d.id, turn, kind, question, on, s));
    if (!n) return;
    onDetail({ ...d, nodes: [...d.nodes, n as DialogueNode] });
    setDraft(null);
    setCurrent((n as DialogueNode).id);
  };

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
    if (deleting.parentId != null) setCurrent(deleting.parentId);
    onDetail({ ...d, nodes: d.nodes.filter((n) => !gone.has(n.id)) });
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
        <Crumbs nodes={d.nodes} current={node.id} draft={draft} onOpen={open} />
        <span className="dlg-head-count">节点 {count}</span>
        <button type="button" className={cn('dlg-head-report', reporting && 'on')} disabled={!!pending} aria-pressed={reporting}
          title={pending ? '这一轮回答完以后才能生成' : '把整棵对话树整理成一份文档'}
          aria-label="生成探索报告" onClick={() => { setDraft(null); setRefused(null); setReporting(true); }}><FileText /><span>生成探索报告</span></button>
        <button type="button" className="dlg-close" aria-label="关闭" onClick={onClose}><X /></button>
      </header>
      <div className="dlg-body">
        <div className="dlg-stack">
          {layers.map((l) => {
            const i = live.indexOf(l);
            const at = l.phase === 'leave' ? { left: Math.min(live.length - 1, SHOWN) * SPINE, covered: false, gone: false } : spine(i, live.length, SPINE, SHOWN);
            const n = l.key === 'draft' || l.key === 'report' ? null : d.nodes.find((x) => String(x.id) === l.key) ?? null;
            if (l.key !== 'draft' && l.key !== 'report' && !n) return null;
            const placed = { left: at.left, zIndex: l.phase === 'leave' ? 99 : i, ['--gone' as string]: `${-SPINE * Math.max(live.length - 1 - i - SHOWN, 0)}px` };
            return (
              <Fragment key={l.key}>
                {/* a covered layer is inert; its spine, drawn over it, stays usable */}
                <section className={cn('dlg-layer', i > 0 || l.phase === 'leave' ? 'drawer' : 'base', `phase-${l.phase}`, at.gone && 'gone')}
                  style={placed} inert={at.covered || l.phase === 'leave' || undefined}>
                  <div className={cn('dlg-layer-inner', at.covered && 'covered')}>
                    {l.key === 'report' ? (
                      <ReportView exp={d.id} onBack={() => setReporting(false)} />
                    ) : n ? (
                      <NodeView d={d} node={n} pending={pending?.layer === l.key ? pending : null} refused={refused?.layer === l.key ? refused : null}
                        busy={!!pending} onOpen={open} onFork={(f) => { setRefused(null); setDraft(f); }} onAsk={ask} onRetry={retry}
                        onBack={pop} onDelete={() => setDeleting(n)} />
                    ) : draft ? (
                      <DraftView d={d} draft={draft} pending={pending?.layer === 'draft' ? pending : null} refused={refused?.layer === 'draft' ? refused : null}
                        onCancel={() => setDraft(null)} onStart={start} />
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
function withTurn(d: ExplorationDetail, nodeId: number, t: Turn): ExplorationDetail {
  return {
    ...d,
    nodes: d.nodes.map((n) => {
      if (n.id !== nodeId) return n;
      const i = n.turns.findIndex((x) => x.id === t.id);
      return { ...n, turns: i >= 0 ? n.turns.map((x) => (x.id === t.id ? t : x)) : [...n.turns, t] };
    }),
  };
}

function deleteText(nodes: DialogueNode[], n: DialogueNode) {
  let under = 0;
  const walk = (id: number) => nodes.filter((x) => x.parentId === id).forEach((x) => { under++; walk(x.id); });
  walk(n.id);
  return under ? `它下面的 ${under} 个节点会一起删除，无法恢复。` : '这个节点的对话会一起删除，无法恢复。';
}

// ---- pieces ----

// a node as the reader names it: the main node is 主节点, others by name
const nameOf = (n: DialogueNode) => (n.kind === 'main' ? '主节点' : n.name);

function Dot({ kind }: { kind: NodeKind }) {
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
function Crumbs({ nodes, current, draft, onOpen }: { nodes: DialogueNode[]; current: number; draft: Draft | null; onOpen: (id: number) => void }) {
  const p = pathTo(nodes, current);
  const [sib, setSib] = useState<number | null>(null);
  if (p.length < 2 && !draft) return <span className="dlg-crumbs" />;
  return (
    <nav className="dlg-crumbs" aria-label="节点路径">
      {p.map((n, i) => {
        const sibs = n.parentId == null ? [] : nodes.filter((x) => x.parentId === n.parentId);
        const last = i === p.length - 1 && !draft;
        return (
          <span key={n.id} className="dlg-crumb">
            {i ? <span className="dlg-crumb-sep">›</span> : null}
            <button type="button" className={cn('dlg-crumb-btn', last && 'current', `kind-${n.kind}`)} onClick={() => onOpen(n.id)}>
              <Dot kind={n.kind} />{nameOf(n)}
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
      {draft ? <span className="dlg-crumb"><span className="dlg-crumb-sep">›</span><span className={cn('dlg-crumb-btn current', `kind-${draft.kind}`)}><Dot kind={draft.kind} />新{KINDS[draft.kind].label}</span></span> : null}
    </nav>
  );
}

// NodeView is one node's drawer: the way back, where it forked, its turns
// (with the nodes opened on each), a turn being answered, and the box to
// ask the next question.
function NodeView({ d, node, pending, refused, busy, onOpen, onFork, onAsk, onRetry, onBack, onDelete }: {
  d: ExplorationDetail; node: DialogueNode; pending: Pending | null; refused: Refused | null; busy: boolean;
  onOpen: (id: number) => void; onFork: (f: Draft) => void; onAsk: (q: Question) => void; onRetry: (t: Turn) => void;
  onBack: () => boolean; onDelete: () => void;
}) {
  const hint = carried(node);
  const parent = node.parentId == null ? null : d.nodes.find((n) => n.id === node.parentId) ?? null;
  const scroller = useRef<HTMLDivElement>(null);
  const last = node.turns[node.turns.length - 1];
  // stay at the end as a reply streams in
  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && pending) el.scrollTop = el.scrollHeight;
  }, [pending?.run.text, pending?.run.activities.length, node.turns.length]); // eslint-disable-line react-hooks/exhaustive-deps
  const failed = last.meta.interrupted === 'failed';
  return (
    <div className="dlg-node">
      {parent ? (
        <div className="dlg-node-bar">
          <button type="button" className="dlg-back" onClick={onBack}><ChevronLeft />{parent.kind === 'main' ? '主节点' : parent.name}</button>
          <span className="grow" />
          <button type="button" className="dlg-node-delete" onClick={onDelete}><Trash2 />删除节点</button>
        </div>
      ) : null}
      <div className="dlg-scroll" ref={scroller}>
        <div className="dlg-col">
          {hint ? <p className="dlg-carried">{hint}</p> : null}
          {parent ? <ForkHead d={d} node={node} /> : null}
          {node.turns.map((t, i) => (
            pending?.retry === t.id ? <Asking key={t.id} question={pending.question} run={pending.run} kind={node.kind} /> : (
              <TurnView key={t.id} d={d} node={node} turn={t} first={node.kind === 'main' && i === 0} last={i === node.turns.length - 1}
                busy={busy} onOpen={onOpen} onFork={onFork} onRetry={onRetry} />
            )
          ))}
          {pending && pending.retry == null ? <Asking question={pending.question} run={pending.run} kind={node.kind} /> : null}
          {refused?.message ? <Refusal refused={refused} /> : null}
        </div>
      </div>
      <Composer busy={busy} disabled={failed && !pending ? '重试这一轮后才能接着问' : refused?.code === 'context_too_long' ? '这个节点已经太长，从更早的一轮开节点继续' : ''}
        onAsk={(text) => onAsk({ text, attachments: [] })} />
    </div>
  );
}

// ForkHead says where a node forked and what it reads: the turn it forked
// from, then a line: everything above it is inherited, nothing after.
function ForkHead({ d, node }: { d: ExplorationDetail; node: DialogueNode }) {
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
// open a node (a passage selected in the reply goes with it), copy, try
// again when it failed, and a chip for each node opened on it.
function TurnView({ d, node, turn: t, first, last, busy, onOpen, onFork, onRetry }: {
  d: ExplorationDetail; node: DialogueNode; turn: Turn; first: boolean; last: boolean; busy: boolean;
  onOpen: (id: number) => void; onFork: (f: Draft) => void; onRetry: (t: Turn) => void;
}) {
  const [menu, setMenu] = useState<false | 'down' | 'up'>(false);
  const [fragment, setFragment] = useState('');
  const reply = useRef<HTMLDivElement>(null);
  const forks = forksAt(d.nodes, t.id);
  const stop = t.meta.interrupted ? INTERRUPTED[t.meta.interrupted] : null;
  const full = d.nodes.length >= MAX_NODES;
  // a passage selected inside this reply goes with a node opened on it
  const noteSelection = () => {
    const sel = window.getSelection();
    const text = sel?.toString().trim() ?? '';
    setFragment(text && sel?.anchorNode && reply.current?.contains(sel.anchorNode) ? text.slice(0, 2000) : '');
  };
  useEffect(() => {
    if (!menu) return undefined;
    const close = (e: MouseEvent) => { if (!(e.target as HTMLElement).closest('.dlg-fork')) setMenu(false); };
    document.addEventListener('mousedown', close);
    return () => document.removeEventListener('mousedown', close);
  }, [menu]);
  const retriable = !!t.meta.interrupted && last && !forks.length && !first;
  return (
    <div className="dlg-turn" data-turn={t.id}>
      {first ? (
        <>
          <Asked question={{ text: d.firstQuestion, attachments: [] }} kind={node.kind} />
          <div className="dlg-reply dlg-analysis" ref={reply} onMouseUp={noteSelection}>
            <h3 className="analysis-title">{d.title}</h3>
            <p className="analysis-concept">{d.concept}</p>
            <div className="analysis-label">概念解释</div>
            <div className="analysis-explanation"><LazyMarkdown text={d.explanation} components={documentParts} /></div>
          </div>
        </>
      ) : (
        <>
          <Asked question={t.question} kind={node.kind} />
          {t.reply ? (
            <div className="dlg-reply prose-wrap" ref={reply} onMouseUp={noteSelection}>
              <LazyMarkdown text={t.reply} components={documentParts} />
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
      {t.meta.interrupted === 'failed' && !t.reply ? null : (
        <div className="dlg-acts">
          <span className="dlg-fork">
            <button type="button" className={cn('dlg-act', menu && 'on')} disabled={busy} aria-expanded={!!menu}
              onMouseDown={noteSelection}
              onClick={(e) => {
                // the menu opens upward when there is no room for it below
                const below = window.innerHeight - e.currentTarget.getBoundingClientRect().bottom;
                setMenu((v) => (v ? false : below < 320 ? 'up' : 'down'));
              }}><Plus />开节点</button>
            {menu ? (
              <div className={cn('dlg-menu dlg-fork-menu', menu === 'up' && 'up')} role="menu">
                {(['child', 'branch', 'divergent'] as const).map((k) => (
                  <button type="button" key={k} role="menuitem" disabled={full} onClick={() => { setMenu(false); onFork({ kind: k, turn: t.id, fragment: fragment || undefined }); }}>
                    <Dot kind={k} /><span><b>{KINDS[k].label}</b><small>{KINDS[k].hint}</small></span>
                  </button>
                ))}
                <div className={cn('dlg-fork-note', full && 'full')}>
                  {full ? `已有 ${MAX_NODES} 个节点，删除一些后才能再开` : fragment ? `带上选中的「${fragment.slice(0, 16)}${fragment.length > 16 ? '…' : ''}」` : '可以先在回复里选中一段，作为重点'}
                </div>
              </div>
            ) : null}
          </span>
          {!first ? <button type="button" className="dlg-act" onClick={() => { void navigator.clipboard?.writeText(t.reply); toast.success('已复制'); }}><Copy />复制</button> : null}
          {forks.map((n) => (
            <button type="button" key={n.id} className={cn('dlg-chip', `kind-${n.kind}`)} onClick={() => onOpen(n.id)} title={`${KINDS[n.kind].label}：${n.name}`}>
              <Dot kind={n.kind} /><span>{n.name}</span><small>{n.turns.length} 轮</small>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function Asked({ question: q, kind }: { question: Question; kind: NodeKind }) {
  return (
    <div className="dlg-asked">
      <div className="dlg-asked-bubble">
        {q.attachments.map((a, i) => <div key={i} className={cn('dlg-fragment', `kind-${kind}`)}><small>选中片段</small>「{a.text}」</div>)}
        {q.text ? <span>{q.text}</span> : null}
      </div>
    </div>
  );
}

// Asking is a turn being answered: the question, the lookups (a list
// while nothing is written, one line after), and the reply as it streams.
function Asking({ question, run, kind }: { question: Question; run: Run; kind: NodeKind }) {
  return (
    <div className="dlg-turn asking" aria-live="polite" aria-busy="true">
      <Asked question={question} kind={kind} />
      {run.text ? <Lookups activities={run.activities} /> : run.activities.length ? <Progress activities={run.activities} /> : null}
      {run.thinking && !run.text ? <p className="analysis-wait thinking"><span className="analysis-dot" aria-hidden />正在思考…</p> : null}
      {!run.text && !run.thinking && !run.activities.length ? <p className="analysis-wait">正在回答…</p> : null}
      {run.text ? <div className={cn('dlg-reply analysis-explanation', !run.linking && 'caret')}><LazyMarkdown text={run.text} /></div> : null}
      {run.linking ? <p className="analysis-wait thinking dlg-linking"><span className="analysis-dot" aria-hidden />正在关联课程内容…</p> : null}
    </div>
  );
}

// CourseLinks is the end of a reply that links it to the course (ADR
// 0011): a line for each lesson and section, with how it relates; nothing
// when the turn linked none.
function CourseLinks({ courseId, links }: { courseId: number; links: Turn['courseLinks'] }) {
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

function Refusal({ refused }: { refused: Refused }) {
  const title = refused.code === 'context_too_long' ? '对话太长，模型容纳不下' : refused.code === 'credits_insufficient' ? '积分不足' : '没能发出这一轮';
  return <div className="dlg-stop" role="alert"><div><b>{title}</b><span>{refused.message}</span></div></div>;
}

// Composer is the box to ask the next question, with the credit line.
function Composer({ busy, disabled, onAsk }: { busy: boolean; disabled: string; onAsk: (text: string) => void }) {
  const [text, setText] = useState('');
  const send = () => {
    const q = text.trim();
    if (!q || busy || disabled) return;
    setText('');
    onAsk(q);
  };
  return (
    <div className="dlg-composer">
      <div className={cn('dlg-box', (disabled || busy) && 'off')}>
        <textarea value={text} rows={2} placeholder={disabled || '接着问…'} disabled={!!disabled} aria-label="接着问"
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

// DraftView is a node about to be opened: what it will read, then its
// first question, the kind's default to start from, the selected passage
// removable.
function DraftView({ d, draft, pending, refused, onCancel, onStart }: {
  d: ExplorationDetail; draft: Draft; pending: Pending | null; refused: Refused | null; onCancel: () => void; onStart: (q: Question) => void;
}) {
  const [text, setText] = useState(DEFAULT_QUESTION[draft.kind]);
  const [fragment, setFragment] = useState(draft.fragment);
  const at = turnAt(d.nodes, draft.turn);
  const seen = seenBy(d.nodes, draft.turn);
  const from = at ? (at.node.kind === 'main' ? '主节点' : `「${at.node.name}」`) : '';
  const question: Question = { text: text.trim(), attachments: fragment ? [{ kind: 'selection', text: fragment }] : [] };
  return (
    <div className="dlg-node">
      <div className="dlg-node-bar"><button type="button" className="dlg-back" onClick={onCancel}><ChevronLeft />取消</button></div>
      <div className="dlg-scroll">
        <div className="dlg-col">
          {pending ? (
            <>
              <div className="dlg-forkhead-title"><Dot kind={draft.kind} /><b>新{KINDS[draft.kind].label}</b><span>从{from}第 {(at?.index ?? 0) + 1} 轮岔出</span></div>
              <Asking question={pending.question} run={pending.run} kind={draft.kind} />
            </>
          ) : (
            <div className="dlg-draft">
              <div className="dlg-forkhead-title"><Dot kind={draft.kind} /><b>新{KINDS[draft.kind].label}</b><span>从{from}第 {(at?.index ?? 0) + 1} 轮岔出</span></div>
              <div className="dlg-seen">
                <small>这个节点看得到的对话</small>
                <ol>{seen.map((t, i) => <li key={t.id}>{i === 0 ? `分析：${d.title}` : t.question.text || `「${t.question.attachments[0]?.text ?? ''}」`}</li>)}</ol>
                <small>{from}在第 {(at?.index ?? 0) + 1} 轮之后的内容、其他节点都不会带进来。</small>
              </div>
              <div className={cn('dlg-first', `kind-${draft.kind}`)}>
                {fragment ? (
                  <div className={cn('dlg-fragment', `kind-${draft.kind}`)}>
                    <small>选中片段</small>「{fragment}」
                    <button type="button" aria-label="去掉选中片段" onClick={() => setFragment(undefined)}><X /></button>
                  </div>
                ) : null}
                <textarea value={text} rows={3} aria-label="第一个问题" onChange={(e) => setText(e.target.value)} />
                <div className="dlg-first-foot">
                  <span>{text === DEFAULT_QUESTION[draft.kind] ? '默认提问，可以直接改' : '已修改，会作为这个节点的第一条提问'}</span>
                  <button type="button" className="dlg-btn" onClick={onCancel}>取消</button>
                  <button type="button" className={cn('dlg-btn primary', `kind-${draft.kind}`)} disabled={!question.text && !fragment} onClick={() => onStart(question)}>开始</button>
                </div>
              </div>
              <p className="dlg-hint">节点类型开了以后不能改。</p>
            </div>
          )}
          {refused?.message ? <Refusal refused={refused} /> : null}
        </div>
      </div>
      <CreditLine />
    </div>
  );
}

// CreditLine names the service that writes the replies; it opens what is
// sent to it. It stays at the foot of every drawer.
function CreditLine() {
  const [notice, setNotice] = useState(false);
  const user = useUser();
  return (
    <div className="dlg-composer dlg-credit">
      <Credit provider={user?.aiProvider} open={notice} onToggle={() => setNotice((v) => !v)} />
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

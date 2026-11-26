// The exploration dialogue as the level-2 zoom shows it (#136, #137, ADR
// 0009): a tree of nodes, each a line of turns. The current node's path is
// a stack of drawers; the node map draws the whole tree.

import { postStream, type StreamEvent } from '@/lib/analysis';
import type { CourseLink, DialogueNode, NodeKind, Question, Turn } from '@/lib/explorations';
import { track, type ToolActivity } from '@/lib/progress';
import { MAX_QUOTES } from '@/lib/quotes';

export const KINDS: Record<NodeKind, { label: string; hint: string }> = {
  main: { label: '主节点', hint: '' },
  child: { label: '子节点', hint: '沿这一轮继续追问，往深处挖' },
  branch: { label: '分支节点', hint: '横向拓宽，讲相关的概念和它们的关系' },
  divergent: { label: '发散节点', hint: '走出课程，看同类的其他内容' },
};

// the kinds a reader may open a node of, in the order they are offered
export type NewKind = Exclude<NodeKind, 'main'>;
export const NEW_KINDS: NewKind[] = ['child', 'branch', 'divergent'];

export const MAX_NODES = 50;

// what the follow-ups bring along as background (ADR 0011): the course,
// the selected passage and the analysis
const BACKGROUND = ['课程信息', '选中的原文', '分析'];

// carried is the faint line atop the main node naming that background; it
// shows once there, on no other node, and is no part of any turn.
export const carried = (n: DialogueNode): string | null =>
  n.kind === 'main' ? `带上了：${BACKGROUND.join(' · ')}` : null;

const byId = (nodes: DialogueNode[], id: number) => nodes.find((n) => n.id === id);

// pathTo is the node and its ancestors, from the main node down.
export function pathTo(nodes: DialogueNode[], id: number): DialogueNode[] {
  const out: DialogueNode[] = [];
  for (let n = byId(nodes, id); n; n = n.parentId == null ? undefined : byId(nodes, n.parentId)) out.unshift(n);
  return out;
}

// withoutNode is the dialogue with a node gone, and every node under it.
export function withoutNode(nodes: DialogueNode[], id: number): DialogueNode[] {
  const gone = new Set([id]);
  for (let grew = true; grew;) {
    grew = false;
    for (const n of nodes) if (n.parentId != null && gone.has(n.parentId) && !gone.has(n.id)) { gone.add(n.id); grew = true; }
  }
  return nodes.filter((n) => !gone.has(n.id));
}

// turnNoIn says where a turn sits in its node (1-based), null for one
// not in the dialogue; a quote's tag names its turn by it.
export const turnNoIn = (nodes: DialogueNode[]) => (turn: number): number | null => {
  const at = turnAt(nodes, turn);
  return at ? at.index + 1 : null;
};

// forksAt is the nodes opened on a turn, oldest first.
export const forksAt = (nodes: DialogueNode[], turnId: number) => nodes.filter((n) => n.forkTurnId === turnId);

// turnAt finds a turn: its node and its place there.
export function turnAt(nodes: DialogueNode[], turnId: number): { node: DialogueNode; index: number } | null {
  for (const node of nodes) {
    const index = node.turns.findIndex((t) => t.id === turnId);
    if (index >= 0) return { node, index };
  }
  return null;
}

// seenBy is what a node forked at turn `at` reads of the dialogue: the
// chain up to that turn, each parent cut where its child forked.
export function seenBy(nodes: DialogueNode[], at: number): Turn[] {
  const t = turnAt(nodes, at);
  if (!t) return [];
  let out = t.node.turns.slice(0, t.index + 1);
  for (let n = t.node; n.parentId != null;) {
    const p = byId(nodes, n.parentId);
    if (!p) break;
    const cut = p.turns.findIndex((x) => x.id === n.forkTurnId);
    out = [...p.turns.slice(0, cut + 1), ...out];
    n = p;
  }
  return out;
}

// ---- what the next question carries (a narrow screen's box) ----

// Carry is what the next question takes along, shown as tags in the box:
// the kind of node to open with it, the turn marked to fork from, and a
// selection (选段) taken from a reply, with its turn. Each is optional.
export interface Carry { kind: NewKind | null; fork: number | null; selection: { turn: number; text: string } | null }

export const noCarry: Carry = { kind: null, fork: null, selection: null };

// kind / fork / select add a tag (a selection takes the place of the one
// before: one at a time); unkind takes off the kind and the fork mark
// with it, unfork and unselect take off theirs; sent clears the box.
export type CarryEvent =
  | { type: 'kind'; kind: NewKind }
  | { type: 'unkind' }
  | { type: 'fork'; turn: number }
  | { type: 'unfork' }
  | { type: 'select'; selection: { turn: number; text: string } }
  | { type: 'unselect' }
  | { type: 'sent' };

export function stepCarry(c: Carry, e: CarryEvent): Carry {
  switch (e.type) {
    case 'kind': return { ...c, kind: e.kind };
    case 'unkind': return { ...c, kind: null, fork: null };
    case 'fork': return { ...c, fork: e.turn };
    case 'unfork': return { ...c, fork: null };
    case 'select': return { ...c, selection: e.selection };
    case 'unselect': return { ...c, selection: null };
    case 'sent': return noCarry;
  }
}

// what sending the box does: ask on in the current node after its last
// turn, open a node forked from a turn with the question as its first, or
// nothing (reason says why, '' when there is just nothing to send)
export type Next =
  | { to: 'turn'; after: number; question: Question }
  | { to: 'node'; fork: number; kind: NewKind; question: Question }
  | { to: 'none'; reason: string };

export const FULL_TEXT = `已有 ${MAX_NODES} 个节点，删除一些后才能再开`;

// nextQuestion is what the box sends from node, with what it carries and
// what was typed: words (a narrow screen's box), or a question with quotes
// in its words (a wide screen's, see quotes.ts). A new node forks from the
// turn marked, else the selection's turn, else the first quote's, else the
// node's last turn; a fork marked waits for a kind. No node opens past
// MAX_NODES, no question carries more than MAX_QUOTES quotes.
export function nextQuestion(node: DialogueNode, nodes: DialogueNode[], c: Carry, typed: string | Question): Next {
  const asked = typeof typed === 'string' ? { text: typed.trim(), attachments: [] } : typed;
  const question: Question = { text: asked.text, attachments: [...(c.selection ? [{ kind: 'selection' as const, text: c.selection.text }] : []), ...asked.attachments] };
  const empty = !question.text && !question.attachments.length;
  const last = node.turns[node.turns.length - 1];
  if (question.attachments.length > MAX_QUOTES) return { to: 'none', reason: `一个问题最多引用 ${MAX_QUOTES} 段` };
  if (c.kind) {
    if (nodes.length >= MAX_NODES) return { to: 'none', reason: FULL_TEXT };
    if (empty) return { to: 'none', reason: '' };
    return { to: 'node', fork: c.fork ?? c.selection?.turn ?? asked.attachments.find((a) => a.turn != null)?.turn ?? last.id, kind: c.kind, question };
  }
  if (c.fork != null) return { to: 'none', reason: '先选节点类型' };
  if (last.meta.interrupted === 'failed') return { to: 'none', reason: '重试这一轮后才能接着问' };
  if (empty) return { to: 'none', reason: '' };
  return { to: 'turn', after: last.id, question };
}

// ---- the stack of drawers ----

// Layer is a drawer on screen: entering slides in from the right, leaving
// slides out before it goes.
export interface Layer { key: string; phase: 'in' | 'enter' | 'leave' }

// restack brings the layers on screen to the wanted stack: kept ones stay,
// new ones enter, gone ones leave (and stay drawn until pruned).
export function restack(cur: Layer[], want: string[]): Layer[] {
  const kept = cur.filter((l) => want.includes(l.key)).map((l) => (l.phase === 'leave' ? { ...l, phase: 'in' as const } : l));
  const added = want.filter((k) => !cur.some((l) => l.key === k)).map((key) => ({ key, phase: 'enter' as const }));
  const gone = cur.filter((l) => !want.includes(l.key)).map((l) => ({ ...l, phase: 'leave' as const }));
  const order = (l: Layer) => want.indexOf(l.key);
  return [...[...kept, ...added].sort((a, b) => order(a) - order(b)), ...gone];
}

// settle lets entering layers in; prune drops the ones that left.
export const settle = (l: Layer[]) => l.map((x) => (x.phase === 'enter' ? { ...x, phase: 'in' as const } : x));
export const prune = (l: Layer[]) => l.filter((x) => x.phase !== 'leave');

// spine is where live layer i of n sits: the top one right of the spines
// of the layers it covers, each covered one a spine apart; past `shown`
// covered layers, the oldest are gone off the left.
export function spine(i: number, n: number, width: number, shown: number) {
  const covered = Math.min(n - 1, shown);
  const fromTop = n - 1 - i;
  return { left: Math.max(covered - fromTop, 0) * width, covered: fromTop > 0, gone: fromTop > shown };
}

// ---- the node map ----

export interface MapSpot { node: DialogueNode; x: number; y: number; depth: number; fork?: number }

// mapLayout lays the tree out top down: depth is the row, leaves take a
// column each from the left in the order of the turns they forked from,
// a parent sits over the middle of its children. fork is which turn of
// its parent a node forked from (1-based).
export function mapLayout(nodes: DialogueNode[], o: { col: number; row: number; top: number; left: number }) {
  const at = new Map<number, MapSpot>();
  const root = nodes.find((n) => n.kind === 'main');
  let col = 0;
  const walk = (n: DialogueNode, depth: number, fork?: number): MapSpot => {
    const kids = n.turns.flatMap((t, i) => forksAt(nodes, t.id).map((k) => walk(k, depth + 1, i + 1)));
    const x = kids.length ? (kids[0].x + kids[kids.length - 1].x) / 2 : o.left + col++ * o.col;
    const spot = { node: n, x, y: o.top + depth * o.row, depth, fork };
    at.set(n.id, spot);
    return spot;
  };
  if (root) walk(root, 0);
  const spots = [...at.values()];
  return { at, width: o.left + Math.max(col, 1) * o.col, height: (spots.length ? Math.max(...spots.map((s) => s.y)) : 0) + o.row };
}

// ---- a run of the dialogue ----

// Run is a turn being answered as it streams: the reply so far, the
// lookups, whether the model is thinking, and whether, the answer written,
// it is linking it to the course (ADR 0011).
export interface Run { text: string; activities: ToolActivity[]; thinking: boolean; linking?: boolean }

export function stepRun(s: Run | undefined, e: StreamEvent): Run {
  const r = s ?? { text: '', activities: [], thinking: false };
  switch (e.type) {
    case 'delta':
      return e.reset ? { ...r, text: '' } : { ...r, text: r.text + (e.text ?? ''), thinking: false };
    case 'activity':
      if (e.kind === 'thinking') return { ...r, thinking: e.status === 'running' };
      if (e.kind === 'link_course') return { ...r, linking: e.status === 'running' };
      return { ...r, activities: track(r.activities, e as unknown as ToolActivity) };
    default:
      return r;
  }
}

// CourseLinkItem is a line of a reply's 「课程里的相关内容」: where in the
// course, the reader's link there, and how it relates to the answer.
export interface CourseLinkItem { href: string; place: string; note: string }

// courseLinkItems are a turn's course links as the reply's end lists them,
// each linked to its lesson and section in the reader.
export function courseLinkItems(courseId: number, links: CourseLink[] | undefined): CourseLinkItem[] {
  return (links ?? []).map((l) => ({
    href: `/courses/${courseId}/lessons/${l.lesson.id}${l.section ? `#${encodeURIComponent(l.section.id)}` : ''}`,
    place: l.section ? `${l.lesson.title} › ${l.section.title}` : l.lesson.title,
    note: l.note,
  }));
}

// what the stream's last event says: done carries the turn or node kept;
// error says why the reply stops short, with what was kept (if anything)
export type Ended = { ok: true; result: any } | { ok: false; code: string; message: string; result?: any };

const base = (exp: number) => `/api/explorations/${exp}`;

// runTurn streams a run and returns how it ended.
async function run(path: string, body: unknown, on: (e: StreamEvent) => void, signal: AbortSignal): Promise<Ended> {
  let end: Ended = { ok: false, code: 'network', message: '连接中断了，请重试。' };
  await postStream(path, body, (e) => {
    if (e.type === 'done') end = { ok: true, result: e.result };
    else if (e.type === 'error') end = { ok: false, code: e.code, message: e.message || '这一轮没有完成，请重试。', result: e.result };
    else on(e);
  }, signal);
  return end;
}

export const askTurn = (exp: number, after: number, question: Question, on: (e: StreamEvent) => void, signal: AbortSignal) =>
  run(`${base(exp)}/turns`, { after, question }, on, signal);

export const retryTurn = (exp: number, turn: number, on: (e: StreamEvent) => void, signal: AbortSignal) =>
  run(`${base(exp)}/turns/${turn}/retry`, {}, on, signal);

export const openNode = (exp: number, fork: number, kind: NewKind, question: Question, on: (e: StreamEvent) => void, signal: AbortSignal) =>
  run(`${base(exp)}/nodes`, { fork, kind, question }, on, signal);

// interruption says why a kept reply stops short
export const INTERRUPTED: Record<string, { title: string; body: string }> = {
  credits_exhausted: { title: '因积分用完而中断', body: '已经生成的部分保留在这里。积分每天 0 点重置。' },
  timeout: { title: '运行超时，回复没有写完', body: '已经生成的部分保留在这里，可以重试这一轮。' },
  truncated: { title: '回复太长，被截断了', body: '已经生成的部分保留在这里，可以重试这一轮。' },
  failed: { title: '这一轮没有完成', body: '重试会在原位重新生成这一轮。' },
};

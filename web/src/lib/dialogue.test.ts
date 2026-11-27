import { describe, expect, it } from 'vitest';
import { atEnd, rekey, openingName, carried, courseLinkItems, withoutNode, forksAt, mapLayout, MAX_NODES, nextQuestion, noCarry, pathTo, prune, restack, seenBy, settle, spine, stepCarry, stepRun, type Carry, type Layer } from './dialogue';
import { MAX_QUOTES, toQuestion } from './quotes';
import type { DialogueNode, NodeKind } from './explorations';

let ids = 0;
const turn = (id: number, q = `问${id}`) => ({ id, parentId: null, question: { text: q, attachments: [] }, reply: `答${id}`, meta: { source: 'site' as const, model: 'm', sources: [] }, createdAt: '' });
const node = (id: number, kind: NodeKind, turns: number[], parentId: number | null = null, forkTurnId: number | null = null): DialogueNode =>
  ({ id, kind, name: `n${id}`, parentId, forkTurnId, turns: turns.map((t) => turn(t)), createdAt: String(++ids) });

// main 1: turns 10 11 12; A (2) forks at 11 with 20 21; B (3) forks at 20; C (4) forks at 11 too
const tree = [node(1, 'main', [10, 11, 12]), node(2, 'child', [20, 21], 1, 11), node(3, 'divergent', [30], 2, 20), node(4, 'branch', [40], 1, 11)];

describe('pathTo', () => {
  it('runs from the main node down to the node', () => {
    expect(pathTo(tree, 3).map((n) => n.id)).toEqual([1, 2, 3]);
    expect(pathTo(tree, 1).map((n) => n.id)).toEqual([1]);
  });
});

describe('forksAt', () => {
  it('finds the nodes opened on a turn, in the order they were made', () => {
    expect(forksAt(tree, 11).map((n) => n.id)).toEqual([2, 4]);
    expect(forksAt(tree, 12)).toEqual([]);
  });
});

describe('seenBy', () => {
  it('is the chain up to the fork: parents cut where their child forked', () => {
    expect(seenBy(tree, 20).map((t) => t.id)).toEqual([10, 11, 20]);
    expect(seenBy(tree, 12).map((t) => t.id)).toEqual([10, 11, 12]);
  });
});

describe('restack', () => {
  const live = (l: Layer[]) => l.filter((x) => x.phase !== 'leave').map((x) => x.key);
  it('pushes a new layer entering from the right, then settles it', () => {
    const next = restack([{ key: '1', phase: 'in' }], ['1', '2']);
    expect(next).toEqual([{ key: '1', phase: 'in' }, { key: '2', phase: 'enter' }]);
    expect(settle(next)).toEqual([{ key: '1', phase: 'in' }, { key: '2', phase: 'in' }]);
  });
  it('pops the top layer, which leaves before it goes', () => {
    const next = restack([{ key: '1', phase: 'in' }, { key: '2', phase: 'in' }], ['1']);
    expect(live(next)).toEqual(['1']);
    expect(next.find((l) => l.key === '2')?.phase).toBe('leave');
    expect(prune(next)).toEqual([{ key: '1', phase: 'in' }]);
  });
  it('swaps a sibling: the old top leaves as the new one enters', () => {
    const next = restack([{ key: '1', phase: 'in' }, { key: '2', phase: 'in' }], ['1', '4']);
    expect(live(next)).toEqual(['1', '4']);
    expect(next.map((l) => `${l.key}:${l.phase}`)).toEqual(['1:in', '4:enter', '2:leave']);
  });
  it('brings back a layer that was leaving', () => {
    const next = restack([{ key: '1', phase: 'in' }, { key: '2', phase: 'leave' }], ['1', '2']);
    expect(live(next)).toEqual(['1', '2']);
  });
});

describe('spine', () => {
  it('leaves a spine per covered layer, at most three, older ones off the left', () => {
    // five live layers: the top sits right of three spines
    expect(spine(4, 5, 56, 3)).toEqual({ left: 168, covered: false, gone: false });
    expect(spine(3, 5, 56, 3)).toEqual({ left: 112, covered: true, gone: false });
    expect(spine(1, 5, 56, 3)).toEqual({ left: 0, covered: true, gone: false });
    expect(spine(0, 5, 56, 3)).toEqual({ left: 0, covered: true, gone: true });
  });
  it('puts a lone layer at the left edge', () => {
    expect(spine(0, 1, 56, 3)).toEqual({ left: 0, covered: false, gone: false });
  });
});

describe('mapLayout', () => {
  it('lays the tree top down: a column per leaf, parents over their children', () => {
    const m = mapLayout(tree, { col: 60, row: 62, top: 20, left: 30 });
    const at = (id: number) => m.at.get(id)!;
    // leaves left to right: B (under A), C
    expect(at(3)).toMatchObject({ x: 30, y: 20 + 2 * 62 });
    expect(at(4)).toMatchObject({ x: 90, y: 20 + 62 });
    expect(at(2).x).toBe(30);
    expect(at(1)).toMatchObject({ x: 60, y: 20 });
    expect(at(2).fork).toBe(2); // A forked at the main node's 2nd turn
    expect(m.width).toBe(30 + 2 * 60);
  });
});

describe('stepRun', () => {
  it('collects the reply, resets a draft, and tracks lookups', () => {
    let s = stepRun(undefined, { type: 'delta', text: '草稿' });
    s = stepRun(s, { type: 'delta', text: '', reset: true });
    s = stepRun(s, { type: 'activity', id: 't1', kind: 'search_course', status: 'running' });
    s = stepRun(s, { type: 'delta', text: '答' });
    s = stepRun(s, { type: 'delta', text: '复' });
    expect(s.text).toBe('答复');
    expect(s.activities).toHaveLength(1);
  });
  it('notes thinking while the model reasons', () => {
    const s = stepRun(undefined, { type: 'activity', id: 'thinking-1', kind: 'thinking', status: 'running' });
    expect(s.thinking).toBe(true);
    expect(s.activities).toHaveLength(0);
  });
  it('notes linking to the course after the answer, which is no lookup', () => {
    let s = stepRun(undefined, { type: 'delta', text: '答复' });
    s = stepRun(s, { type: 'activity', id: 'link_course-1', kind: 'link_course', status: 'running' });
    expect(s.linking).toBe(true);
    expect(s.activities).toHaveLength(0);
    s = stepRun(s, { type: 'activity', id: 'link_course-1', kind: 'link_course', status: 'done' });
    expect(s.linking).toBe(false);
    expect(s.text).toBe('答复');
  });
});

describe('courseLinkItems', () => {
  const lesson = { id: 7, key: 'l05', title: '05｜动态数据传输' };
  it('links each to its lesson and section in the reader', () => {
    expect(courseLinkItems(3, [{ label: 'Nagle 算法', note: '小数据包合并发送就是这里讲的做法。', lesson, section: { id: 'nagle-算法', title: 'Nagle 算法' } }])).toEqual([
      { href: '/courses/3/lessons/7#nagle-%E7%AE%97%E6%B3%95', place: '05｜动态数据传输 › Nagle 算法', note: '小数据包合并发送就是这里讲的做法。' },
    ]);
  });
  it('links to the top of the lesson when the link is to no section', () => {
    expect(courseLinkItems(3, [{ label: '数据传输', note: '说明', lesson, section: null }])).toEqual([
      { href: '/courses/3/lessons/7', place: '05｜动态数据传输', note: '说明' },
    ]);
  });
  it('is nothing for a turn without links', () => {
    expect(courseLinkItems(3, undefined)).toEqual([]);
    expect(courseLinkItems(3, [])).toEqual([]);
  });
});

describe('carried', () => {
  it('names the background the main node brings along', () => {
    expect(carried(tree[0])).toBe('带上了：课程信息 · 选中的原文 · 分析');
  });
  it('is nothing on any other node', () => {
    expect(carried(tree[1])).toBeNull();
    expect(carried(tree[2])).toBeNull();
    expect(carried(tree[3])).toBeNull();
  });
});

describe('what the next question carries', () => {
  // the current node is A (2): turns 20, 21
  const A = tree[1];
  const none = noCarry;
  const sel = (turn: number, text: string) => ({ turn, text });
  const plainly = (text: string) => ({ text, attachments: [] });

  it('asks on in the current node, after its last turn, when it carries nothing', () => {
    expect(nextQuestion(A, tree, none, '为什么？')).toEqual({ to: 'turn', after: 21, question: plainly('为什么？') });
  });

  it('asks on in the current node with the selection above the question, when only a selection is carried', () => {
    const c = stepCarry(none, { type: 'select', selection: sel(20, '一段话') });
    expect(nextQuestion(A, tree, c, '这是什么意思？')).toEqual({
      to: 'turn', after: 21, question: { text: '这是什么意思？', attachments: [{ kind: 'selection', text: '一段话' }] },
    });
  });

  it('opens a node of the kind picked, from the turn marked, then the selection’s turn, then the last turn', () => {
    const kind = (c: Carry) => stepCarry(c, { type: 'kind', kind: 'branch' });
    expect(nextQuestion(A, tree, kind(none), '问')).toMatchObject({ to: 'node', kind: 'branch', fork: 21 });
    const selected = stepCarry(none, { type: 'select', selection: sel(20, '一段话') });
    expect(nextQuestion(A, tree, kind(selected), '问')).toMatchObject({ to: 'node', fork: 20 });
    const marked = stepCarry(selected, { type: 'fork', turn: 21 });
    expect(nextQuestion(A, tree, kind(marked), '问')).toMatchObject({ to: 'node', fork: 21 });
  });

  it('opens the node with the selection as part of its first question', () => {
    const c = stepCarry(stepCarry(none, { type: 'select', selection: sel(20, '一段话') }), { type: 'kind', kind: 'child' });
    expect(nextQuestion(A, tree, c, '细说')).toEqual({
      to: 'node', fork: 20, kind: 'child', question: { text: '细说', attachments: [{ kind: 'selection', text: '一段话' }] },
    });
  });

  it('will not send with a turn marked to fork from but no kind picked', () => {
    const c = stepCarry(none, { type: 'fork', turn: 20 });
    expect(nextQuestion(A, tree, c, '问')).toEqual({ to: 'none', reason: '先选节点类型' });
    // said before anything is typed, where the box shows it
    expect(nextQuestion(A, tree, c, '')).toEqual({ to: 'none', reason: '先选节点类型' });
  });

  it('carries one selection at a time: another takes its place', () => {
    const c = stepCarry(stepCarry(none, { type: 'select', selection: sel(20, '第一段') }), { type: 'select', selection: sel(21, '第二段') });
    expect(c.selection).toEqual(sel(21, '第二段'));
    expect(nextQuestion(A, tree, c, '问')).toMatchObject({ question: { attachments: [{ kind: 'selection', text: '第二段' }] } });
  });

  it('opens no node once the dialogue has MAX_NODES nodes, and says why', () => {
    const many = [...tree, ...Array.from({ length: MAX_NODES - tree.length }, (_, i) => node(100 + i, 'child', [], 1, 10))];
    const c = stepCarry(none, { type: 'kind', kind: 'child' });
    expect(nextQuestion(A, many, c, '问')).toEqual({ to: 'none', reason: `已有 ${MAX_NODES} 个节点，删除一些后才能再开` });
    // asking on in the node still works
    expect(nextQuestion(A, many, none, '问')).toMatchObject({ to: 'turn' });
  });

  it('has nothing to send without words or a selection', () => {
    expect(nextQuestion(A, tree, none, '  ')).toEqual({ to: 'none', reason: '' });
    const c = stepCarry(stepCarry(none, { type: 'select', selection: sel(20, '一段话') }), { type: 'kind', kind: 'child' });
    expect(nextQuestion(A, tree, c, '')).toMatchObject({ to: 'node', question: { text: '' } });
  });

  it('asks on in no node whose last turn failed until it is retried', () => {
    const failed = { ...A, turns: [A.turns[0], { ...A.turns[1], meta: { ...A.turns[1].meta, interrupted: 'failed' as const } }] };
    expect(nextQuestion(failed, tree, none, '问')).toEqual({ to: 'none', reason: '重试这一轮后才能接着问' });
    expect(nextQuestion(failed, tree, none, '')).toEqual({ to: 'none', reason: '重试这一轮后才能接着问' });
  });

  it('drops what it carried once the question is sent, and the fork mark with the kind taken off', () => {
    const c = stepCarry(stepCarry(stepCarry(none, { type: 'select', selection: sel(20, '一段话') }), { type: 'fork', turn: 21 }), { type: 'kind', kind: 'child' });
    expect(stepCarry(c, { type: 'sent' })).toEqual(none);
    expect(stepCarry(c, { type: 'unkind' })).toEqual({ ...none, selection: sel(20, '一段话') });
  });

  // a wide screen's box: the quotes sit in the words (see quotes.ts)
  const quoted = toQuestion(['比较', { text: '进程', turn: 20, start: 4, end: 6 }, '和', { text: '线程', turn: 21, start: 0, end: 2 }]);

  it('asks on with the quotes in the words as they were put', () => {
    expect(nextQuestion(A, tree, none, quoted)).toEqual({ to: 'turn', after: 21, question: quoted });
  });

  it('opens a node from the first quote\'s turn unless a turn is marked', () => {
    const kind = stepCarry(none, { type: 'kind', kind: 'branch' });
    expect(nextQuestion(A, tree, kind, quoted)).toMatchObject({ to: 'node', fork: 20, question: quoted });
    expect(nextQuestion(A, tree, stepCarry(kind, { type: 'fork', turn: 21 }), quoted)).toMatchObject({ fork: 21 });
  });

  it('sends quotes alone, with no words', () => {
    const alone = toQuestion([{ text: '进程', turn: 20, start: 4, end: 6 }]);
    expect(nextQuestion(A, tree, none, alone)).toMatchObject({ to: 'turn', question: alone });
    expect(nextQuestion(A, tree, none, toQuestion(['  ']))).toEqual({ to: 'none', reason: '' });
  });

  it(`sends no more than ${MAX_QUOTES} quotes, and says why`, () => {
    const many = toQuestion(Array.from({ length: MAX_QUOTES + 1 }, (_, i) => ({ text: `q${i}`, turn: 20, start: i, end: i + 1 })));
    expect(nextQuestion(A, tree, none, many)).toEqual({ to: 'none', reason: `一个问题最多引用 ${MAX_QUOTES} 段` });
  });
});

describe('withoutNode', () => {
  it('drops the node and every node under it, keeping the rest', () => {
    expect(withoutNode(tree, 2).map((n) => n.id)).toEqual([1, 4]);
    expect(withoutNode(tree, 3).map((n) => n.id)).toEqual([1, 2, 4]);
  });
});

describe('rekey', () => {
  it('turns the node being opened into the node kept, in place and as it was', () => {
    const l: Layer[] = [{ key: '1', phase: 'in' }, { key: 'opening', phase: 'enter' }];
    expect(rekey(l, 'opening', '5')).toEqual([{ key: '1', phase: 'in' }, { key: '5', phase: 'enter' }]);
    // the stack wanted next keeps it rather than sliding it in again
    expect(restack(rekey(l, 'opening', '5'), ['1', '5'])).toEqual([{ key: '1', phase: 'in' }, { key: '5', phase: 'enter' }]);
  });

  it('leaves the layers alone when none has the key', () => {
    const l: Layer[] = [{ key: '1', phase: 'in' }];
    expect(rekey(l, 'opening', '5')).toEqual(l);
  });
});

describe('atEnd', () => {
  it('is at the end within a few pixels of the bottom', () => {
    expect(atEnd({ scrollTop: 600, scrollHeight: 1000, clientHeight: 400 })).toBe(true);
    expect(atEnd({ scrollTop: 560, scrollHeight: 1000, clientHeight: 400 })).toBe(true);
    expect(atEnd({ scrollTop: 559, scrollHeight: 1000, clientHeight: 400 })).toBe(false);
  });

  it('is not once the reader has scrolled up', () => {
    expect(atEnd({ scrollTop: 400, scrollHeight: 1000, clientHeight: 400 })).toBe(false);
  });
});

describe('openingName', () => {
  it('names a node being opened after its kind until it is kept', () => {
    expect(openingName('branch')).toBe('新分支节点');
  });
});

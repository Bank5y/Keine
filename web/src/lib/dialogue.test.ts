import { describe, expect, it } from 'vitest';
import { carried, courseLinkItems, forksAt, mapLayout, pathTo, prune, restack, seenBy, settle, spine, stepRun, type Layer } from './dialogue';
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
    expect(courseLinkItems(3, [{ concept: 'Nagle 算法', note: '小数据包合并发送就是这里讲的做法。', lesson, section: { id: 'nagle-算法', title: 'Nagle 算法' } }])).toEqual([
      { href: '/courses/3/lessons/7#nagle-%E7%AE%97%E6%B3%95', place: '05｜动态数据传输 › Nagle 算法', note: '小数据包合并发送就是这里讲的做法。' },
    ]);
  });
  it('links to the top of the lesson when the concept is in no section', () => {
    expect(courseLinkItems(3, [{ concept: '数据传输', note: '说明', lesson, section: null }])).toEqual([
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

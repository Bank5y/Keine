import { describe, expect, it } from 'vitest';
import { citationTable, splitCitations, type Cited } from './citations';
import type { DialogueNode, NodeKind } from './explorations';

const src = (n: number, url: string, title = `资料${n}`) => ({ n, url, title, kind: 'search' });
const turn = (id: number, sources: unknown[] = []) =>
  ({ id, parentId: null, question: { text: '', attachments: [] }, reply: '', meta: { source: 'site' as const, model: 'm', sources }, createdAt: '' });
const node = (id: number, kind: NodeKind, turns: ReturnType<typeof turn>[], parentId: number | null = null, forkTurnId: number | null = null): DialogueNode =>
  ({ id, kind, name: `n${id}`, parentId, forkTurnId, turns, createdAt: '' });

// main: 10 (the analysis, no numbers) 11 (finds 1, 2); a sibling node forks
// at 10 and finds 3; a child node forks at 11 and finds 4
const tree = [
  node(1, 'main', [turn(10, [{ title: '旧', url: 'https://old.example/x', kind: 'search' }]), turn(11, [src(1, 'https://www.raft.github.io/raft.pdf', 'Raft 论文'), src(2, 'https://example.com/term')])]),
  node(2, 'divergent', [turn(20, [src(3, 'https://lamport.org/paxos.pdf')])], 1, 10),
  node(3, 'child', [turn(30, [src(4, 'https://arxiv.org/abs/1', 'A paper'), src(1, 'https://www.raft.github.io/raft.pdf', 'Raft 论文')])], 1, 11),
];

describe('citationTable', () => {
  it('holds what the chain up to the turn found, by number', () => {
    const t = citationTable(tree, 30);
    expect([...t.keys()].sort()).toEqual([1, 2, 4]);
    expect(t.get(1)).toEqual({ n: 1, url: 'https://www.raft.github.io/raft.pdf', title: 'Raft 论文', host: 'raft.github.io' });
  });

  it("leaves out a sibling's sources and sources kept without a number", () => {
    expect([...citationTable(tree, 20).keys()]).toEqual([3]);
    expect([...citationTable(tree, 10).keys()]).toEqual([]);
  });
});

describe('splitCitations', () => {
  const table = new Map<number, Cited>([
    [1, { n: 1, url: 'https://raft.github.io/raft.pdf', title: 'Raft 论文', host: 'raft.github.io' }],
    [2, { n: 2, url: 'https://example.com/term', title: '任期', host: 'example.com' }],
  ]);
  const one = table.get(1)!, two = table.get(2)!;

  it('turns a number into its source', () => {
    expect(splitCitations('Raft 的论文 [1]。', table)).toEqual(['Raft 的论文 ', one, '。']);
  });

  it('reads numbers side by side and comma separated', () => {
    expect(splitCitations('见 [1][2]，或 [2, 1]', table)).toEqual(['见 ', one, two, '，或 ', two, one]);
  });

  it('drops a number it does not know, and the space before it', () => {
    expect(splitCitations('Paxos 的论文 [9]。', table)).toEqual(['Paxos 的论文。']);
    expect(splitCitations('见 [1, 9]', table)).toEqual(['见 ', one]);
  });

  it('leaves text without citations alone', () => {
    expect(splitCitations('数组 a[i] 和 [链接]', table)).toEqual(['数组 a[i] 和 [链接]']);
  });

  it('reads an index after a name as code, not a citation', () => {
    expect(splitCitations('取 a[1] 和 f(x)[2]', table)).toEqual(['取 a[1] 和 f(x)[2]']);
    expect(splitCitations('论文[1]', table)).toEqual(['论文', one]);
  });
});

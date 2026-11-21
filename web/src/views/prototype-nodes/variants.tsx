// PROTOTYPE (#137): four ways to show and navigate an exploration's nodes
// inside the level-2 zoom, one per paradigm in docs/research/branch-rendering.md.
import { useEffect, useRef, useState } from 'react';
import { TYPES, type DNode, addNode, byId, childrenOf, forksAt, inherited, path, removeNode, turnIndex } from './data';
import { Composer, Draft, ForkHead, type Fork, Question, Reply, Turns } from './parts';
import { TypeDot, Zoom } from './shell';

export interface Store {
  nodes: DNode[]; current: string; open: (id: string) => void;
  fork?: Fork; setFork: (f?: Fork) => void;
  start: (q: string, fragment?: string) => void; remove: (id: string) => void;
}

export function useStore(initial: DNode[]): Store {
  const [nodes, setNodes] = useState(initial);
  const [current, open] = useState('main');
  const [fork, setFork] = useState<Fork>();
  return {
    nodes, current, open: (id) => { setFork(undefined); open(id); }, fork, setFork,
    start: (q, fragment) => {
      if (!fork) return;
      const parent = turnIndex(nodes, fork.turn)!.node.id;
      const next = addNode(nodes, fork.type, parent, fork.turn, q, fragment);
      setNodes(next); setFork(undefined); open(next[next.length - 1].id);
    },
    remove: (id) => {
      const n = byId(nodes, id);
      const kids = removeNode(nodes, id);
      if (!confirm(`删除节点「${n.name}」？它下面的 ${nodes.length - kids.length - 1} 个节点会一起删除。`)) return;
      setNodes(kids); if (!kids.some((k) => k.id === current)) open(n.parent!);
    },
  };
}

const count = (nodes: DNode[]) => <span style={{ fontSize: 12, color: 'var(--text-3)', border: '1px solid var(--line)', borderRadius: 99, padding: '1px 10px', marginRight: 8 }}>节点 {nodes.length - 1}</span>;

// the current node's conversation in one column, with its fork head
function Column({ s, chips, children }: { s: Store; chips?: boolean; children?: React.ReactNode }) {
  const node = byId(s.nodes, s.current);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => { ref.current?.scrollTo({ top: 0 }); }, [s.current]);
  return (
    <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', padding: '0 40px' }}>
      {children}
      <div ref={ref} style={{ flex: 1, overflow: 'auto', paddingTop: 18 }}>
        {s.fork ? <Draft fork={s.fork} nodes={s.nodes} onCancel={() => s.setFork(undefined)} onStart={s.start} /> : (
          <>
            <ForkHead nodes={s.nodes} id={s.current} onOpen={s.open} />
            <Turns node={node} nodes={s.nodes} onFork={s.setFork} chips={chips} onOpen={s.open} />
          </>
        )}
      </div>
      {s.fork ? null : <Composer />}
    </div>
  );
}

// ---------- A: outline tree (pi / Loom) ----------
// the main node's turns as rows; nodes hang under the turn they forked from;
// indent only where the tree really forks; current path marked.
export function VariantA({ s }: { s: Store }) {
  const onPath = new Set(path(s.nodes, s.current).map((n) => n.id));
  const [folded, setFolded] = useState<Set<string>>(new Set());
  const toggle = (id: string) => setFolded((f) => { const g = new Set(f); if (g.has(id)) g.delete(id); else g.add(id); return g; });
  const Row = ({ n, d, at }: { n: DNode; d: number; at?: number }) => {
    const cur = n.id === s.current; const kids = childrenOf(s.nodes, n.id);
    return (
      <div style={{ borderLeft: d ? `1px solid ${onPath.has(n.id) ? TYPES[n.type].color : 'var(--line)'}` : 0, marginLeft: d ? 6 : 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '5px 6px', paddingLeft: d ? 6 : 2, borderRadius: 4, cursor: 'pointer', background: cur ? TYPES[n.type].soft : 'transparent', fontWeight: cur ? 600 : 400, fontSize: 13 }}
          onClick={() => s.open(n.id)}>
          <span onClick={(e) => { e.stopPropagation(); if (kids.length) toggle(n.id); }} style={{ width: 10, flex: 'none', color: 'var(--text-3)', fontSize: 9 }}>{kids.length ? (folded.has(n.id) ? '▸' : '▾') : ''}</span>
          <TypeDot type={n.type} />
          {at ? <span style={{ flex: 'none', fontSize: 10, color: 'var(--text-3)', fontWeight: 400 }}>{at}</span> : null}
          <span style={{ flex: 1, minWidth: 40, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: onPath.has(n.id) ? 'var(--text)' : 'var(--text-2)' }}>{n.name}</span>
          <span style={{ flex: 'none', fontSize: 11, color: 'var(--text-3)' }}>{n.turns.length} 轮</span>
          {n.type !== 'main' ? <span onClick={(e) => { e.stopPropagation(); s.remove(n.id); }} title="删除节点" style={{ flex: 'none', fontSize: 11, color: 'var(--text-3)', padding: '0 2px' }}>⋯</span> : null}
        </div>
        {folded.has(n.id) ? null : (
          <div style={{ paddingLeft: 6 }}>
            {n.turns.flatMap((t, i) => forksAt(s.nodes, t.id).map((f) => <Row key={f.id} n={f} d={d + 1} at={i + 1} />))}
          </div>
        )}
      </div>
    );
  };
  return (
    <Zoom right={count(s.nodes)}>
      <nav style={{ width: 240, flex: 'none', background: 'var(--bg)', borderRight: '1px solid var(--line)', overflow: 'auto', padding: '14px 8px' }}>
        <div style={{ fontSize: 12, color: 'var(--text-3)', padding: '0 8px 8px' }}>节点 · 行首数字是岔出的那一轮</div>
        <Row n={byId(s.nodes, 'main')} d={0} />
      </nav>
      <Column s={s}>
        <Crumbs s={s} />
      </Column>
    </Zoom>
  );
}

function Crumbs({ s }: { s: Store }) {
  const p = path(s.nodes, s.current);
  if (p.length < 2) return null;
  return (
    <div style={{ display: 'flex', gap: 6, alignItems: 'center', fontSize: 12, padding: '10px 0', borderBottom: '1px solid var(--line)', flexWrap: 'wrap' }}>
      {p.map((n, i) => (
        <span key={n.id} style={{ display: 'inline-flex', gap: 6, alignItems: 'center' }}>
          {i ? <span style={{ color: 'var(--text-3)' }}>›</span> : null}
          <button type="button" onClick={() => s.open(n.id)} style={{ border: 0, background: 'none', cursor: 'pointer', color: i === p.length - 1 ? 'var(--text)' : '#4b6a9b', fontWeight: i === p.length - 1 ? 600 : 400, display: 'inline-flex', gap: 5, alignItems: 'center', padding: 0, font: 'inherit' }}>
            <TypeDot type={n.type} size={6} />{n.name}
          </button>
        </span>
      ))}
    </div>
  );
}

// ---------- B: thread side panel (Slack / LobeChat) ----------
// the parent's conversation stays on the left; the node opens beside it,
// scrolled to the fork turn; nesting replaces the panel, with a back link.
export function VariantB({ s }: { s: Store }) {
  const node = byId(s.nodes, s.current);
  const left = node.type === 'main' ? node : byId(s.nodes, node.parent!);
  const leftRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (node.forkTurn) document.getElementById(`turn-${node.forkTurn}`)?.scrollIntoView({ block: 'center', behavior: 'smooth' });
  }, [node.forkTurn]);
  const panel = node.type !== 'main' || s.fork;
  return (
    <Zoom right={count(s.nodes)}>
      <div ref={leftRef} style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', padding: '0 32px', borderRight: panel ? '1px solid var(--line)' : 0 }}>
        <div style={{ fontSize: 12, color: 'var(--text-3)', padding: '12px 0 0' }}>
          {left.type === 'main' ? '主节点' : <>
            <button type="button" onClick={() => s.open(left.parent!)} style={{ border: 0, background: 'none', color: '#4b6a9b', cursor: 'pointer', padding: 0, font: 'inherit' }}>← {byId(s.nodes, left.parent!).name}</button>
            <span> · </span><TypeDot type={left.type} size={6} /> {left.name}</>}
        </div>
        <div style={{ flex: 1, overflow: 'auto' }}>
          {left.turns.map((t, i) => {
            const hot = node.forkTurn === t.id;
            return (
              <div key={t.id} id={`turn-${t.id}`} style={{ opacity: panel && !hot && node.forkTurn ? 0.55 : 1, background: hot ? '#f6f2eb' : 'transparent', borderRadius: 6, padding: hot ? '0 10px' : 0, margin: hot ? '0 -10px' : 0 }}>
                {left.type === 'main' && i === 0 ? <div style={{ fontSize: 11, color: 'var(--text-3)', marginTop: 12 }}>第 1 轮 · 分析</div> : <Question t={t} type={left.type} />}
                <Reply t={t} nodes={s.nodes} onFork={s.setFork} chips onOpen={s.open} compact={!!panel} />
              </div>
            );
          })}
        </div>
        {panel ? null : <Composer />}
      </div>
      {panel ? (
        <aside style={{ width: '46%', flex: 'none', display: 'flex', flexDirection: 'column', padding: '0 28px', background: 'var(--surface)' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '12px 0 8px', borderBottom: '1px solid var(--line)', fontSize: 13 }}>
            {s.fork ? <b>新{TYPES[s.fork.type].label}</b> : <><TypeDot type={node.type} /><b style={{ flex: 1 }}>{node.name}</b>
              <span onClick={() => s.remove(node.id)} style={{ fontSize: 12, color: 'var(--text-3)', cursor: 'pointer' }}>删除</span></>}
            <span style={{ flex: s.fork ? 1 : 0 }} />
            <span onClick={() => s.fork ? s.setFork(undefined) : s.open(node.parent!)} style={{ cursor: 'pointer', color: 'var(--text-3)' }}>×</span>
          </div>
          <div style={{ flex: 1, overflow: 'auto' }}>
            {s.fork ? <Draft fork={s.fork} nodes={s.nodes} onCancel={() => s.setFork(undefined)} onStart={s.start} /> : (
              <>
                <div style={{ fontSize: 11, color: 'var(--text-3)', margin: '12px 0 0' }}>{TYPES[node.type].label} · 继承左边到高亮那一轮为止</div>
                <Turns node={node} nodes={s.nodes} onFork={s.setFork} chips onOpen={s.open} compact />
              </>
            )}
          </div>
          {s.fork ? null : <Composer />}
        </aside>
      ) : null}
    </Zoom>
  );
}

// ---------- C: inline markers, no tree (SillyTavern flags / reply counts) ----------
// one full-width column; nodes show as chips under the turn they forked from;
// a breadcrumb with each step's siblings is the only way up.
export function VariantC({ s }: { s: Store }) {
  const p = path(s.nodes, s.current);
  const [sib, setSib] = useState<string>();
  return (
    <Zoom right={count(s.nodes)}>
      <Column s={s} chips>
        <div style={{ display: 'flex', gap: 4, alignItems: 'center', fontSize: 13, padding: '10px 0', borderBottom: '1px solid var(--line)', position: 'relative' }}>
          {p.map((n, i) => {
            const sibs = n.parent ? childrenOf(s.nodes, n.parent) : [];
            return (
              <span key={n.id} style={{ display: 'inline-flex', alignItems: 'center', gap: 4, position: 'relative' }}>
                {i ? <span style={{ color: 'var(--text-3)' }}>›</span> : null}
                <button type="button" onClick={() => s.open(n.id)} style={{ border: 0, background: i === p.length - 1 ? TYPES[n.type].soft : 'none', borderRadius: 4, padding: '2px 8px', cursor: 'pointer', display: 'inline-flex', gap: 6, alignItems: 'center', font: 'inherit', fontWeight: i === p.length - 1 ? 600 : 400 }}>
                  <TypeDot type={n.type} size={7} />{n.name}
                </button>
                {sibs.length > 1 ? <button type="button" onClick={() => setSib(sib === n.id ? undefined : n.id)} style={{ border: 0, background: 'none', cursor: 'pointer', color: 'var(--text-3)', fontSize: 10 }}>▾</button> : null}
                {sib === n.id ? (
                  <div style={{ position: 'absolute', top: '100%', left: 0, zIndex: 30, background: '#fff', border: '1px solid var(--line)', borderRadius: 6, boxShadow: '0 8px 24px rgb(0 0 0 / .12)', padding: 4, minWidth: 200 }}>
                    {sibs.map((x) => <div key={x.id} onClick={() => { setSib(undefined); s.open(x.id); }} style={{ padding: '6px 8px', cursor: 'pointer', display: 'flex', gap: 6, alignItems: 'center', fontSize: 13 }}><TypeDot type={x.type} />{x.name}</div>)}
                  </div>
                ) : null}
              </span>
            );
          })}
          <span style={{ flex: 1 }} />
          {p.length > 1 ? <span onClick={() => s.remove(s.current)} style={{ fontSize: 12, color: 'var(--text-3)', cursor: 'pointer' }}>删除节点</span> : null}
        </div>
      </Column>
    </Zoom>
  );
}

// ---------- D: canvas (obsidian-chat-stream / Open WebUI overview) ----------
// the nodes drawn as a graph that fills the card; the chosen node's
// conversation slides in from the right.
export function VariantD({ s }: { s: Store }) {
  const [shown, setShown] = useState(false);
  const pos = layout(s.nodes);
  const W = Math.max(...Object.values(pos).map((p) => p.x)) + 200;
  useEffect(() => { document.getElementById(`card-${s.current}`)?.scrollIntoView({ block: 'nearest', inline: 'center', behavior: 'smooth' }); }, [s.current, shown]);
  const H = Math.max(...Object.values(pos).map((p) => p.y)) + 100;
  const onPath = new Set(path(s.nodes, s.current).map((n) => n.id));
  return (
    <Zoom right={count(s.nodes)}>
      <div style={{ flex: 1, overflow: 'auto', background: 'radial-gradient(circle, #e2dace 1px, transparent 1px) 0 0 / 18px 18px', position: 'relative' }}>
        <svg width={W} height={H} style={{ position: 'absolute', left: 0, top: 0 }}>
          {s.nodes.filter((n) => n.parent).map((n) => {
            const a = pos[n.parent!], b = pos[n.id];
            const ty = a.y + 26 + (pos[n.parent!].turnY[n.forkTurn!] ?? 0);
            return <path key={n.id} d={`M${a.x + 160},${ty} C${a.x + 180},${ty} ${b.x - 30},${b.y + 26} ${b.x},${b.y + 26}`} fill="none" stroke={TYPES[n.type].color} strokeWidth={onPath.has(n.id) ? 2.5 : 1.5} strokeDasharray={n.type === 'divergent' ? '5 4' : undefined} />;
          })}
        </svg>
        {s.nodes.map((n) => {
          const p = pos[n.id]; const cur = n.id === s.current;
          return (
            <div key={n.id} id={`card-${n.id}`} onClick={() => { s.open(n.id); setShown(true); }}
              style={{ position: 'absolute', left: p.x, top: p.y, width: 160, background: '#fff', border: `1.5px solid ${cur ? TYPES[n.type].color : 'var(--line)'}`, borderRadius: 8, padding: '8px 10px', cursor: 'pointer', boxShadow: cur ? '0 6px 18px rgb(0 0 0 / .12)' : 'none' }}>
              <div style={{ display: 'flex', gap: 6, alignItems: 'center', fontSize: 13, fontWeight: 600 }}><TypeDot type={n.type} /><span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{n.name}</span></div>
              {n.turns.map((t, i) => (
                <div key={t.id} style={{ fontSize: 11, color: 'var(--text-3)', marginTop: 3, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{i + 1}. {t.q}</div>
              ))}
            </div>
          );
        })}
      </div>
      {shown || s.fork ? (
        <div style={{ width: '48%', flex: 'none', borderLeft: '1px solid var(--line)', background: 'var(--surface)', display: 'flex', boxShadow: '-12px 0 30px rgb(0 0 0 / .08)' }}>
          <Column s={s}>
            <div style={{ display: 'flex', padding: '10px 0', fontSize: 12, color: 'var(--text-3)' }}>
              <span style={{ flex: 1 }}>{byId(s.nodes, s.current).name}</span>
              {s.current !== 'main' ? <span onClick={() => s.remove(s.current)} style={{ cursor: 'pointer', marginRight: 12 }}>删除节点</span> : null}
              <span onClick={() => { setShown(false); s.setFork(undefined); }} style={{ cursor: 'pointer' }}>收起 ›</span>
            </div>
          </Column>
        </div>
      ) : null}
    </Zoom>
  );
}

// a left-to-right tree: depth is the column, rows stack by subtree
function layout(nodes: DNode[]) {
  const out: Record<string, { x: number; y: number; turnY: Record<string, number> }> = {};
  let row = 0;
  const place = (n: DNode, d: number) => {
    const turnY: Record<string, number> = {};
    n.turns.forEach((t, i) => { turnY[t.id] = 12 + i * 17; });
    out[n.id] = { x: 30 + d * 200, y: 30 + row * 0, turnY };
    const start = row;
    const kids = n.turns.flatMap((t) => forksAt(nodes, t.id));
    if (!kids.length) row += 1;
    kids.forEach((k) => place(k, d + 1));
    out[n.id].y = 30 + start * (64 + 26) ;
  };
  place(byId(nodes, 'main'), 0);
  // keep inherited() referenced for readers comparing variants
  void inherited;
  return out;
}

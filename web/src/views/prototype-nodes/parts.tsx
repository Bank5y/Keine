// PROTOTYPE (#137): pieces every variant uses — a turn, the fork menu, the
// first-question composer, the fork-point header. Layout stays per variant.
import { useState } from 'react';
import { DEFAULT_Q, TYPES, type DNode, type NodeType, type Turn, forksAt, inherited, path, turnIndex } from './data';
import { TypeDot } from './shell';

export type Fork = { type: Exclude<NodeType, 'main'>; turn: string; fragment?: string };

const md = (s: string) => s.split('\n\n').map((p, i) => <p key={i} style={{ margin: '0 0 10px' }} dangerouslySetInnerHTML={{ __html: p.replace(/\*\*(.+?)\*\*/g, '<b>$1</b>') }} />);

export function Question({ t, type }: { t: Turn; type: NodeType }) {
  return (
    <div style={{ display: 'flex', justifyContent: 'flex-end', margin: '18px 0 12px' }}>
      <div style={{ maxWidth: '78%', background: 'var(--surface-2)', borderRadius: 10, padding: '9px 14px', fontSize: 14, lineHeight: 1.6 }}>
        {t.fragment ? (
          <div style={{ borderLeft: `3px solid ${TYPES[type].color}`, background: 'var(--surface)', borderRadius: 4, padding: '4px 10px', marginBottom: 6, fontSize: 12, color: 'var(--text-2)' }}>
            <span style={{ color: 'var(--text-3)', marginRight: 8 }}>选中片段</span>「{t.fragment}」
          </div>
        ) : null}
        {t.q}
      </div>
    </div>
  );
}

// the reply, its fork button, and (optionally) chips for the nodes forked here
export function Reply({ t, nodes, onFork, chips, onOpen, compact }: {
  t: Turn; nodes: DNode[]; onFork: (f: Fork) => void; chips?: boolean; onOpen?: (id: string) => void; compact?: boolean;
}) {
  const [menu, setMenu] = useState(false);
  const [frag, setFrag] = useState<string>();
  const here = forksAt(nodes, t.id);
  return (
    <div style={{ position: 'relative', fontSize: compact ? 14 : 15, lineHeight: 1.8 }}
      onMouseUp={() => { const s = window.getSelection()?.toString().trim(); setFrag(s || undefined); }}>
      {md(t.a)}
      <div style={{ display: 'flex', gap: 6, alignItems: 'center', flexWrap: 'wrap', fontSize: 12, color: 'var(--text-3)' }}>
        <button type="button" onClick={() => setMenu((v) => !v)} style={{ ...ghost, background: menu ? '#e6ecf4' : 'transparent', color: menu ? '#4b6a9b' : 'var(--text-3)' }}>＋ 开节点</button>
        {chips ? here.map((n) => (
          <button key={n.id} type="button" onClick={() => onOpen?.(n.id)} style={{ ...ghost, border: `1px solid ${TYPES[n.type].color}55`, background: TYPES[n.type].soft, color: 'var(--text-2)', display: 'inline-flex', gap: 6, alignItems: 'center' }}>
            <TypeDot type={n.type} size={7} />{n.name}<span style={{ color: 'var(--text-3)' }}>{n.turns.length} 轮</span>
          </button>
        )) : null}
      </div>
      {menu ? (
        <div style={{ position: 'absolute', zIndex: 20, top: '100%', left: 0, width: 280, background: '#fff', border: '1px solid var(--line)', borderRadius: 8, boxShadow: '0 8px 24px rgb(0 0 0 / .14)', padding: 6 }}>
          {(['child', 'branch', 'divergent'] as const).map((k) => (
            <button key={k} type="button" onClick={() => { setMenu(false); onFork({ type: k, turn: t.id, fragment: frag }); }}
              style={{ display: 'flex', gap: 10, alignItems: 'flex-start', width: '100%', textAlign: 'left', padding: '8px 10px', border: 0, background: 'transparent', borderRadius: 6, cursor: 'pointer' }}>
              <span style={{ marginTop: 6 }}><TypeDot type={k} /></span>
              <span><b style={{ fontSize: 14, fontWeight: 500 }}>{TYPES[k].label}</b><br /><span style={{ fontSize: 12, color: 'var(--text-3)' }}>{TYPES[k].hint}</span></span>
            </button>
          ))}
          <div style={{ borderTop: '1px solid var(--line)', padding: '6px 10px 2px', fontSize: 12, color: 'var(--text-3)' }}>
            {frag ? `带上选中的「${frag.slice(0, 16)}」` : '可以先在回复里选中一段，作为重点'}
          </div>
        </div>
      ) : null}
    </div>
  );
}

const ghost: React.CSSProperties = { border: 0, borderRadius: 4, padding: '2px 8px', cursor: 'pointer', fontSize: 12, font: 'inherit' };

export function Turns({ node, nodes, onFork, chips, onOpen, compact }: {
  node: DNode; nodes: DNode[]; onFork: (f: Fork) => void; chips?: boolean; onOpen?: (id: string) => void; compact?: boolean;
}) {
  return (
    <>
      {node.turns.map((t, i) => (
        <div key={t.id} id={`turn-${t.id}`}>
          {node.type === 'main' && i === 0 ? <AnalysisCard /> : <Question t={t} type={node.type} />}
          {node.type === 'main' && i === 0 ? null : null}
          <Reply t={t} nodes={nodes} onFork={onFork} chips={chips} onOpen={onOpen} compact={compact} />
        </div>
      ))}
    </>
  );
}

function AnalysisCard() {
  return (
    <div style={{ margin: '8px 0 12px', fontSize: 11, color: 'var(--text-3)' }}>第 1 轮 · 分析</div>
  );
}

// the head of a node: where it forked, what it inherits (LobeChat's divider)
export function ForkHead({ nodes, id, onOpen }: { nodes: DNode[]; id: string; onOpen: (id: string) => void }) {
  const p = path(nodes, id);
  const n = p[p.length - 1];
  if (n.type === 'main') return null;
  const at = turnIndex(nodes, n.forkTurn!)!;
  const forkTurn = at.node.turns[at.i];
  const chain = inherited(nodes, id);
  const seen = chain.reduce((a, c) => a + c.turns.length, 0);
  return (
    <div style={{ marginBottom: 8 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
        <TypeDot type={n.type} size={10} />
        <b style={{ fontSize: 17 }}>{n.name}</b>
        <span style={{ fontSize: 12, color: 'var(--text-3)' }}>{TYPES[n.type].label} · 从{at.node.type === 'main' ? '主节点' : `「${at.node.name}」`}第 {at.i + 1} 轮岔出</span>
      </div>
      <button type="button" onClick={() => onOpen(at.node.id)} style={{ display: 'block', width: '100%', textAlign: 'left', border: '1px solid var(--line)', background: 'var(--bg)', borderRadius: 6, padding: '8px 12px', cursor: 'pointer', fontSize: 13, color: 'var(--text-2)' }}>
        <span style={{ color: 'var(--text-3)', fontSize: 11 }}>岔出的那一轮 ↩</span><br />
        {forkTurn.a.replace(/\*\*/g, '').slice(0, 70)}…
      </button>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, margin: '12px 0 4px', fontSize: 11, color: 'var(--text-3)' }}>
        <span style={{ flex: 1, height: 1, background: 'var(--line)' }} />
        继承到这一轮为止 · 共 {seen} 轮 · 之后的内容和兄弟节点不带进来
        <span style={{ flex: 1, height: 1, background: 'var(--line)' }} />
      </div>
    </div>
  );
}

// the first question of a new node: default text, editable, fragment removable
export function Draft({ fork, nodes, onCancel, onStart }: { fork: Fork; nodes: DNode[]; onCancel: () => void; onStart: (q: string, fragment?: string) => void }) {
  const [q, setQ] = useState(DEFAULT_Q[fork.type]);
  const [frag, setFrag] = useState(fork.fragment);
  const at = turnIndex(nodes, fork.turn)!;
  const c = TYPES[fork.type].color;
  return (
    <div style={{ padding: '20px 0' }}>
      <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 12 }}>
        <TypeDot type={fork.type} size={10} /><b style={{ fontSize: 17 }}>新{TYPES[fork.type].label}</b>
        <span style={{ fontSize: 13, color: 'var(--text-3)' }}>从{at.node.type === 'main' ? '主节点' : `「${at.node.name}」`}第 {at.i + 1} 轮岔出</span>
      </div>
      <div style={{ border: `1.5px solid ${c}`, borderRadius: 8, background: '#fff', padding: 12 }}>
        {frag ? (
          <div style={{ display: 'inline-flex', gap: 8, alignItems: 'center', borderLeft: `3px solid ${c}`, background: 'var(--surface)', border: '1px solid var(--line)', borderRadius: 6, padding: '4px 10px', fontSize: 12, marginBottom: 8 }}>
            <span style={{ color: 'var(--text-3)' }}>选中片段</span>「{frag}」
            <button type="button" onClick={() => setFrag(undefined)} style={{ border: 0, background: 'none', cursor: 'pointer', color: 'var(--text-3)' }}>×</button>
          </div>
        ) : null}
        <textarea value={q} onChange={(e) => setQ(e.target.value)} rows={3} style={{ width: '100%', border: 0, outline: 'none', resize: 'none', font: '15px/1.7 var(--sans)', background: 'transparent' }} />
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ flex: 1, fontSize: 12, color: 'var(--text-3)' }}>{q === DEFAULT_Q[fork.type] ? '默认提问，可以直接改' : '已修改，会作为这个节点的第一条提问'}</span>
          <button type="button" onClick={onCancel} style={btn('#fff', 'var(--text)')}>取消</button>
          <button type="button" onClick={() => onStart(q, frag)} style={btn(c, '#fff')}>开始</button>
        </div>
      </div>
      <div style={{ fontSize: 12, color: 'var(--text-3)', marginTop: 8 }}>节点类型开了以后不能改。</div>
    </div>
  );
}

const btn = (bg: string, fg: string): React.CSSProperties => ({ background: bg, color: fg, border: '1px solid var(--line)', borderRadius: 4, padding: '5px 16px', cursor: 'pointer', fontSize: 13 });

export function Composer() {
  return (
    <div style={{ flex: 'none', padding: '10px 0 12px' }}>
      <div style={{ border: '1px solid var(--rule)', borderRadius: 8, background: '#fff', padding: '10px 14px', display: 'flex', alignItems: 'center', color: 'var(--text-3)', fontSize: 14 }}>
        <span style={{ flex: 1 }}>接着问…</span>
        <span style={{ width: 28, height: 28, borderRadius: 99, background: '#4b6a9b', color: '#fff', display: 'grid', placeItems: 'center' }}>↑</span>
      </div>
      <div style={{ fontSize: 11, color: 'var(--text-3)', marginTop: 6 }}>AI 生成 · DeepSeek</div>
    </div>
  );
}

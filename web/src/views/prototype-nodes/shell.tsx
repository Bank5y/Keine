// PROTOTYPE (#137): the level-2 zoom every variant renders inside — a blurred
// reading page behind, the ~70% card, its header — plus a state readout.
import { TITLE, TYPES, type DNode, inherited, path } from './data';

export function Backdrop() {
  return (
    <div aria-hidden style={{ position: 'fixed', inset: 0, background: 'var(--bg)', filter: 'blur(8px)', padding: '90px 140px', color: 'var(--text-3)' }}>
      <div style={{ maxWidth: 620, margin: '0 auto', font: '17px/2 var(--sans)' }}>
        {Array.from({ length: 14 }, (_, i) => <p key={i}>下面这段代码展示了一个最小的实现。注意我们没有引入任何框架，所有的控制流都是显式的。更好的做法是把任务拆成几个职责清晰的步骤，每一步只做一件事。</p>)}
      </div>
    </div>
  );
}

export function Zoom({ children, right, head }: { children: React.ReactNode; right?: React.ReactNode; head?: React.ReactNode }) {
  return (
    <>
      <Backdrop />
      <div style={{ position: 'fixed', inset: 0, background: 'rgb(29 27 24 / .42)' }} />
      <div style={{
        position: 'fixed', left: '6vw', top: '5vh', width: '88vw', height: '90vh', minWidth: 900, minHeight: 560,
        background: 'var(--surface)', borderRadius: 8, boxShadow: '0 30px 80px rgb(29 27 24 / .35)',
        display: 'flex', flexDirection: 'column', overflow: 'hidden', color: 'var(--text)', fontFamily: 'var(--sans)',
      }}>
        <header style={{ height: 52, flex: 'none', display: 'flex', alignItems: 'center', gap: 8, padding: '0 20px 0 24px', borderBottom: '1px solid var(--line)' }}>
          <span style={{ fontWeight: 600, fontSize: 14 }}>展开探索</span>
          <span style={{ fontSize: 13, color: 'var(--text-3)' }}>· {TITLE}</span>
          <span style={{ flex: 1, minWidth: 0, display: 'flex', justifyContent: 'center' }}>{head}</span>
          {right}
          <span style={{ color: 'var(--text-3)', fontSize: 18, padding: '0 4px' }}>×</span>
        </header>
        <div style={{ flex: 1, minHeight: 0, display: 'flex' }}>{children}</div>
      </div>
    </>
  );
}

export function TypeDot({ type, size = 8 }: { type: DNode['type']; size?: number }) {
  return <span style={{ width: size, height: size, borderRadius: 99, background: TYPES[type].color, display: 'inline-block', flex: 'none' }} />;
}

// what the current node is and which turns it sees, so a variant's
// navigation can be checked against the model
export function StateReadout({ nodes, current }: { nodes: DNode[]; current: string }) {
  const chain = inherited(nodes, current);
  const p = path(nodes, current);
  return (
    <div style={{ position: 'fixed', left: 12, bottom: 12, zIndex: 9998, width: 260, padding: '8px 10px', borderRadius: 6, background: '#111d', color: '#eee', font: '11px/1.6 ui-monospace, monospace' }}>
      <div>current: {current} ({TYPES[p[p.length - 1].type].label})</div>
      <div>path: {p.map((n) => n.id).join(' › ')}</div>
      <div>sees: {chain.map((c) => `${c.node.id}[${c.turns.length}]`).join(' + ') || '—'} + own</div>
      <div>nodes: {nodes.length - 1} (excl. main)</div>
    </div>
  );
}

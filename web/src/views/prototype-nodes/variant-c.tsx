// PROTOTYPE (#137): variant C — inline markers, opened as a stack of drawers.
// The stack is the current node's path: clicking a node's chip pushes its
// drawer from the right, going back pops it. Underneath, at most three
// layers show as spines on the left; older ones slide off left and fade.
import { Menu } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { TYPES, type DNode, byId, childrenOf, path } from './data';
import { Composer, Draft, ForkHead, Turns } from './parts';
import type { Store } from './variants';
import { TypeDot, Zoom } from './shell';

const SPINE = 56; // the width of each covered layer left showing
const SHOWN = 3; // how many covered layers keep a spine
const MS = 340;

type Layer = { key: string; node?: DNode; draft?: boolean; phase: 'in' | 'enter' | 'leave' };

export function VariantC({ s }: { s: Store }) {
  const want = [...path(s.nodes, s.current).map((n) => n.id), ...(s.fork ? ['draft'] : [])];
  const [layers, setLayers] = useState<Layer[]>(() => want.map((k) => mk(k, s, 'in')));

  // diff the wanted stack against what is on screen: new layers enter from
  // the right, gone ones leave to the right before they unmount
  useEffect(() => {
    setLayers((cur) => {
      const keep = cur.filter((l) => want.includes(l.key) && l.phase !== 'leave').map((l) => ({ ...l, node: l.draft ? undefined : byId(s.nodes, l.key) }));
      const gone = cur.filter((l) => !want.includes(l.key)).map((l) => ({ ...l, phase: 'leave' as const }));
      const added = want.filter((k) => !cur.some((l) => l.key === k && l.phase !== 'leave')).map((k) => mk(k, s, 'enter'));
      return [...keep, ...added, ...gone];
    });
    const t1 = requestAnimationFrame(() => requestAnimationFrame(() => setLayers((cur) => cur.map((l) => (l.phase === 'enter' ? { ...l, phase: 'in' } : l)))));
    const t2 = setTimeout(() => setLayers((cur) => cur.filter((l) => l.phase !== 'leave')), MS + 40);
    return () => { cancelAnimationFrame(t1); clearTimeout(t2); };
  }, [want.join('/'), s.nodes]); // eslint-disable-line react-hooks/exhaustive-deps

  const pop = () => { if (s.fork) s.setFork(undefined); else { const n = byId(s.nodes, s.current); if (n.parent) s.open(n.parent); } };
  useEffect(() => {
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape' && !(e.target as HTMLElement).closest('textarea')) pop(); };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  });

  const live = layers.filter((l) => l.phase !== 'leave');
  const top = live.length - 1;
  const covered = Math.min(top, SHOWN); // spines showing under the top layer
  return (
    <Zoom right={<span style={{ fontSize: 12, color: 'var(--text-3)', marginRight: 8 }}>节点 {s.nodes.length - 1}</span>} head={<Crumbs s={s} />}>
      <div style={{ position: 'relative', flex: 1, overflow: 'hidden', background: 'var(--bg)' }}>
        {layers.map((l) => {
          const i = live.indexOf(l);
          const t = l.phase === 'leave' ? -1 : top - i; // distance from the top layer
          // where this layer's left edge sits: the top one right of the spines,
          // covered ones one spine apart, older than SHOWN slid off the left
          const left = t < 0 ? covered * SPINE : (covered - t) * SPINE;
          const off = l.phase === 'enter' || l.phase === 'leave';
          const gone = t > SHOWN;
          return (
            <section key={l.key} style={{
              position: 'absolute', top: 0, bottom: 0, right: 0, left: Math.max(left, 0),
              transform: off ? 'translateX(100%)' : gone ? `translateX(${-SPINE * (t - SHOWN)}px)` : 'none',
              opacity: gone ? 0 : 1,
              transition: `transform ${MS}ms cubic-bezier(.2,.8,.2,1), left ${MS}ms cubic-bezier(.2,.8,.2,1), opacity ${MS}ms`,
              background: 'var(--surface)', display: 'flex',
              borderLeft: i > 0 || t < 0 ? '1px solid var(--line)' : 0,
              boxShadow: i > 0 || t < 0 ? '-14px 0 36px rgb(29 27 24 / .14)' : 'none',
              zIndex: l.phase === 'leave' ? 99 : i,
              pointerEvents: gone ? 'none' : 'auto',
            }}>
              <Spine l={l} shown={t > 0 && !gone} onClick={() => (l.draft ? null : s.open(l.key))} />
              <div style={{ flex: 1, minWidth: 0, display: 'flex', opacity: t > 0 ? 0 : 1, transition: `opacity ${MS}ms` }}>
                <Body l={l} s={s} onBack={pop} />
              </div>
            </section>
          );
        })}
      </div>
      <NodeMap s={s} />
    </Zoom>
  );
}

// ---------- the node map: read-only, collapsible, on the right ----------
// where the reader is in the whole tree; it never switches nodes (the chips
// and the back link do that), so nothing on it is clickable but its toggle.
const MAP_W = 260;
const COL = 60; // a column per leaf
const ROW = 62; // a row per depth

function NodeMap({ s }: { s: Store }) {
  const [open, setOpen] = useState(true);
  const g = tidy(s.nodes);
  const onPath = new Set(path(s.nodes, s.current).map((n) => n.id));
  const ease = 'cubic-bezier(.2,.8,.2,1)';
  return (
    <aside style={{ width: open ? MAP_W : 36, flex: 'none', borderLeft: '1px solid var(--line)', background: 'var(--bg)', transition: `width ${MS}ms ${ease}`, overflow: 'hidden', position: 'relative' }}>
      <button type="button" onClick={() => setOpen((v) => !v)} title={open ? '收起节点图' : '展开节点图'} style={{
        position: 'absolute', top: 10, right: 6, zIndex: 2, width: 24, height: 24, border: '1px solid var(--line)', borderRadius: 4, background: open ? 'var(--surface-2)' : 'var(--surface)', cursor: 'pointer', color: 'var(--text-3)', display: 'grid', placeItems: 'center', padding: 0, transition: `background ${MS}ms`,
      }}><Menu size={14} strokeWidth={2} /></button>
      {!open ? <div style={{ position: 'absolute', top: 46, left: 0, width: 36, writingMode: 'vertical-rl', display: 'flex', alignItems: 'center', fontSize: 12, color: 'var(--text-3)', letterSpacing: 2, opacity: open ? 0 : 1, transition: `opacity ${MS}ms` }}>节点图</div> : null}
      <div style={{ width: MAP_W, height: '100%', opacity: open ? 1 : 0, transition: `opacity ${MS * 0.7}ms ${ease} ${open ? MS * 0.4 : 0}ms`, display: 'flex', flexDirection: 'column', pointerEvents: 'none', userSelect: 'none' }}>
        <div style={{ padding: '13px 40px 8px 16px', fontSize: 12, color: 'var(--text-3)' }}>节点图 · {s.nodes.length - 1} 个节点</div>
        <div style={{ flex: 1, overflow: 'auto', position: 'relative', pointerEvents: 'auto' }}>
          <svg width={Math.max(MAP_W, g.w)} height={g.h} style={{ display: 'block' }}>
            {g.nodes.filter((r) => r.n.parent).map((r) => {
              const p = g.at.get(r.n.parent!)!;
              const hot = onPath.has(r.n.id);
              const my = (p.y + r.y) / 2;
              return (
                <g key={r.n.id}>
                  <path d={`M${p.x},${p.y + 6} C${p.x},${my} ${r.x},${my} ${r.x},${r.y - 6}`} fill="none"
                    stroke={hot ? TYPES[r.n.type].color : 'var(--rule)'} strokeWidth={hot ? 2 : 1.2}
                    strokeDasharray={r.n.type === 'divergent' ? '3 3' : undefined} style={{ transition: `stroke ${MS}ms` }} />
                  <text x={r.x + 5} y={r.y - 12} fontSize={9} fill="var(--text-3)">{r.at}</text>
                </g>
              );
            })}
            {g.nodes.map((r) => {
              const cur = r.n.id === s.current; const hot = onPath.has(r.n.id);
              const name = r.n.name.length > 5 ? r.n.name.slice(0, 4) + '…' : r.n.name;
              return (
                <g key={r.n.id} opacity={hot ? 1 : 0.5} style={{ transition: `opacity ${MS}ms` }}>
                  <title>{r.n.name}</title>
                  {cur ? <circle cx={r.x} cy={r.y} r={10} fill={TYPES[r.n.type].soft} stroke={TYPES[r.n.type].color} strokeWidth={1.5}>
                    <animate attributeName="r" values="9;11.5;9" dur="2.4s" repeatCount="indefinite" />
                  </circle> : null}
                  <circle cx={r.x} cy={r.y} r={cur ? 4.5 : 4} fill={TYPES[r.n.type].color} />
                  <text x={r.x} y={r.n.parent ? r.y + 22 : r.y - 12} textAnchor="middle" fontSize={11} fontWeight={cur ? 600 : 400} fill={cur ? TYPES[r.n.type].color : hot ? 'var(--text)' : 'var(--text-2)'}>{name}</text>
                </g>
              );
            })}
          </svg>
        </div>
      </div>
    </aside>
  );
}

// a top-down tidy tree: leaves take columns left to right in the order
// of the turns they forked from, a parent sits over the middle of its
// children, depth is the row
function tidy(nodes: DNode[]) {
  type P = { n: DNode; d: number; at?: number; x: number; y: number };
  const all: P[] = [];
  let col = 0;
  const walk = (n: DNode, d: number, at?: number): P => {
    const me: P = { n, d, at, x: 0, y: 40 + d * ROW };
    all.push(me);
    const kids = n.turns.flatMap((t, i) => nodes.filter((x) => x.forkTurn === t.id).map((k) => walk(k, d + 1, i + 1)));
    me.x = kids.length ? (kids[0].x + kids[kids.length - 1].x) / 2 : 30 + col++ * COL;
    return me;
  };
  walk(byId(nodes, 'main'), 0);
  const w = col * COL + 24;
  const shift = Math.max(0, (MAP_W - w) / 2);
  all.forEach((p) => { p.x += shift; });
  return { nodes: all, at: new Map(all.map((p) => [p.n.id, p])), w, h: Math.max(...all.map((p) => p.y)) + 40 };
}

const mk = (key: string, s: Store, phase: Layer['phase']): Layer => (key === 'draft' ? { key, draft: true, phase } : { key, node: byId(s.nodes, key), phase });

// a covered layer: its type and name standing on its left edge; click to
// pop everything above it
// it is always mounted and fades: the tint first, then the dot and the name
// a beat later, sliding in from the left, so a push reads as one motion
function Spine({ l, shown, onClick }: { l: Layer; shown: boolean; onClick: () => void }) {
  const type = l.node?.type ?? 'child';
  const ease = 'cubic-bezier(.2,.8,.2,1)';
  return (
    <button type="button" onClick={onClick} tabIndex={shown ? 0 : -1} title={l.node ? `回到「${l.node.name}」` : ''} style={{
      position: 'absolute', left: 0, top: 0, bottom: 0, width: SPINE, zIndex: 2, border: 0, cursor: 'pointer', padding: 0,
      background: 'var(--surface)', pointerEvents: shown ? 'auto' : 'none',
      opacity: shown ? 1 : 0, transition: `opacity ${MS}ms ${ease}`,
    }}>
      <span aria-hidden style={{
        position: 'absolute', inset: 0, background: `linear-gradient(90deg, ${TYPES[type].soft}, var(--surface))`,
        opacity: shown ? 1 : 0, transition: `opacity ${MS * 1.4}ms ${ease} ${shown ? 60 : 0}ms`,
      }} />
      <span style={{
        position: 'relative', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 10, paddingTop: 18, height: '100%',
        opacity: shown ? 1 : 0, transform: shown ? 'none' : 'translateX(-10px)',
        transition: `opacity ${MS}ms ${ease} ${shown ? MS * 0.55 : 0}ms, transform ${MS}ms ${ease} ${shown ? MS * 0.55 : 0}ms`,
      }}>
        <TypeDot type={type} size={9} />
        <span style={{ writingMode: 'vertical-rl', fontSize: 13, color: 'var(--text-2)', letterSpacing: 2, maxHeight: '70%', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{l.node?.name ?? '新节点'}</span>
      </span>
    </button>
  );
}

function Body({ l, s, onBack }: { l: Layer; s: Store; onBack: () => void }) {
  const ref = useRef<HTMLDivElement>(null);
  if (l.draft) {
    return s.fork ? (
      <div style={{ flex: 1, padding: '0 56px', overflow: 'auto' }}>
        <Back label="取消" onClick={onBack} />
        <Draft fork={s.fork} nodes={s.nodes} onCancel={onBack} onStart={s.start} />
      </div>
    ) : <div style={{ flex: 1 }} />;
  }
  const node = l.node!;
  const parent = node.parent ? byId(s.nodes, node.parent) : undefined;
  return (
    <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', padding: '0 56px' }}>
      {parent ? (
        <div style={{ display: 'flex', alignItems: 'center', padding: '12px 0 4px' }}>
          <Back label={parent.type === 'main' ? '主节点' : parent.name} onClick={onBack} />
          <span style={{ flex: 1 }} />
          <span onClick={() => s.remove(node.id)} style={{ fontSize: 12, color: 'var(--text-3)', cursor: 'pointer' }}>删除节点</span>
        </div>
      ) : null}
      <div ref={ref} style={{ flex: 1, overflow: 'auto', paddingTop: parent ? 8 : 20 }}>
        <div style={{ maxWidth: 680, margin: '0 auto' }}>
          <ForkHead nodes={s.nodes} id={node.id} onOpen={() => onBack()} />
          <Turns node={node} nodes={s.nodes} onFork={s.setFork} chips onOpen={s.open} />
        </div>
      </div>
      <div style={{ maxWidth: 680, width: '100%', margin: '0 auto' }}><Composer /></div>
    </div>
  );
}

function Back({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} style={{ border: 0, background: 'none', cursor: 'pointer', color: '#4b6a9b', fontSize: 13, padding: '4px 0', display: 'inline-flex', gap: 4, alignItems: 'center', font: 'inherit' }}>
      ‹ {label}
    </button>
  );
}

// the whole path, including layers whose spines have faded off the left
function Crumbs({ s }: { s: Store }) {
  const p = path(s.nodes, s.current);
  const [sib, setSib] = useState<string>();
  if (p.length < 2) return null;
  return (
    <div style={{ display: 'flex', gap: 2, alignItems: 'center', fontSize: 12, minWidth: 0 }}>
      {p.map((n, i) => {
        const sibs = n.parent ? childrenOf(s.nodes, n.parent) : [];
        const last = i === p.length - 1;
        return (
          <span key={n.id} style={{ display: 'inline-flex', alignItems: 'center', gap: 2, position: 'relative', minWidth: 0 }}>
            {i ? <span style={{ color: 'var(--text-3)', padding: '0 2px' }}>›</span> : null}
            <button type="button" onClick={() => s.open(n.id)} style={{ border: 0, background: last ? TYPES[n.type].soft : 'none', borderRadius: 4, padding: '2px 6px', cursor: 'pointer', display: 'inline-flex', gap: 5, alignItems: 'center', font: 'inherit', fontWeight: last ? 600 : 400, color: last ? 'var(--text)' : 'var(--text-2)', maxWidth: 160, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
              <TypeDot type={n.type} size={6} />{n.name}
            </button>
            {sibs.length > 1 ? <button type="button" onClick={() => setSib(sib === n.id ? undefined : n.id)} style={{ border: 0, background: 'none', cursor: 'pointer', color: 'var(--text-3)', fontSize: 9 }}>▾</button> : null}
            {sib === n.id ? (
              <div style={{ position: 'absolute', top: '120%', left: 0, zIndex: 300, background: '#fff', border: '1px solid var(--line)', borderRadius: 6, boxShadow: '0 8px 24px rgb(0 0 0 / .12)', padding: 4, minWidth: 200 }}>
                {sibs.map((x) => <div key={x.id} onClick={() => { setSib(undefined); s.open(x.id); }} style={{ padding: '6px 8px', cursor: 'pointer', display: 'flex', gap: 6, alignItems: 'center', fontSize: 13, fontWeight: x.id === n.id ? 600 : 400 }}><TypeDot type={x.type} />{x.name}</div>)}
              </div>
            ) : null}
          </span>
        );
      })}
    </div>
  );
}

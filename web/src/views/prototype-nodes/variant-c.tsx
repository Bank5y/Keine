// PROTOTYPE (#137): variant C — inline markers, opened as a stack of drawers.
// The stack is the current node's path: clicking a node's chip pushes its
// drawer from the right, going back pops it. Underneath, at most three
// layers show as spines on the left; older ones slide off left and fade.
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
              {t > 0 && !gone ? <Spine l={l} onClick={() => (l.draft ? null : s.open(l.key))} /> : null}
              <div style={{ flex: 1, minWidth: 0, display: 'flex', opacity: t > 0 ? 0 : 1, transition: `opacity ${MS}ms` }}>
                <Body l={l} s={s} onBack={pop} />
              </div>
            </section>
          );
        })}
      </div>
    </Zoom>
  );
}

const mk = (key: string, s: Store, phase: Layer['phase']): Layer => (key === 'draft' ? { key, draft: true, phase } : { key, node: byId(s.nodes, key), phase });

// a covered layer: its type and name standing on its left edge; click to
// pop everything above it
function Spine({ l, onClick }: { l: Layer; onClick: () => void }) {
  const type = l.node?.type ?? 'child';
  return (
    <button type="button" onClick={onClick} title={l.node ? `回到「${l.node.name}」` : ''} style={{
      position: 'absolute', left: 0, top: 0, bottom: 0, width: SPINE, zIndex: 2, border: 0, cursor: 'pointer',
      background: `linear-gradient(90deg, ${TYPES[type].soft}, var(--surface))`, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 10, paddingTop: 18,
    }}>
      <TypeDot type={type} size={9} />
      <span style={{ writingMode: 'vertical-rl', fontSize: 13, color: 'var(--text-2)', letterSpacing: 2, maxHeight: '70%', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{l.node?.name ?? '新节点'}</span>
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
        <div style={{ maxWidth: 820, margin: '0 auto' }}>
          <ForkHead nodes={s.nodes} id={node.id} onOpen={() => onBack()} />
          <Turns node={node} nodes={s.nodes} onFork={s.setFork} chips onOpen={s.open} />
        </div>
      </div>
      <div style={{ maxWidth: 820, width: '100%', margin: '0 auto' }}><Composer /></div>
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

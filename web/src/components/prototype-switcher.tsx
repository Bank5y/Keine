// PROTOTYPE: throwaway variant switcher for UI prototypes. Never rendered in
// production builds.
import { useEffect } from 'react';
import { setParam } from '@/lib/router';

export function PrototypeSwitcher({ variants, current, names, extra }: {
  variants: string[]; current: string; names: Record<string, string>; extra?: React.ReactNode;
}) {
  const i = Math.max(0, variants.indexOf(current));
  const go = (d: number) => setParam('variant', variants[(i + d + variants.length) % variants.length]);
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (t.closest('input, textarea, [contenteditable]')) return;
      if (e.key === 'ArrowLeft') go(-1);
      if (e.key === 'ArrowRight') go(1);
    };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  });
  if (!import.meta.env.DEV) return null;
  return (
    <div style={{
      position: 'fixed', left: '50%', bottom: 18, transform: 'translateX(-50%)', zIndex: 9999,
      display: 'flex', alignItems: 'center', gap: 10, padding: '6px 10px', borderRadius: 999,
      background: '#111', color: '#fff', font: '13px/1.4 system-ui, sans-serif', boxShadow: '0 8px 24px rgba(0,0,0,.35)',
    }}>
      <button type="button" onClick={() => go(-1)} style={btn}>←</button>
      <span style={{ minWidth: 220, textAlign: 'center' }}>{current} · {names[current]}</span>
      <button type="button" onClick={() => go(1)} style={btn}>→</button>
      {extra}
    </div>
  );
}

const btn: React.CSSProperties = { background: '#333', color: '#fff', border: 0, borderRadius: 999, width: 28, height: 28, cursor: 'pointer' };

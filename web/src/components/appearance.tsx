import { useEffect, useMemo, useState } from 'react';
import { ScanSearch } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { CHANGED, MODES, PALETTES, PRESETS, loadAppearance, setFont, setMode, setPalette, type FontChoice, type FontRole } from '@/lib/appearance';
import { detectFonts, installed, monospace, splitMonospace } from '@/lib/local-fonts';
import { cn } from '@/lib/utils';

// AppearanceSection is 外观 on the account page: the palette, light or dark,
// and the fonts for reading and code, all kept in this browser only.
export function AppearanceSection() {
  const [a, setA] = useState(loadAppearance);
  const [found, setFound] = useState<string[] | null>(null);
  const [detecting, setDetecting] = useState(false);
  // the masthead's light/dark button changes the same setting
  useEffect(() => {
    const sync = () => setA(loadAppearance());
    window.addEventListener(CHANGED, sync);
    return () => window.removeEventListener(CHANGED, sync);
  }, []);

  async function detect() {
    setDetecting(true);
    try { setFound(await detectFonts()); } finally { setDetecting(false); }
  }

  return (
    <section className="me-section appearance" aria-labelledby="ap-head">
      <h2 id="ap-head">外观</h2>
      <p>只保存在这个浏览器上, 换一台设备要重新设置。</p>
      <div className="form-rows">
        <div className="field">
          <label id="ap-palette">配色</label>
          <div className="swatches" role="radiogroup" aria-labelledby="ap-palette">
            {PALETTES.map((p) => (
              <button key={p.id} type="button" role="radio" aria-checked={a.palette === p.id} className={cn('swatch', a.palette === p.id && 'on')} onClick={() => setPalette(p.id)}>
                <span className="swatch-ball" style={{ background: `linear-gradient(90deg, ${p.paper} 50%, ${p.accent} 50%)` }} aria-hidden="true" />
                <span>{p.label}</span>
              </button>
            ))}
          </div>
        </div>
        <div className="field">
          <label id="ap-mode">明暗</label>
          <div className="seg" role="radiogroup" aria-labelledby="ap-mode">
            {MODES.map((m) => (
              <button key={m.id} type="button" role="radio" aria-checked={a.mode === m.id} className={cn(a.mode === m.id && 'on')} onClick={() => setMode(m.id)}>{m.label}</button>
            ))}
          </div>
        </div>
        <FontField role="read" label="文章字体" choice={a.read} found={found} />
        <FontField role="code" label="代码字体" choice={a.code} found={found} />
        <div className="ap-detect">
          <Button type="button" variant="outline" disabled={detecting} onClick={() => void detect()}>
            <ScanSearch aria-hidden="true" />{detecting ? '检测中…' : '检测本机字体'}
          </Button>
          <small>{found ? `找到 ${found.length} 款字体, 已加进上面的下拉框。` : '只在这个浏览器里查找, 字体列表不会发出去。'}</small>
        </div>
      </div>
    </section>
  );
}

const OTHER = 'other';

function FontField({ role, label, choice, found }: { role: FontRole; label: string; choice: FontChoice; found: string[] | null }) {
  const family = 'family' in choice ? choice.family : '';
  // typing a name: the input stays open even while it is empty
  const [typing, setTyping] = useState(false);
  const local = useMemo(() => {
    const names = found ?? [];
    const all = family && !names.includes(family) ? [family, ...names] : names;
    return role === 'code' ? splitMonospace(all, monospace) : { mono: [], other: all };
  }, [found, family, role]);
  const value = typing ? OTHER : 'family' in choice ? `f:${choice.family}` : `p:${choice.preset}`;
  const id = `ap-${role}`;

  function pick(v: string) {
    if (v === OTHER) { setTyping(true); return; }
    setTyping(false);
    setFont(role, v.startsWith('f:') ? { family: v.slice(2) } : { preset: v.slice(2) });
  }

  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <select id={id} className="select" value={value} onChange={(e) => pick(e.target.value)}>
        <optgroup label="预设">
          {PRESETS[role].map((p) => <option key={p.id} value={`p:${p.id}`}>{p.label}</option>)}
        </optgroup>
        {local.mono.length ? <optgroup label="等宽字体">{local.mono.map((f) => <option key={f} value={`f:${f}`}>{f}</option>)}</optgroup> : null}
        {local.other.length ? <optgroup label={role === 'code' ? '其他字体' : '本机字体'}>{local.other.map((f) => <option key={f} value={`f:${f}`}>{f}</option>)}</optgroup> : null}
        <option value={OTHER}>其他…</option>
      </select>
      {typing ? (
        <>
          <Input aria-label={`${label}名`} placeholder="字体名, 如 LXGW WenKai" value={family} autoFocus onChange={(e) => setFont(role, { family: e.target.value })} />
          {family.trim() && !installed(family) ? <small className="hint warn">本机未安装该字体, 已回退到默认字体。</small> : null}
        </>
      ) : null}
      <div className={cn('ap-preview', role === 'code' && 'code')} aria-hidden="true">
        {role === 'read' ? '学而时习之, 不亦说乎。The quick brown fox jumps over the lazy dog.' : 'const ok = items.filter((x) => x.id !== 0); // 0O1lI'}
      </div>
    </div>
  );
}

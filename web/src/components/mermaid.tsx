import { useEffect, useId, useState, type ReactNode } from 'react';
import { DiagramZoom } from '@/components/zoom';

let loading: Promise<typeof import('mermaid').default> | null = null;

// load brings Mermaid in the first time a diagram shows: it is large, and
// only analyses draw diagrams.
function load() {
  loading ??= import('mermaid').then(({ default: m }) => m);
  return loading;
}

// configure sets Mermaid up in the page's current colours. securityLevel
// strict keeps the model's text from running scripts or adding links.
// Labels are SVG text, not HTML (#184): an HTML label is sized where
// Mermaid measures it, then drawn in the page's own type, and cut off
// where the two differ. The font is named outright, since the drawn
// diagram is an image and inherits nothing.
function configure(m: typeof import('mermaid').default) {
  const css = getComputedStyle(document.documentElement);
  const v = (name: string) => css.getPropertyValue(name).trim();
  m.initialize({
    startOnLoad: false, securityLevel: 'strict', theme: 'base', htmlLabels: false, fontFamily: v('--sans'),
    themeVariables: {
      fontFamily: v('--sans'), fontSize: '13px',
      background: v('--surface'), primaryColor: v('--surface-2'), primaryBorderColor: v('--rule'), primaryTextColor: v('--text'),
      secondaryColor: v('--surface'), tertiaryColor: v('--surface'), lineColor: v('--text-3'), textColor: v('--text-2'),
      edgeLabelBackground: v('--surface'), noteBkgColor: v('--surface-2'), noteTextColor: v('--text'), noteBorderColor: v('--rule'),
      actorTextColor: v('--text'), signalTextColor: v('--text-2'), labelTextColor: v('--text'),
    },
  });
}

// Drawn is a diagram ready to embed: its SVG as a data URL, and its size.
export interface Drawn { src: string; w: number; h: number }

// centerLabels puts each shape's label in its middle: with SVG labels
// Mermaid lays a state's text out from the left of a box sized for a
// longer one (#184). A shape is drawn about its own origin.
function centerLabels(svg: SVGSVGElement) {
  for (const label of svg.querySelectorAll<SVGGElement>('g.node > g.label')) {
    const text = label.querySelector('text');
    const m = /translate\(\s*([-\d.e]+)[\s,]+([-\d.e]+)\s*\)/.exec(label.getAttribute('transform') ?? '');
    if (!text || !m) continue;
    const box = text.getBBox();
    const off = Number(m[1]) + box.x + box.width / 2;
    if (box.width > 0 && Math.abs(off) > 1) label.setAttribute('transform', `translate(${Number(m[1]) - off}, ${m[2]})`);
  }
}

// toImage makes Mermaid's SVG an image: its labels centred, measured on
// the page out of sight; sized by its viewBox (it says width 100%, which
// means nothing in an image); as a data URL.
function toImage(svg: string): Drawn | null {
  const doc = new DOMParser().parseFromString(svg, 'image/svg+xml');
  const parsed = doc.documentElement;
  const box = (parsed.getAttribute('viewBox') ?? '').split(/[\s,]+/).map(Number);
  if (parsed.nodeName !== 'svg' || box.length !== 4 || !(box[2] > 0) || !(box[3] > 0)) return null;
  const [, , w, h] = box;
  const host = document.createElement('div');
  host.style.cssText = 'position:absolute;left:-100000px;top:0;visibility:hidden';
  const el = document.importNode(parsed, true) as unknown as SVGSVGElement;
  host.appendChild(el);
  document.body.appendChild(host);
  try { centerLabels(el); } finally { host.remove(); }
  el.setAttribute('width', String(w));
  el.setAttribute('height', String(h));
  el.removeAttribute('style');
  el.setAttribute('xmlns', 'http://www.w3.org/2000/svg');
  return { src: 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(new XMLSerializer().serializeToString(el)), w, h };
}

// useTheme is the page's theme as it is now: the one the reader picked,
// else the system's. It changes as either does.
function useTheme(): string {
  const read = () => document.documentElement.dataset.theme || (matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
  const [theme, setTheme] = useState(read);
  useEffect(() => {
    const update = () => setTheme(read());
    const mo = new MutationObserver(update);
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
    const mq = matchMedia('(prefers-color-scheme: dark)');
    mq.addEventListener('change', update);
    return () => { mo.disconnect(); mq.removeEventListener('change', update); };
  }, []);
  return theme;
}

// Mermaid draws a diagram from its source, as an image kept apart from the
// page's styles; until it is drawn, and for good if it cannot be, the
// fallback (the code block) shows instead. A click opens it in a lightbox.
export function Mermaid({ code, fallback }: { code: string; fallback: ReactNode }) {
  const id = 'mmd-' + useId().replace(/[^a-zA-Z0-9]/g, '');
  const [drawn, setDrawn] = useState<Drawn | null>(null);
  const [open, setOpen] = useState(false);
  // the theme the colours come from: a diagram is drawn again when it changes
  const theme = useTheme();
  useEffect(() => {
    let alive = true;
    setDrawn(null);
    load()
      .then((m) => { configure(m); return m.render(id, code); })
      .then((r) => { if (alive) setDrawn(toImage(r.svg)); })
      .catch(() => { document.getElementById('d' + id)?.remove(); });
    return () => { alive = false; };
  }, [id, code, theme]);
  if (!drawn) return <>{fallback}</>;
  return (
    <div className="mermaid-diagram">
      <button type="button" className="mermaid-open" aria-label="放大查看图表" onClick={() => setOpen(true)}>
        <img src={drawn.src} alt="图表" width={drawn.w} height={drawn.h} draggable={false} />
      </button>
      {open ? <DiagramZoom picture={drawn} onClose={() => setOpen(false)} /> : null}
    </div>
  );
}

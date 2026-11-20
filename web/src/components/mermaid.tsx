import { useEffect, useId, useState, type ReactNode } from 'react';

let loading: Promise<typeof import('mermaid').default> | null = null;

// load brings Mermaid in the first time a diagram shows: it is large, and
// only analyses draw diagrams. securityLevel strict keeps the model's text
// from running scripts or adding links.
function load() {
  loading ??= import('mermaid').then(({ default: m }) => {
    // the page's own colours, as the current theme sets them
    const css = getComputedStyle(document.documentElement);
    const v = (name: string) => css.getPropertyValue(name).trim();
    m.initialize({
      startOnLoad: false, securityLevel: 'strict', theme: 'base', fontFamily: 'inherit',
      themeVariables: {
        background: v('--surface'), primaryColor: v('--surface-2'), primaryBorderColor: v('--rule'), primaryTextColor: v('--text'),
        secondaryColor: v('--surface'), tertiaryColor: v('--surface'), lineColor: v('--text-3'), textColor: v('--text-2'), fontSize: '13px',
      },
    });
    return m;
  });
  return loading;
}

// Mermaid draws a diagram from its source; until it is drawn, and for good
// if it cannot be, the fallback (the code block) shows instead.
export function Mermaid({ code, fallback }: { code: string; fallback: ReactNode }) {
  const id = 'mmd-' + useId().replace(/[^a-zA-Z0-9]/g, '');
  const [svg, setSvg] = useState<string | null>(null);
  useEffect(() => {
    let alive = true;
    setSvg(null);
    load()
      .then((m) => m.render(id, code))
      .then((r) => { if (alive) setSvg(r.svg); })
      .catch(() => { document.getElementById('d' + id)?.remove(); });
    return () => { alive = false; };
  }, [id, code]);
  if (!svg) return <>{fallback}</>;
  // eslint-disable-next-line react/no-danger -- Mermaid's own output, rendered with securityLevel strict
  return <div className="mermaid-diagram" dangerouslySetInnerHTML={{ __html: svg }} />;
}

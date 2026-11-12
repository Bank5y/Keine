import type { ReactNode } from 'react';

// Highlight marks every case-insensitive occurrence of q in text.
export function Highlight({ text, q }: { text: string; q?: string }) {
  if (!q) return <>{text}</>;
  const lower = text.toLowerCase();
  const ql = q.toLowerCase();
  const out: ReactNode[] = [];
  let i = 0;
  for (;;) {
    const j = lower.indexOf(ql, i);
    if (j < 0) break;
    out.push(text.slice(i, j), <mark key={j}>{text.slice(j, j + q.length)}</mark>);
    i = j + q.length;
  }
  out.push(text.slice(i));
  return <>{out}</>;
}

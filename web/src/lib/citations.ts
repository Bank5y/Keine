import { seenBy } from '@/lib/dialogue';
import type { DialogueNode } from '@/lib/explorations';

// Citations in the dialogue's replies (ADR 0013): the model writes [n] for
// a web source the run numbered, never an address; the reply shows the
// source the system recorded under that number, and nothing for a number
// it does not know.

// Cited is a numbered source a reply may cite: where it is, its title, and
// its site, which the citation shows.
export interface Cited { n: number; url: string; title: string; host: string }

export const hostOf = (url: string) => {
  try { return new URL(url).hostname.replace(/^www\./, ''); } catch { return url; }
};

// citationTable is what a turn may cite: the numbered sources of the turns
// on its chain, itself included; a sibling's are not on it.
export function citationTable(nodes: DialogueNode[], turnId: number): Map<number, Cited> {
  const out = new Map<number, Cited>();
  for (const t of seenBy(nodes, turnId)) {
    for (const raw of t.meta.sources) {
      const s = raw as { n?: number; url?: string; title?: string };
      if (!s.n || !s.url || out.has(s.n)) continue;
      out.set(s.n, { n: s.n, url: s.url, title: s.title || hostOf(s.url), host: hostOf(s.url) });
    }
  }
  return out;
}

// a citation as written: one number in brackets, or several side by side
// or comma separated
const CITATION = /(?<![A-Za-z0-9_)\]])\s*((?:\[\d+(?:\s*[,，]\s*\d+)*\])+)/g;

// splitCitations cuts a text at its citations: the text around them, and
// the source of each number known; one not known goes, with the space
// before it when none of its group is known.
export function splitCitations(text: string, table: Map<number, Cited>): (string | Cited)[] {
  const out: (string | Cited)[] = [];
  let at = 0;
  let plain = '';
  for (const m of text.matchAll(CITATION)) {
    const found = (m[1].match(/\d+/g) ?? []).map(Number).flatMap((n) => table.get(n) ?? []);
    const lead = m[0].slice(0, m[0].length - m[1].length);
    plain += text.slice(at, m.index) + (found.length ? lead : '');
    at = m.index + m[0].length;
    if (!found.length) continue;
    if (plain) out.push(plain);
    plain = '';
    out.push(...found);
  }
  plain += text.slice(at);
  if (plain || !out.length) out.push(plain);
  return out;
}

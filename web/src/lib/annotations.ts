// Annotations remember their place by the quoted text and a little text
// around it (ADR-0003). The server finds them in the lesson text and answers
// with a range counted in non-whitespace characters; TextMap turns such a
// range into a DOM Range over the rendered lesson, and a selection back into
// a quote.

export type AnnotationStyle = 'underline' | 'highlight';
export type HighlightColor = 'yellow' | 'green' | 'blue' | 'pink';
export const HIGHLIGHT_COLORS: HighlightColor[] = ['yellow', 'green', 'blue', 'pink'];
export const COLOR_LABEL: Record<HighlightColor, string> = { yellow: '黄色', green: '绿色', blue: '蓝色', pink: '粉色' };

export interface Annotation {
  id: number;
  style: AnnotationStyle;
  color: HighlightColor | null;
  quote: string;
  prefix: string;
  suffix: string;
  note: string | null;
  range: { start: number; end: number } | null;
}

// a look is a style with its colour, as the toolbar offers them
export type Look = { style: 'underline' } | { style: 'highlight'; color: HighlightColor };
export const lookName = (a: { style: AnnotationStyle; color: HighlightColor | null }) => (a.style === 'underline' ? 'underline' : a.color ?? 'yellow');

export interface Quote { quote: string; prefix: string; suffix: string }

// whitespace the server skips as well
const SPACE = /[\s\u0085\uFEFF]/u;
// how much text before and after a selection a quote keeps
const CONTEXT = 32;

interface Char { node: Text; offset: number; len: number }

export class TextMap {
  private chars: Char[] = [];
  private visible = '';

  constructor(private root: HTMLElement) {
    const w = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    const out: string[] = [];
    for (let n = w.nextNode() as Text | null; n; n = w.nextNode() as Text | null) {
      let off = 0;
      for (const ch of n.data) {
        if (!SPACE.test(ch)) { this.chars.push({ node: n, offset: off, len: ch.length }); out.push(ch); }
        off += ch.length;
      }
    }
    this.visible = out.join('');
  }

  private domRange(start: number, end: number) {
    if (start < 0 || end > this.chars.length || start >= end) return null;
    const a = this.chars[start], b = this.chars[end - 1];
    const r = document.createRange();
    r.setStart(a.node, a.offset);
    r.setEnd(b.node, b.offset + b.len);
    return r;
  }

  // codepoint offsets of the visible text, to compare server ranges with
  private slice(start: number, end: number) {
    return Array.from(this.visible).slice(start, end).join('');
  }

  // place finds an annotation on the page: where the server says it is, or,
  // should the page's text differ from the server's there, the nearest place
  // on the page that holds its quote.
  place(a: Annotation): Range | null {
    if (!a.range) return null;
    const want = strip(a.quote);
    if (this.slice(a.range.start, a.range.end) === want) return this.domRange(a.range.start, a.range.end);
    const cps = Array.from(this.visible);
    const hay = cps.join('');
    let best = -1;
    for (let i = hay.indexOf(want); i >= 0; i = hay.indexOf(want, i + 1)) {
      const at = Array.from(hay.slice(0, i)).length;
      if (best < 0 || Math.abs(at - a.range.start) < Math.abs(best - a.range.start)) best = at;
    }
    return best < 0 ? null : this.domRange(best, best + Array.from(want).length);
  }

  // quote reads a selection inside the root as an annotation would keep it;
  // null when it holds no text (an image alone, say).
  quote(sel: Range): Quote | null {
    const before = document.createRange();
    before.selectNodeContents(this.root);
    const after = before.cloneRange();
    if (!this.root.contains(sel.commonAncestorContainer)) return null;
    before.setEnd(sel.startContainer, sel.startOffset);
    after.setStart(sel.endContainer, sel.endOffset);
    const quote = sel.toString();
    if (!strip(quote)) return null;
    return { quote: quote.trim(), prefix: tail(before.toString()), suffix: head(after.toString()) };
  }
}

const strip = (s: string) => s.replace(new RegExp(SPACE.source, 'gu'), '');

// head and tail keep CONTEXT visible characters of text, plus the spaces
// between them
function head(s: string) {
  let n = 0, i = 0;
  for (const ch of s) { if (n >= CONTEXT) break; if (!SPACE.test(ch)) n++; i += ch.length; }
  return s.slice(0, i);
}
function tail(s: string) {
  const cps = Array.from(s);
  let n = 0, i = cps.length;
  while (i > 0 && n < CONTEXT) { i--; if (!SPACE.test(cps[i])) n++; }
  return cps.slice(i).join('');
}

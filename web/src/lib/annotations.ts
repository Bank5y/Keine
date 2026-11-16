import { api } from '@/lib/api';
import { dropSpan, wordStart } from '@/lib/drop';

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
  // attached: shown in the lesson text. pending: its text changed in a
  // course update and it awaits migration. changed / deleted: an excerpt
  // whose text changed / whose lesson was deleted.
  state: AnnotationState;
  // while pending, the previous text it was last shown in
  // (GET /api/courses/{id}/lessons/{lesson}/previous-texts)
  previousTextId: number | null;
  range: { start: number; end: number } | null;
}
export type AnnotationState = 'attached' | 'pending' | 'changed' | 'deleted';
export const STATE_LABEL: Record<Exclude<AnnotationState, 'attached'>, string> = { pending: '待迁移', changed: '原文已变化', deleted: '原文已删除' };

// The notes page: every annotation of the reader's, with where it is.
export interface NoteEntry extends Annotation {
  createdAt: string;
  updatedAt: string;
  course: { id: number; title: string };
  // null when the course no longer has the lesson
  lesson: { id: number; title: string; chapter: string } | null;
}
export interface NoteCourse { id: number; title: string; count: number }
export interface Notebook { annotations: NoteEntry[]; courses: NoteCourse[] }
export type NoteSort = 'time' | 'text';

// the address that opens a lesson at one of its annotations
export const ANNOTATION_HASH = '#annotation-';
export const annotationURL = (courseId: number, lessonId: number, id: number) => `/courses/${courseId}/lessons/${lessonId}${ANNOTATION_HASH}${id}`;

// a look is a style with its colour, as the toolbar offers them
export type Look = { style: 'underline' } | { style: 'highlight'; color: HighlightColor };
export const lookName = (a: { style: AnnotationStyle; color: HighlightColor | null }) => (a.style === 'underline' ? 'underline' : a.color ?? 'yellow');

export interface Quote { quote: string; prefix: string; suffix: string }

// Annotation migration: an annotation awaiting migration (or kept as an
// excerpt) is placed on a quote of the lesson's text as it is now, however
// that place was picked: a selection, a suggestion or a drop.
export const placeAnnotation = (id: number, at: Quote) => api<Annotation>('POST', `/api/annotations/${id}/place`, at);
// keepAsExcerpt keeps an annotation awaiting migration as an excerpt
// (state changed): out of the text, its quote and note kept.
export const keepAsExcerpt = (id: number) => api<void>('POST', `/api/annotations/${id}/excerpt`);
export const migrateURL = (courseId: number, lessonId: number) => `/courses/${courseId}/lessons/${lessonId}/migrate`;

// whitespace the server skips as well
const SPACE = /[\s\u0085\uFEFF]/u;
// how much text before and after a selection a quote keeps
const CONTEXT = 32;

interface Char { node: Text; offset: number; len: number }

// Page furniture inside the rendered text (a code block's title bar, say) is
// not lesson text; it is marked data-chrome and skipped.
const CHROME = '[data-chrome]';

export class TextMap {
  private chars: Char[] = [];
  private visible = '';
  // the lesson's text nodes and where each starts in raw
  private nodes: { node: Text; start: number }[] = [];
  private raw = '';
  private chrome: Element[];

  constructor(root: HTMLElement) {
    this.chrome = Array.from(root.querySelectorAll(CHROME));
    const w = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
      acceptNode: (n) => (n.parentElement?.closest(CHROME) ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT),
    });
    const out: string[] = [];
    for (let n = w.nextNode() as Text | null; n; n = w.nextNode() as Text | null) {
      this.nodes.push({ node: n, start: this.raw.length });
      this.raw += n.data;
      let off = 0;
      for (const ch of n.data) {
        if (!SPACE.test(ch)) { this.chars.push({ node: n, offset: off, len: ch.length }); out.push(ch); }
        off += ch.length;
      }
    }
    this.visible = out.join('');
  }

  // the visible text: the lesson's characters without whitespace, as the
  // server counts ranges
  get text() { return this.visible; }

  // domRanges covers [start, end), split around page furniture so that is
  // never part of an annotation
  domRanges(start: number, end: number): Range[] | null {
    if (start < 0 || end > this.chars.length || start >= end) return null;
    const out: Range[] = [];
    const span = (a: Char, b: Char) => {
      const r = document.createRange();
      r.setStart(a.node, a.offset);
      r.setEnd(b.node, b.offset + b.len);
      return r;
    };
    let first = this.chars[start];
    for (let i = start + 1; i < end; i++) {
      const prev = this.chars[i - 1], c = this.chars[i];
      if (c.node !== prev.node && this.chrome.some((el) => span(prev, c).intersectsNode(el))) {
        out.push(span(first, prev));
        first = c;
      }
    }
    out.push(span(first, this.chars[end - 1]));
    return out;
  }

  // codepoint offsets of the visible text, to compare server ranges with
  private slice(start: number, end: number) {
    return Array.from(this.visible).slice(start, end).join('');
  }

  // place finds an annotation on the page: where the server says it is, or,
  // should the page's text differ from the server's there, the nearest place
  // on the page that holds its quote.
  place(a: Annotation): Range[] | null {
    if (!a.range) return null;
    const want = strip(a.quote);
    if (this.slice(a.range.start, a.range.end) === want) return this.domRanges(a.range.start, a.range.end);
    const cps = Array.from(this.visible);
    const hay = cps.join('');
    let best = -1;
    for (let i = hay.indexOf(want); i >= 0; i = hay.indexOf(want, i + 1)) {
      const at = Array.from(hay.slice(0, i)).length;
      if (best < 0 || Math.abs(at - a.range.start) < Math.abs(best - a.range.start)) best = at;
    }
    return best < 0 ? null : this.domRanges(best, best + Array.from(want).length);
  }

  // find looks for a quote on the page without the server's help (in a
  // previous text, say): where its text is, the place whose surroundings
  // match its prefix and suffix best.
  find(q: Quote): { start: number; end: number } | null {
    const want = strip(q.quote);
    if (!want) return null;
    const len = Array.from(want).length;
    const pre = Array.from(strip(q.prefix)), suf = Array.from(strip(q.suffix));
    const cps = Array.from(this.visible);
    const hay = cps.join('');
    let best: { start: number; score: number } | null = null;
    for (let i = hay.indexOf(want); i >= 0; i = hay.indexOf(want, i + 1)) {
      const at = Array.from(hay.slice(0, i)).length;
      let score = 0;
      while (score < pre.length && cps[at - 1 - score] === pre[pre.length - 1 - score]) score++;
      for (let j = 0; j < suf.length && cps[at + len + j] === suf[j]; j++) score++;
      if (!best || score > best.score) best = { start: at, score };
    }
    return best && { start: best.start, end: best.start + len };
  }

  // quoteOf reads [start, end) of the visible text as an annotation would
  // keep it, like a selection of exactly that text (for a suggested or
  // dropped place, say).
  quoteOf(start: number, end: number): Quote | null {
    if (start < 0 || end > this.chars.length || start >= end) return null;
    const a = this.rawOf(this.chars[start]);
    const last = this.chars[end - 1];
    const b = this.rawOf(last) + last.len;
    return { quote: this.raw.slice(a, b).trim(), prefix: tail(this.raw.slice(0, a)), suffix: head(this.raw.slice(b)) };
  }

  // dropAt is where an annotation of len visible characters lands when
  // dropped at a DOM boundary point: from the word there, as long as it was.
  dropAt(container: Node, offset: number, len: number): { start: number; end: number } | null {
    if (container.nodeType === Node.TEXT_NODE && this.nodes.some((n) => n.node === container)) {
      offset = wordStart((container as Text).data, offset);
    }
    const at = this.indexOf(container, offset);
    // the first visible character at or after the point
    let lo = 0, hi = this.chars.length;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (this.rawOf(this.chars[mid]) < at) lo = mid + 1; else hi = mid;
    }
    return dropSpan(lo, len, this.chars.length);
  }

  private rawOf(c: Char) { return this.nodes.find((n) => n.node === c.node)!.start + c.offset; }

  // quote reads a selection inside the root as an annotation would keep it;
  // null when it holds no text (an image alone, say).
  quote(sel: Range): Quote | null {
    const a = this.indexOf(sel.startContainer, sel.startOffset);
    const b = this.indexOf(sel.endContainer, sel.endOffset);
    const quote = this.raw.slice(a, b);
    if (!strip(quote)) return null;
    return { quote: quote.trim(), prefix: tail(this.raw.slice(0, a)), suffix: head(this.raw.slice(b)) };
  }

  // indexOf turns a DOM boundary point into an index into raw
  private indexOf(container: Node, offset: number) {
    const at = document.createRange();
    at.setStart(container, offset);
    for (const { node, start } of this.nodes) {
      if (node === container) return start + offset;
      if (at.comparePoint(node, 0) >= 0) return start; // the node lies after the point
    }
    return this.raw.length;
  }
}

const strip = (s: string) => s.replace(new RegExp(SPACE.source, 'gu'), '');
// how many visible characters a quote holds, as TextMap counts them
export const visibleLength = (s: string) => Array.from(strip(s)).length;

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

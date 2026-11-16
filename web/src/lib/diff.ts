// diffText compares two texts codepoint by codepoint (Myers' O(ND) diff) and
// tells which spans of the old text were removed and which of the new text
// were added. Offsets count codepoints, as TextMap's do.

export interface Span { start: number; end: number }
export interface TextDiff { removed: Span[]; added: Span[] }

// past this many edits the texts are told apart as one block each
const MAX_EDITS = 4000;

export function diffText(oldText: string, newText: string): TextDiff {
  const a = Array.from(oldText), b = Array.from(newText);
  let lo = 0;
  while (lo < a.length && lo < b.length && a[lo] === b[lo]) lo++;
  let ea = a.length, eb = b.length;
  while (ea > lo && eb > lo && a[ea - 1] === b[eb - 1]) { ea--; eb--; }
  const edits = myers(a.slice(lo, ea), b.slice(lo, eb));
  const removed: number[] = [], added: number[] = [];
  if (edits) {
    for (const i of edits.removed) removed.push(lo + i);
    for (const j of edits.added) added.push(lo + j);
  } else {
    for (let i = lo; i < ea; i++) removed.push(i);
    for (let j = lo; j < eb; j++) added.push(j);
  }
  absorbCoincidences(removed, added, a.length, b.length);
  return { removed: spans(removed), added: spans(added) };
}

// a character that happens to match inside a rewrite ("调用" and "使用"
// share "用") would cut the marking in pieces: a lone equal character
// between edits counts as rewritten too. Both lists stay ascending.
function absorbCoincidences(removed: number[], added: number[], n: number, m: number) {
  const rm = new Set(removed), ad = new Set(added);
  const extraA: number[] = [], extraB: number[] = [];
  let i = 0, j = 0, edited = false;
  while (i < n || j < m) {
    if (rm.has(i)) { i++; edited = true; continue; }
    if (ad.has(j)) { j++; edited = true; continue; }
    if (i >= n || j >= m) break;
    let len = 0;
    while (i + len < n && j + len < m && !rm.has(i + len) && !ad.has(j + len)) len++;
    const editedAfter = rm.has(i + len) || ad.has(j + len);
    if (len === 1 && edited && editedAfter) { extraA.push(i); extraB.push(j); }
    i += len; j += len; edited = false;
  }
  const merge = (to: number[], extra: number[]) => { to.push(...extra); to.sort((x, y) => x - y); };
  merge(removed, extraA);
  merge(added, extraB);
}

// spans joins ascending indices into runs
function spans(at: number[]): Span[] {
  const out: Span[] = [];
  for (const i of at) {
    const last = out[out.length - 1];
    if (last && last.end === i) last.end = i + 1;
    else out.push({ start: i, end: i + 1 });
  }
  return out;
}

// myers returns the indices of a removed and of b added; null past MAX_EDITS
function myers(a: string[], b: string[]): { removed: number[]; added: number[] } | null {
  const n = a.length, m = b.length, max = n + m;
  if (max === 0) return { removed: [], added: [] };
  const off = max + 1;
  const v = new Int32Array(2 * max + 3);
  // trace[d] is v over k in [-d, d] after d edits
  const trace: Int32Array[] = [];
  for (let d = 0; d <= Math.min(max, MAX_EDITS); d++) {
    for (let k = -d; k <= d; k += 2) {
      let x = k === -d || (k !== d && v[off + k - 1] < v[off + k + 1]) ? v[off + k + 1] : v[off + k - 1] + 1;
      let y = x - k;
      while (x < n && y < m && a[x] === b[y]) { x++; y++; }
      v[off + k] = x;
      if (x >= n && y >= m) return backtrack(trace, d, n, m);
    }
    trace.push(v.slice(off - d, off + d + 1));
  }
  return null;
}

function backtrack(trace: Int32Array[], edits: number, n: number, m: number) {
  const removed: number[] = [], added: number[] = [];
  let x = n, y = m;
  for (let d = edits; d > 0; d--) {
    const prev = trace[d - 1];
    const get = (k: number) => prev[k + d - 1];
    const k = x - y;
    const down = k === -d || (k !== d && get(k - 1) < get(k + 1));
    const pk = down ? k + 1 : k - 1;
    const px = get(pk), py = px - pk;
    if (down) added.push(py); else removed.push(px);
    x = px; y = py;
  }
  return { removed: removed.reverse(), added: added.reverse() };
}

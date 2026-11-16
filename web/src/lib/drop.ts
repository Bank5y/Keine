// Dropping an annotation on the new text (annotation migration): it lands on
// the word under the pointer and covers as much text as it did before, which
// the reader then refines by selection.

const words = typeof Intl !== 'undefined' && 'Segmenter' in Intl ? new Intl.Segmenter('zh', { granularity: 'word' }) : null;

// wordStart is where the word at offset of s begins (UTF-16 offsets, as the
// DOM counts them); off a word (on a space or punctuation), the next word's
// start, or the end of s.
export function wordStart(s: string, offset: number): number {
  if (offset >= s.length) return s.length;
  if (!words) return offset;
  for (const seg of words.segment(s)) {
    const end = seg.index + seg.segment.length;
    if (end > offset && seg.isWordLike) return seg.index;
  }
  return s.length;
}

// dropSpan is the span of len characters from at, shifted back to stay
// within a text of total characters; null in an empty text.
export function dropSpan(at: number, len: number, total: number): { start: number; end: number } | null {
  if (total <= 0 || len <= 0) return null;
  const n = Math.min(len, total);
  const start = Math.max(0, Math.min(at, total - n));
  return { start, end: start + n };
}

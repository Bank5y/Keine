// Quotes: on a wide screen the reader quotes passages of the replies
// into the words of their question, as many as they like, each a tag
// sitting where it was put. A question carries them as selections (选段),
// in order, its text marking where each sits with MARK.

import type { Attachment, Question } from '@/lib/explorations';

// MARK is where a quote sits in a question's text: the object replacement
// character, which nobody types
export const MARK = '\uFFFC';

// how many quotes a question may carry (the server's limit too)
export const MAX_QUOTES = 8;

// Quote is a passage quoted from a reply: its text, the turn, and where it
// sits in the reply's text (start inclusive, end exclusive).
export interface Quote { text: string; turn: number; start: number; end: number }

// Piece is a part of the box's input: words typed, or a quote.
export type Piece = string | Quote;

// toQuestion is the question the box's input asks: the words with a MARK
// for each quote, trimmed at both ends, the quotes attached in order.
export function toQuestion(pieces: Piece[]): Question {
  let text = '';
  const attachments: Attachment[] = [];
  for (const p of pieces) {
    if (typeof p === 'string') text += p.split(MARK).join('');
    else { text += MARK; attachments.push({ kind: 'selection', text: p.text, turn: p.turn, start: p.start, end: p.end }); }
  }
  return { text: text.trim(), attachments };
}

// readQuestion is a question as it shows: each quote back where its mark
// is, and the selections with no mark (carried on a phone, or kept before
// quotes were) above it.
export function readQuestion(q: Question): { above: Attachment[]; pieces: (string | Attachment)[] } {
  const parts = q.text.split(MARK);
  const marked = Math.min(parts.length - 1, q.attachments.length);
  const inline = q.attachments.slice(q.attachments.length - marked);
  const pieces: (string | Attachment)[] = [];
  parts.forEach((t, i) => {
    if (i > 0 && i <= marked) pieces.push(inline[i - 1]);
    if (t) pieces.push(t);
  });
  return { above: q.attachments.slice(0, q.attachments.length - marked), pieces };
}

// a quote this long or shorter, on one line, shows as it is
export const SHORT = 16;

// quoteTag is what a quote's tag says: a word or a phrase as it is; a
// longer passage as its turn (counted in its node, turnNo) and where it
// sits in the reply, R3 #211–241, or its start when that is unknown.
export function quoteTag(a: Attachment, turnNo: (turn: number) => number | null): { short: boolean; label: string } {
  const text = a.text.trim();
  if ([...text].length <= SHORT && !/\n/.test(text)) return { short: true, label: text };
  const n = a.turn == null ? null : turnNo(a.turn);
  if (n != null && a.start != null && a.end != null) return { short: false, label: `R${n} #${a.start + 1}–${a.end}` };
  return { short: false, label: `「${[...text].slice(0, 7).join('')}…」` };
}

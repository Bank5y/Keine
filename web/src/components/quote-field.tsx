import { useEffect, useImperativeHandle, useRef, useState, type Ref } from 'react';
import { TextQuote } from 'lucide-react';
import type { Attachment, Question } from '@/lib/explorations';
import { quoteTag, readQuestion, toQuestion, type Piece, type Quote } from '@/lib/quotes';
import { cn } from '@/lib/utils';

// what a quote's tag says needs its turn's place in its node
export type TurnNo = (turn: number) => number | null;

// FieldHandle is what the box does to its field: put a quote at the caret
// (where it last was in the field, else at the end), empty it, focus it.
export interface FieldHandle { insert: (q: Quote) => void; clear: () => void; focus: () => void }

// a zero-width space keeps a caret room after a tag; it is no part of
// the question
const ROOM = '\u200B';

const ICON = (paths: string[]) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths.map((d) => `<path d="${d}"/>`).join('')}</svg>`;
const QUOTE_ICON = ICON(['M17 6H3', 'M21 12H8', 'M21 18H8', 'M3 12v6']);
const X_ICON = ICON(['M18 6 6 18', 'm6 6 12 12']);

// piecesOf reads the field: its words, and its tags as the quotes they
// stand for; a line break or a line the browser wrapped is a newline.
function piecesOf(root: HTMLElement, quotes: Map<string, Quote>): Piece[] {
  const out: Piece[] = [];
  const walk = (n: Node) => n.childNodes.forEach((c) => {
    if (c.nodeType === Node.TEXT_NODE) { out.push((c.textContent ?? '').split(ROOM).join('')); return; }
    if (!(c instanceof HTMLElement)) return;
    const id = c.dataset.quote;
    if (id) { const q = quotes.get(id); if (q) out.push(q); return; }
    if (c.tagName === 'BR') { out.push('\n'); return; }
    if (c.tagName === 'DIV' || c.tagName === 'P') { if (out.length) out.push('\n'); walk(c); return; }
    walk(c);
  });
  walk(root);
  return out;
}

// QuoteField is the box's field (wide screens): words typed, with the
// passages quoted from replies sitting among them as tags, each where it
// was put; a tag goes with its ✕ or a backspace. Enter sends, Shift+Enter
// starts a new line; pasting pastes text only.
export function QuoteField({ handle, placeholder, label, turnNo, onChange, onEnter }: {
  handle: Ref<FieldHandle>; placeholder: string; label: string; turnNo: TurnNo;
  onChange: (q: Question) => void; onEnter: () => void;
}) {
  const el = useRef<HTMLDivElement>(null);
  const quotes = useRef(new Map<string, Quote>());
  const caret = useRef<Range | null>(null);
  const seq = useRef(0);
  const [empty, setEmpty] = useState(true);

  const read = () => {
    if (!el.current) return;
    const q = toQuestion(piecesOf(el.current, quotes.current));
    setEmpty(!q.text && !q.attachments.length);
    onChange(q);
  };

  // where the caret last was in the field, for a quote taken from a reply:
  // kept as it moves, and at once on each key and press, before a
  // selection made in a reply right after takes its place
  const keep = () => {
    const sel = getSelection();
    if (sel?.rangeCount && el.current?.contains(sel.anchorNode)) caret.current = sel.getRangeAt(0).cloneRange();
  };
  useEffect(() => {
    document.addEventListener('selectionchange', keep);
    return () => document.removeEventListener('selectionchange', keep);
  }, []);

  useImperativeHandle(handle, () => ({
    insert: (q) => {
      const root = el.current;
      if (!root) return;
      const id = `q${++seq.current}`;
      quotes.current.set(id, q);
      const tag = quoteTag({ kind: 'selection', ...q }, turnNo);
      const chip = document.createElement('span');
      chip.className = cn('dlg-quote', tag.short ? 'short' : 'long');
      chip.contentEditable = 'false';
      chip.dataset.quote = id;
      chip.title = q.text;
      chip.innerHTML = `${tag.short ? '' : QUOTE_ICON}<span></span><button type="button" data-unquote aria-label="去掉引用">${X_ICON}</button>`;
      chip.querySelector('span')!.textContent = tag.label;
      const at = caret.current && root.contains(caret.current.startContainer) ? caret.current : document.createRange();
      if (at !== caret.current) { at.selectNodeContents(root); at.collapse(false); }
      at.deleteContents();
      const room = document.createTextNode(ROOM);
      at.insertNode(room);
      at.insertNode(chip);
      const after = document.createRange();
      after.setStart(room, 1);
      after.collapse(true);
      root.focus();
      getSelection()?.removeAllRanges();
      getSelection()?.addRange(after);
      caret.current = after.cloneRange();
      read();
    },
    clear: () => {
      if (el.current) el.current.textContent = '';
      quotes.current.clear();
      caret.current = null;
      read();
    },
    focus: () => el.current?.focus(),
  }));

  return (
    <div ref={el} className={cn('dlg-field', empty && 'blank')} contentEditable suppressContentEditableWarning role="textbox" aria-multiline="true"
      aria-label={label} data-placeholder={placeholder} onInput={() => { keep(); read(); }} onKeyUp={keep} onMouseUp={keep}
      onClick={(e) => {
        const x = (e.target as Element).closest('[data-unquote]');
        if (!x) return;
        x.closest('[data-quote]')?.remove();
        read();
      }}
      onPaste={(e) => {
        e.preventDefault();
        document.execCommand('insertText', false, e.clipboardData.getData('text/plain'));
      }}
      onKeyDown={(e) => {
        if (e.key !== 'Enter' || e.nativeEvent.isComposing) return;
        e.preventDefault();
        if (e.shiftKey) document.execCommand('insertLineBreak');
        else onEnter();
      }} />
  );
}

// QuoteTag is a quote as a question shows it, its whole text on hover.
export function QuoteTag({ a, turnNo }: { a: Attachment; turnNo: TurnNo }) {
  const tag = quoteTag(a, turnNo);
  return <span className={cn('dlg-quote', tag.short ? 'short' : 'long')} title={a.text}>{tag.short ? null : <TextQuote aria-hidden />}<span>{tag.label}</span></span>;
}

// QuotedWords are a question's words with its quotes back where they were
// put; the selections it carried unquoted are shown apart (readQuestion).
export function QuotedWords({ question, turnNo }: { question: Question; turnNo: TurnNo }) {
  const { pieces } = readQuestion(question);
  return <>{pieces.map((p, i) => (typeof p === 'string' ? <span key={i}>{p}</span> : <QuoteTag key={i} a={p} turnNo={turnNo} />))}</>;
}

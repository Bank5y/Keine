// Dragging chapters and lessons in the course editor (desktop only; touch
// and keyboard use the move buttons). A drop sends the same draft operation
// as the buttons would for that move (lessonDrop / chapterDrop).
import { useEffect, useRef, useState, type DragEvent } from 'react';
import { chapterDrop, lessonDrop, type DraftChapter, type Op } from '@/lib/drafts';

type Dragged = { kind: 'chapter' | 'lesson'; key: string };
// Spot is where a drop would land: gap `at` among the chapters, or among
// the lessons of chapter `ci`.
type Spot = { kind: 'chapter'; at: number } | { kind: 'lesson'; ci: number; at: number };

const FINE = '(hover: hover) and (pointer: fine)';

function useFinePointer() {
  const [on, setOn] = useState(() => typeof window !== 'undefined' && !!window.matchMedia?.(FINE).matches);
  useEffect(() => {
    const m = window.matchMedia?.(FINE);
    if (!m) return;
    const onChange = () => setOn(m.matches);
    m.addEventListener('change', onChange);
    return () => m.removeEventListener('change', onChange);
  }, []);
  return on;
}

// Rows the hook finds in the DOM: [data-drag-chapter] blocks, each holding
// [data-drag-lesson] rows, in display order.
const middle = (el: Element) => { const r = el.getBoundingClientRect(); return r.top + r.height / 2; };
const above = (els: Element[], y: number) => els.filter((el) => middle(el) < y).length;

export function useReorderDrag(chapters: DraftChapter[], onMove: (op: Op) => void, disabled: boolean) {
  const fine = useFinePointer();
  const enabled = fine && !disabled;
  const root = useRef<HTMLDivElement>(null);
  const dragged = useRef<Dragged | null>(null);
  const [source, setSource] = useState('');
  const [spot, setSpot] = useState<Spot | null>(null);

  // target is the spot under the pointer and the op a drop there sends;
  // null where a drop would change nothing.
  function target(y: number): { spot: Spot; op: Op } | null {
    const d = dragged.current;
    if (!d || !root.current) return null;
    const blocks = [...root.current.querySelectorAll('[data-drag-chapter]')];
    if (!blocks.length) return null;
    if (d.kind === 'chapter') {
      const at = above(blocks, y);
      const op = chapterDrop(chapters, d.key, at);
      return op && { spot: { kind: 'chapter', at }, op };
    }
    let ci = blocks.findIndex((el) => el.getBoundingClientRect().bottom >= y);
    if (ci < 0) ci = blocks.length - 1;
    const at = above([...blocks[ci].querySelectorAll('[data-drag-lesson]')], y);
    const op = chapters[ci] ? lessonDrop(chapters, d.key, chapters[ci].key, at) : null;
    return op && { spot: { kind: 'lesson', ci, at }, op };
  }

  const end = () => { dragged.current = null; setSource(''); setSpot(null); };

  const rootProps = {
    ref: root,
    onDragOver(e: DragEvent) {
      if (!dragged.current) return;
      const t = target(e.clientY);
      if (t) { e.preventDefault(); e.dataTransfer.dropEffect = 'move'; }
      setSpot((s) => (t && s && JSON.stringify(s) === JSON.stringify(t.spot) ? s : t?.spot ?? null));
    },
    onDragLeave(e: DragEvent) {
      if (!root.current?.contains(e.relatedTarget as Node | null)) setSpot(null);
    },
    onDrop(e: DragEvent) {
      if (!dragged.current) return;
      e.preventDefault();
      const t = target(e.clientY);
      end();
      if (t) onMove(t.op);
    },
  };

  const handle = (kind: Dragged['kind'], key: string) => (enabled ? {
    draggable: true,
    onDragStart(e: DragEvent) {
      dragged.current = { kind, key };
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', key);
      setSource(key);
    },
    onDragEnd: end,
  } : {});

  // Classes marking the drop line: before or after a row, or into an empty
  // chapter; and the row being dragged.
  const chapterMark = (ci: number) => {
    if (spot?.kind !== 'chapter') return '';
    if (spot.at === ci) return 'drop-before';
    return spot.at === chapters.length && ci === chapters.length - 1 ? 'drop-after' : '';
  };
  const lessonMark = (ci: number, li: number) => {
    if (spot?.kind !== 'lesson' || spot.ci !== ci) return '';
    if (spot.at === li) return 'drop-before';
    const n = chapters[ci].lessons.length;
    return spot.at === n && li === n - 1 ? 'drop-after' : '';
  };
  const emptyMark = (ci: number) => (spot?.kind === 'lesson' && spot.ci === ci && !chapters[ci].lessons.length ? 'drop-into' : '');

  return {
    enabled,
    rootProps,
    chapterHandle: (key: string) => handle('chapter', key),
    lessonHandle: (key: string) => handle('lesson', key),
    dragging: (key: string) => source === key,
    chapterMark,
    lessonMark,
    emptyMark,
  };
}

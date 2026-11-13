import { useState, type ReactNode } from 'react';
import { cn } from '@/lib/utils';

// how long a chapter takes to open; matches .fold in styles.css
export const FOLD_MS = 200;

// useChapterFolds tracks which chapters of a table of contents are open.
// Several can be open at once; nothing is remembered between visits.
export function useChapterFolds(initial: () => Iterable<number>) {
  const [open, setOpen] = useState(() => new Set(initial()));
  return {
    isOpen: (id: number) => open.has(id),
    toggle: (id: number) => setOpen((s) => { const n = new Set(s); if (!n.delete(id)) n.add(id); return n; }),
    expand: (id: number) => setOpen((s) => (s.has(id) ? s : new Set(s).add(id))),
  };
}

// Fold shows or hides a chapter's lessons with a height animation; hidden
// lessons stay in the page but cannot be focused.
export function Fold({ open, children }: { open: boolean; children: ReactNode }) {
  return (
    <div className={cn('fold', open && 'open')} inert={!open}>
      <div className="fold-inner">{children}</div>
    </div>
  );
}

import type { ReactNode } from 'react';
import { Dialog as D } from 'radix-ui';

// Dialog is the one modal shape of the app: a paper card with a brand rule.
// wide="reader" fits a lesson as the reader shows it.
export function Dialog({ open, onOpenChange, title, children, wide }: { open: boolean; onOpenChange: (v: boolean) => void; title: string; children: ReactNode; wide?: boolean | 'reader' }) {
  return (
    <D.Root open={open} onOpenChange={onOpenChange}>
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-40 bg-black/30 data-[state=open]:animate-in data-[state=open]:fade-in-0" />
        <D.Content
          aria-describedby={undefined}
          className={'modal fixed top-1/2 left-1/2 z-50 max-h-[calc(100vh-40px)] w-[calc(100vw-32px)] -translate-x-1/2 -translate-y-1/2 overflow-auto rounded-[var(--radius-card)] border border-border bg-surface p-7 shadow-[0_1px_2px_rgb(0_0_0/.05),0_24px_56px_-24px_rgb(0_0_0/.3)] outline-none data-[state=open]:animate-in data-[state=open]:fade-in-0 ' + (wide === 'reader' ? 'max-w-[780px]' : wide ? 'max-w-[560px]' : 'max-w-[420px]')}
        >
          <D.Title>{title}</D.Title>
          {children}
        </D.Content>
      </D.Portal>
    </D.Root>
  );
}

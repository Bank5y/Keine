import type { ReactNode } from 'react';
import { Dialog as D } from 'radix-ui';
import { cn } from '@/lib/utils';

// Drawer slides a panel in from one side on narrow screens.
export function Drawer({ open, onOpenChange, side, title, children }: { open: boolean; onOpenChange: (v: boolean) => void; side: 'left' | 'right'; title: string; children: ReactNode }) {
  return (
    <D.Root open={open} onOpenChange={onOpenChange}>
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-40 bg-black/40 data-[state=open]:animate-in data-[state=open]:fade-in-0" />
        <D.Content
          aria-describedby={undefined}
          className={cn(
            'fixed top-0 bottom-0 z-50 w-[min(340px,86vw)] overflow-y-auto bg-surface p-5 shadow-xl outline-none data-[state=open]:animate-in',
            side === 'left' ? 'left-0 border-r border-rule data-[state=open]:slide-in-from-left' : 'right-0 border-l border-rule data-[state=open]:slide-in-from-right',
          )}
        >
          <D.Title className="kicker mb-4">{title}</D.Title>
          {children}
        </D.Content>
      </D.Portal>
    </D.Root>
  );
}

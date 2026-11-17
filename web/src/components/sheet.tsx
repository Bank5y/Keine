import type { ReactNode } from 'react';
import { Dialog as D } from 'radix-ui';

// Sheet rises from the bottom of a narrow screen with a short list of
// choices; tapping the backdrop or 取消 closes it.
export function Sheet({ open, onOpenChange, title, children }: { open: boolean; onOpenChange: (v: boolean) => void; title: string; children: ReactNode }) {
  return (
    <D.Root open={open} onOpenChange={onOpenChange}>
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-40 bg-black/40 data-[state=open]:animate-in data-[state=open]:fade-in-0" />
        <D.Content aria-describedby={undefined} className="sheet data-[state=open]:animate-in data-[state=open]:slide-in-from-bottom">
          <span className="sheet-grip" aria-hidden="true" />
          <D.Title className="sheet-title">{title}</D.Title>
          {children}
          <D.Close className="sheet-cancel">取消</D.Close>
        </D.Content>
      </D.Portal>
    </D.Root>
  );
}

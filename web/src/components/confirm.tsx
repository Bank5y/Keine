import { useState, type ReactNode } from 'react';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';

// Confirm asks before an action that is hard to undo.
export function Confirm({ open, title, children, confirm, onConfirm, onClose }: {
  open: boolean; title: string; children: ReactNode; confirm: string; onConfirm: () => Promise<void> | void; onClose: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const run = async () => { setBusy(true); try { await onConfirm(); } finally { setBusy(false); } };
  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v && !busy) onClose(); }} title={title}>
      <p>{children}</p>
      <div className="head-actions">
        <Button onClick={onClose} disabled={busy}>取消</Button>
        <Button variant="default" onClick={run} disabled={busy}>{confirm}</Button>
      </div>
    </Dialog>
  );
}

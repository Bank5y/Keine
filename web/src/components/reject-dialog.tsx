import { useEffect, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';

// RejectDialog collects the reason a request is turned down; the requester
// reads it, so it cannot be empty.
export function RejectDialog({ open, title, onReject, onClose }: { open: boolean; title: string; onReject: (reason: string) => Promise<void>; onClose: () => void }) {
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => { if (open) setReason(''); }, [open]);
  const run = async () => { setBusy(true); try { await onReject(reason.trim()); } finally { setBusy(false); } };
  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v && !busy) onClose(); }} title={title}>
      <p className="mt-2">驳回理由会展示给申请人, 请写清楚需要修改的地方。</p>
      <textarea className="textarea" rows={4} autoFocus value={reason} onChange={(e) => setReason(e.target.value)} placeholder="例如: 第 3 章的分组不对, 第 15 讲缺少图片" />
      <div className="head-actions mt-4">
        <Button onClick={onClose} disabled={busy}>取消</Button>
        <Button variant="default" onClick={run} disabled={busy || !reason.trim()}>驳回</Button>
      </div>
    </Dialog>
  );
}

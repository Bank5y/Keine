import { useState } from 'react';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { describeOp, type Draft } from '@/lib/drafts';
import { cn } from '@/lib/utils';

// CommitDialog asks before committing a draft, listing every operation in
// order, one per step as undo takes them back. An operation that no longer
// applies stays in its place, struck through, with why it will be skipped.
export function CommitDialog({ open, draft, onCommit, onClose }: {
  open: boolean; draft: Draft; onCommit: () => Promise<void>; onClose: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const run = async () => { setBusy(true); try { await onCommit(); } finally { setBusy(false); } };
  const changes = draft.ops.length;
  const conflicts = draft.conflicts ?? [];
  const why = new Map(conflicts.map((x) => [x.id, x.message]));
  const rest = changes - conflicts.length;
  const effect = draft.mode === 'request' ? '会成为一份更新申请, 管理员批准后生效' : '会作为一次课程更新生效';
  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v && !busy) onClose(); }} title="提交编辑稿" wide>
      <p className="op-list-lead">编辑稿里的 {changes} 处修改:</p>
      <ol className="op-list">
        {draft.ops.map((op, i) => {
          const skip = op.id ? why.get(op.id) : undefined;
          return (
            <li key={op.id ?? i} className={cn(skip !== undefined && 'skipped')}>
              <span className="op-what">{describeOp(op, draft.course.chapters)}</span>
              {skip !== undefined ? <span className="op-why">将跳过 · {skip}</span> : null}
            </li>
          );
        })}
      </ol>
      <p>
        {!conflicts.length
          ? (draft.mode === 'request'
            ? <>这 {changes} 处修改{effect}; 你对这门课还在审核中的申请会被替换。</>
            : <>这 {changes} 处修改{effect}。学习进度按讲保留, 订阅者会收到一条「课程有更新」。</>)
          : !rest
            ? '课程在你编辑期间被更新过, 编辑稿里的修改都已无法应用, 没有可提交的内容; 请撤销或放弃编辑稿。'
            : <>课程在你编辑期间被更新过。跳过 {conflicts.length} 处无法应用的修改, 其余 {rest} 处{effect}。</>}
      </p>
      <div className="head-actions">
        <Button onClick={onClose} disabled={busy}>取消</Button>
        <Button variant="default" onClick={run} disabled={busy || (conflicts.length > 0 && !rest)}>{conflicts.length ? '跳过这些修改并提交' : '提交'}</Button>
      </div>
    </Dialog>
  );
}

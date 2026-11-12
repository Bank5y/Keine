import { Check } from 'lucide-react';
import { STATE_LABEL, percent, type LessonState } from '@/lib/courses';
import { cn } from '@/lib/utils';

export function ProgressBar({ done, total, className }: { done: number; total: number; className?: string }) {
  const p = percent(done, total);
  return (
    <div className={cn('pbar', className)} role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={p} aria-label={`已学 ${done} / ${total}`}>
      <i style={{ width: `${p}%` }} />
    </div>
  );
}

// StateMark shows a lesson's state: nothing for unstarted, a dot while
// studying, a check once done.
export function StateMark({ state, compact }: { state: LessonState; compact?: boolean }) {
  if (state === 'unstarted') return compact ? <span className="sm-none" aria-label={STATE_LABEL.unstarted} /> : <span className="smark unstarted">{STATE_LABEL.unstarted}</span>;
  if (state === 'done') return <span className={cn('smark done', compact && 'compact')} title={STATE_LABEL.done}><Check />{compact ? null : STATE_LABEL.done}</span>;
  return <span className={cn('smark studying', compact && 'compact')} title={STATE_LABEL.studying}>{compact ? null : STATE_LABEL.studying}</span>;
}

import { describeOp, type Conflict } from '@/lib/drafts';

// ConflictList lists a draft's operations that no longer apply, each with
// its step number and the reason. Spans, so it may sit inside a <p>.
export function ConflictList({ conflicts }: { conflicts: Conflict[] }) {
  return (
    <span className="conflict-list">
      {conflicts.map((x) => (
        <span className="conflict" key={x.id}>
          <span className="c-step">第 {x.index + 1} 步</span>
          <span className="c-what">{describeOp(x.op)}</span>
          <span className="c-why">{x.message}</span>
        </span>
      ))}
    </span>
  );
}

import { describeOp, type Conflict, type DraftChapter } from '@/lib/drafts';

// ConflictList lists a draft's operations that no longer apply, each with
// its step number and the reason. Spans, so it may sit inside a <p>.
// chapters: the draft's course, to name where a lesson moves to.
export function ConflictList({ conflicts, chapters }: { conflicts: Conflict[]; chapters?: DraftChapter[] }) {
  return (
    <span className="conflict-list">
      {conflicts.map((x) => (
        <span className="conflict" key={x.id}>
          <span className="c-step">第 {x.index + 1} 步</span>
          <span className="c-what">{describeOp(x.op, chapters)}</span>
          <span className="c-why">{x.message}</span>
        </span>
      ))}
    </span>
  );
}

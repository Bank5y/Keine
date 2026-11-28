import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { api, ApiError } from '@/lib/api';
import { runInTurn } from '@/lib/batch';
import { PUBLISH_LABEL, type CourseSummary } from '@/lib/courses';

// PublishDialog publishes several of the viewer's private courses at once
// (see publishTargets): an admin puts them straight into the library;
// anyone else submits a publish request for each. Every course is ticked
// to begin with. A course that fails keeps its tick and shows why, so
// 公开 again tries just those; the dialog closes once all went through.
// onPublished hears of each round that changed at least one course.
export function PublishDialog({ open, courses, admin, onClose, onPublished }: {
  open: boolean; courses: CourseSummary[]; admin: boolean; onClose: () => void; onPublished: () => void;
}) {
  const [picked, setPicked] = useState<Set<number>>(new Set());
  const [failed, setFailed] = useState<Map<number, string>>(new Map());
  const [busy, setBusy] = useState(false);

  useEffect(() => { if (open) { setPicked(new Set(courses.map((c) => c.id))); setFailed(new Map()); } }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  // a course that left the list (published, deleted) is no longer picked
  const chosen = courses.filter((c) => picked.has(c.id));
  const all = chosen.length === courses.length && courses.length > 0;
  const toggle = (id: number) => setPicked((p) => { const n = new Set(p); if (n.has(id)) n.delete(id); else n.add(id); return n; });
  const verb = admin ? '公开' : '申请公开';

  async function run() {
    setBusy(true);
    try {
      const results = await runInTurn(chosen, (c) => api('POST', admin ? `/api/admin/courses/${c.id}/publish` : `/api/courses/${c.id}/publish-request`));
      const bad = new Map(results.flatMap((r) => (r.ok ? [] : [[r.item.id, r.error instanceof ApiError ? r.error.message : '操作失败'] as const])));
      const done = results.length - bad.size;
      setFailed(bad);
      setPicked(new Set(bad.keys()));
      if (done) onPublished();
      if (!bad.size) {
        toast.success(admin ? `已公开 ${done} 门课程到课程库` : `已为 ${done} 门课程提交公开申请, 等待管理员审核`);
        onClose();
      } else {
        toast.error(`${done ? `已${verb} ${done} 门, ` : ''}${bad.size} 门课程没有${verb}`);
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v && !busy) onClose(); }} title={admin ? '批量公开' : '批量申请公开'} wide>
      <p className="mt-2">
        {admin ? '勾选的课程会直接进入课程库, 所有用户都能看到; 正在审核的公开申请随之批准。' : '勾选的课程各自提交一份公开申请, 由管理员审核; 审核期间课程仍是你的私有课程。'}
      </p>
      <label className="check mt-4">
        <input type="checkbox" checked={all} disabled={busy || !courses.length}
          onChange={(e) => setPicked(e.target.checked ? new Set(courses.map((c) => c.id)) : new Set())} />
        全选 ({chosen.length} / {courses.length})
      </label>
      <ul className="publish-list">
        {courses.map((c) => (
          <li key={c.id}>
            <label className="check">
              <input type="checkbox" checked={picked.has(c.id)} disabled={busy} onChange={() => toggle(c.id)} />
              <span className="name">{c.title}</span>
              <span className="state">{PUBLISH_LABEL[c.publishState]}</span>
            </label>
            {failed.has(c.id) ? <div className="fail" role="alert">{failed.get(c.id)}</div> : null}
          </li>
        ))}
      </ul>
      <div className="head-actions mt-5">
        <Button onClick={onClose} disabled={busy}>取消</Button>
        <Button variant="default" onClick={run} disabled={busy || !chosen.length}>{chosen.length ? `${verb} ${chosen.length} 门` : verb}</Button>
      </div>
    </Dialog>
  );
}

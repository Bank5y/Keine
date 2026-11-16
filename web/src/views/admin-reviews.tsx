import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Empty, ErrorBox } from '@/components/common';
import { RejectDialog } from '@/components/reject-dialog';
import { api, ApiError } from '@/lib/api';
import { CHANGE_LABEL, TYPE_LABEL, type ChangeKind, type Changes, type Review } from '@/lib/courses';
import { Link } from '@/lib/router';

const KIND_LABEL: Record<Review['kind'], string> = { publish: '公开申请', update: '更新申请' };
const fmtTime = (s: string) => new Date(s).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });

// AdminReviews is the queue of requests waiting for an admin.
export function AdminReviews() {
  const [reviews, setReviews] = useState<Review[] | null>(null);
  const [err, setErr] = useState('');
  const [rejecting, setRejecting] = useState<Review | null>(null);
  const load = () => api<{ reviews: Review[] }>('GET', '/api/admin/reviews').then((r) => setReviews(r.reviews)).catch((e) => setErr(e.message));
  useEffect(() => { void load(); }, []);

  const decide = async (r: Review, verb: 'approve' | 'reject', reason?: string) => {
    try {
      await api('POST', `/api/admin/reviews/${r.id}/${verb}`, reason === undefined ? undefined : { reason });
      toast.success(verb === 'approve' ? '已批准' : '已驳回');
      setRejecting(null);
      void load();
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '操作失败');
    }
  };

  return (
    <>
      <div className="result-bar" style={{ marginTop: 0 }}>
        <span className="count">{reviews ? <><b>{reviews.length}</b>个待审核的申请</> : '正在加载…'}</span>
      </div>
      {err ? <ErrorBox message={err} /> : null}
      {reviews && !reviews.length ? <Empty glyph="闲" title="没有待审核的申请" /> : null}
      <div className="ledger">
        {(reviews || []).map((r) => (
          <div className="ledger-row review-row" key={r.id}>
            <span className="name">
              <span className="kind">{KIND_LABEL[r.kind]}</span>
              <Link href={`/courses/${r.course.id}`} className="hover:text-brand">{r.course.title}</Link>
              {r.changes ? <ChangeCounts c={r.changes} /> : null}
              {r.stale ? <span className="stale">课程已更新, 请基于新内容重新审核</span> : null}
            </span>
            <span className="meta">{TYPE_LABEL[r.course.type]} · {r.course.lessonCount} 讲</span>
            <span className="meta">{r.requester.username} · {fmtTime(r.createdAt)}</span>
            <span className="acts">
              <Button size="sm" variant="default" onClick={() => decide(r, 'approve')}>批准</Button>
              <Button size="sm" onClick={() => setRejecting(r)}>驳回</Button>
            </span>
            {r.changes ? <ChangeList c={r.changes} /> : null}
          </div>
        ))}
      </div>
      <RejectDialog open={!!rejecting} title={rejecting ? `驳回「${rejecting.course.title}」` : ''} onClose={() => setRejecting(null)}
        onReject={(reason) => decide(rejecting!, 'reject', reason)} />
    </>
  );
}

function ChangeCounts({ c }: { c: Changes }) {
  return (
    <span className="changes">
      <span className="add">+{c.added}</span>
      <span className="del">-{c.removed}</span>
      {c.modified ? <span>修改 {c.modified}</span> : null}
      {c.renamed ? <span>改名 {c.renamed}</span> : null}
      {c.moved ? <span>移动 {c.moved}</span> : null}
      <span>保留 {c.kept}</span>
    </span>
  );
}

function Tags({ kinds }: { kinds: ChangeKind[] }) {
  return <>{kinds.map((k) => <span key={k} className={'chg ' + k}>{CHANGE_LABEL[k]}</span>)}</>;
}

// ChangeList shows every change of an update request, chapter by chapter.
function ChangeList({ c }: { c: Changes }) {
  if (!c.chapters.length) return <div className="review-changes rc-none">章与讲没有变化, 只改了课程信息。</div>;
  return (
    <ul className="review-changes" aria-label="变化">
      {c.chapters.map((ch) => (
        <li key={ch.key} className="rc-chapter">
          <div className="rc-head">
            <span className="rc-title">{ch.title}</span>
            <Tags kinds={ch.changes} />
            {ch.previousTitle ? <span className="rc-was">原名「{ch.previousTitle}」</span> : null}
          </div>
          {ch.lessons.length ? (
            <ul>
              {ch.lessons.map((l) => (
                <li key={l.key} className="rc-lesson">
                  <span className={l.changes.includes('removed') ? 'rc-title gone' : 'rc-title'}>{l.title}</span>
                  {l.upcoming ? <span className="rc-was">待更新</span> : null}
                  <Tags kinds={l.changes} />
                  {l.previousTitle ? <span className="rc-was">原名「{l.previousTitle}」</span> : null}
                  {l.fromChapter ? <span className="rc-was">从「{l.fromChapter}」移来</span> : null}
                </li>
              ))}
            </ul>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

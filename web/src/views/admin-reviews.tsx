import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Empty, ErrorBox } from '@/components/common';
import { RejectDialog } from '@/components/reject-dialog';
import { api, ApiError } from '@/lib/api';
import { TYPE_LABEL, type Review } from '@/lib/courses';
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
              {r.changes ? (
                <span className="changes" title={[...r.changes.added.map((t) => '+ ' + t), ...r.changes.removed.map((t) => '- ' + t)].join('\n')}>
                  <span className="add">+{r.changes.added.length}</span>
                  <span className="del">-{r.changes.removed.length}</span>
                  <span>保留 {r.changes.kept}</span>
                </span>
              ) : null}
              {r.stale ? <span className="stale">课程已更新, 请基于新内容重新审核</span> : null}
            </span>
            <span className="meta">{TYPE_LABEL[r.course.type]} · {r.course.lessonCount} 讲</span>
            <span className="meta">{r.requester.username} · {fmtTime(r.createdAt)}</span>
            <span className="acts">
              <Button size="sm" variant="default" onClick={() => decide(r, 'approve')}>批准</Button>
              <Button size="sm" onClick={() => setRejecting(r)}>驳回</Button>
            </span>
          </div>
        ))}
      </div>
      <RejectDialog open={!!rejecting} title={rejecting ? `驳回「${rejecting.course.title}」` : ''} onClose={() => setRejecting(null)}
        onReject={(reason) => decide(rejecting!, 'reject', reason)} />
    </>
  );
}

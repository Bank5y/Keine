import { useEffect, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Confirm } from '@/components/confirm';
import { ErrorBox } from '@/components/common';
import { RejectDialog } from '@/components/reject-dialog';
import { UploadDialog } from '@/components/upload-dialog';
import { api, ApiError } from '@/lib/api';
import { COMPLETION_LABEL, type Completion, type CourseDetail, type Review, type UpdateRequest } from '@/lib/courses';
import { navigate } from '@/lib/router';
import { useUser } from '@/lib/session';

const fail = (ex: unknown) => toast.error(ex instanceof ApiError ? ex.message : '操作失败');

// CourseActions offers what the viewer may do with a course in its current
// state: the uploader asks for it to be published (or withdraws, or asks
// again after a rejection); an admin reviews, publishes, unpublishes, edits
// and deletes.
export function CourseActions({ c, onChange }: { c: CourseDetail; onChange: () => void }) {
  const user = useUser();
  const admin = user?.role === 'admin';
  const [dialog, setDialog] = useState<'' | 'unpublish' | 'delete' | 'edit' | 'reject' | 'update' | 'request'>('');
  const [requests, setRequests] = useState<UpdateRequest[]>([]);
  const [review, setReview] = useState<Review | null>(null);

  // an admin looking at a course under review can decide it right here
  useEffect(() => {
    if (!admin || c.publishState !== 'pending' || c.visibility !== 'private') { setReview(null); return; }
    api<{ reviews: Review[] }>('GET', '/api/admin/reviews').then((r) => setReview(r.reviews.find((x) => x.course.id === c.id && x.kind === 'publish') || null)).catch(() => {});
  }, [admin, c.id, c.publishState, c.visibility]);

  // the viewer's own update requests, for a public course
  const loadRequests = () => api<{ requests: UpdateRequest[] }>('GET', `/api/courses/${c.id}/update-requests`).then((r) => setRequests(r.requests)).catch(() => {});
  useEffect(() => { if (c.visibility === 'public') void loadRequests(); }, [c.id, c.visibility]); // eslint-disable-line react-hooks/exhaustive-deps

  const act = async (method: string, path: string, done: string, body?: unknown) => {
    try { await api(method, path, body); toast.success(done); setDialog(''); onChange(); } catch (ex) { fail(ex); }
  };
  const isPrivate = c.visibility === 'private';
  const canRequest = !isPrivate && !admin && (c.isUploader || c.completion !== 'finished');
  const pending = requests.find((r) => r.state === 'pending');
  const latest = requests[0];

  return (
    <div className="stack">
      {isPrivate && c.isUploader && c.publishState === 'rejected' ? (
        <div className="reject-note"><b>公开申请被驳回</b>{c.rejectReason ? <p>{c.rejectReason}</p> : null}</div>
      ) : null}
      {isPrivate && c.isUploader && c.publishState !== 'pending' && !admin ? (
        <Button onClick={() => act('POST', `/api/courses/${c.id}/publish-request`, '已提交公开申请, 等待管理员审核')}>
          {c.publishState === 'rejected' ? '再次申请公开' : '申请公开'}
        </Button>
      ) : null}
      {isPrivate && c.isUploader ? (
        <Button variant="ghost" onClick={() => setDialog('update')}>更新课程</Button>
      ) : null}
      {isPrivate && c.isUploader && c.publishState === 'pending' ? (
        <Button variant="ghost" onClick={() => act('DELETE', `/api/courses/${c.id}/publish-request`, '已撤回公开申请')}>撤回公开申请</Button>
      ) : null}
      {review ? (
        <div className="review-box">
          <div className="t">{review.requester.username} 申请公开这门课程</div>
          <div className="row">
            <Button variant="default" size="sm" onClick={() => act('POST', `/api/admin/reviews/${review.id}/approve`, '已批准, 课程进入课程库')}>批准</Button>
            <Button size="sm" onClick={() => setDialog('reject')}>驳回</Button>
          </div>
        </div>
      ) : null}
      {canRequest && !pending ? (
        <Button variant="ghost" onClick={() => setDialog('request')}>{c.completion === 'incomplete' && !c.isUploader ? '申请补全' : '提交更新'}</Button>
      ) : null}
      {pending ? (
        <div className="request-note">
          <b>更新申请审核中</b>
          <Button variant="text" size="bare" onClick={() => act('DELETE', `/api/courses/${c.id}/update-requests/${pending.id}`, '已撤回更新申请').then(loadRequests)}>撤回</Button>
        </div>
      ) : latest && latest.state === 'rejected' ? (
        <div className="reject-note"><b>更新申请被驳回</b><p>{latest.reason}</p></div>
      ) : null}
      {admin && !isPrivate ? <Button variant="ghost" onClick={() => setDialog('update')}>更新课程</Button> : null}
      {admin && isPrivate && !review ? (
        <Button onClick={() => act('POST', `/api/admin/courses/${c.id}/publish`, '已公开到课程库')}>直接公开</Button>
      ) : null}
      {admin ? (
        <div className="admin-tools">
          {!isPrivate ? <Button variant="text" size="bare" onClick={() => setDialog('edit')}>编辑信息</Button> : null}
          {!isPrivate ? <Button variant="text" size="bare" onClick={() => setDialog('unpublish')}>下架</Button> : null}
          <Button variant="text" size="bare" onClick={() => setDialog('delete')}>删除课程</Button>
        </div>
      ) : null}

      <Confirm open={dialog === 'unpublish'} title="下架课程" confirm="下架" onClose={() => setDialog('')}
        onConfirm={() => act('POST', `/api/admin/courses/${c.id}/unpublish`, '已下架, 课程回到上传者的私有课程')}>
        「{c.title}」会从课程库移除, 变回上传者的私有课程。其他人的学习记录会保留, 但他们将看不到这门课。
      </Confirm>
      <Confirm open={dialog === 'delete'} title="删除课程" confirm="删除" onClose={() => setDialog('')}
        onConfirm={async () => { try { await api('DELETE', `/api/courses/${c.id}`); toast.success('已删除'); navigate('/library'); } catch (ex) { fail(ex); } }}>
        确定删除「{c.title}」吗? 课程内容、文件和所有人的学习记录都会被删除, 无法恢复。
      </Confirm>
      <RejectDialog open={dialog === 'reject'} title="驳回公开申请" onClose={() => setDialog('')}
        onReject={(reason) => act('POST', `/api/admin/reviews/${review?.id}/reject`, '已驳回', { reason })} />
      <UploadDialog courseId={c.id} open={dialog === 'update'} onClose={() => setDialog('')} onUploaded={() => { setDialog(''); onChange(); }} />
      <UploadDialog courseId={c.id} request open={dialog === 'request'} onClose={() => setDialog('')} onUploaded={() => { setDialog(''); void loadRequests(); }} />
      <EditDialog c={c} open={dialog === 'edit'} onClose={() => setDialog('')} onSaved={() => { setDialog(''); onChange(); }} />
    </div>
  );
}

function EditDialog({ c, open, onClose, onSaved }: { c: CourseDetail; open: boolean; onClose: () => void; onSaved: () => void }) {
  const [f, setF] = useState({ title: c.title, intro: c.intro, authorName: c.authorName, authorIntro: c.authorIntro, completion: c.completion });
  const [err, setErr] = useState('');
  useEffect(() => { if (open) { setF({ title: c.title, intro: c.intro, authorName: c.authorName, authorIntro: c.authorIntro, completion: c.completion }); setErr(''); } }, [open, c]);
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });

  async function submit(e: FormEvent) {
    e.preventDefault();
    try { await api('PATCH', `/api/admin/courses/${c.id}`, f); toast.success('已保存'); onSaved(); } catch (ex) { setErr(ex instanceof ApiError ? ex.message : '保存失败'); }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) onClose(); }} title="编辑课程信息" wide>
      <form onSubmit={submit} className="mt-4">
        {err ? <ErrorBox message={err} /> : null}
        <div className="field"><label htmlFor="et">课程名</label><Input id="et" value={f.title} onChange={set('title')} /></div>
        <div className="field"><label htmlFor="ec">完结状态</label>
          <select id="ec" className="select" value={f.completion} onChange={set('completion')}>
            {(Object.keys(COMPLETION_LABEL) as Completion[]).map((k) => <option key={k} value={k}>{COMPLETION_LABEL[k]}</option>)}
          </select>
        </div>
        <div className="field"><label htmlFor="ea">讲师</label><Input id="ea" value={f.authorName} onChange={set('authorName')} /></div>
        <div className="field"><label htmlFor="eai">讲师简介</label><Input id="eai" value={f.authorIntro} onChange={set('authorIntro')} /></div>
        <div className="field"><label htmlFor="ei">课程介绍 (Markdown)</label><textarea id="ei" className="textarea" rows={6} value={f.intro} onChange={set('intro')} /></div>
        <div className="head-actions">
          <Button onClick={onClose}>取消</Button>
          <Button type="submit" variant="default" disabled={!f.title.trim()}>保存</Button>
        </div>
      </form>
    </Dialog>
  );
}

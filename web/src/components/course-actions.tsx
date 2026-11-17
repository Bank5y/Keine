import { useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { ArrowDown, Check, Ellipsis, FilePenLine, Globe, PenLine, Pencil, Send, Settings, Trash2, Undo2, X, type LucideIcon } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Confirm } from '@/components/confirm';
import { ErrorBox } from '@/components/common';
import { RejectDialog } from '@/components/reject-dialog';
import { Sheet } from '@/components/sheet';
import { UploadDialog } from '@/components/upload-dialog';
import { api, ApiError } from '@/lib/api';
import { courseActions, menuActions, type CourseAction } from '@/lib/course-actions';
import { COMPLETION_LABEL, type Completion, type CourseDetail, type Review, type UpdateRequest } from '@/lib/courses';
import { Link, navigate } from '@/lib/router';
import { useUser } from '@/lib/session';
import { cn } from '@/lib/utils';

const fail = (ex: unknown) => toast.error(ex instanceof ApiError ? ex.message : '操作失败');

// useCourseActions holds what the course's actions need, for the desktop
// colophon and the narrow ⋯ menu alike: the actions the viewer has
// (courseActions), what each one does, the notes on earlier requests, and
// the dialogs some of them open (rendered with `dialogs`).
function useCourseActions(c: CourseDetail, onChange: () => void) {
  const user = useUser();
  const admin = user?.role === 'admin';
  const [dialog, setDialog] = useState<'' | 'unpublish' | 'delete' | 'edit' | 'reject' | 'request'>('');
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
  const pending = requests.find((r) => r.state === 'pending');
  const latest = requests[0];
  const actions = courseActions(c, { admin, reviewing: !!review, updatePending: !!pending });

  const run = {
    publishRequest: () => act('POST', `/api/courses/${c.id}/publish-request`, '已提交公开申请, 等待管理员审核'),
    withdrawPublish: () => act('DELETE', `/api/courses/${c.id}/publish-request`, '已撤回公开申请'),
    approve: () => act('POST', `/api/admin/reviews/${review?.id}/approve`, '已批准, 课程进入课程库'),
    withdrawUpdate: () => act('DELETE', `/api/courses/${c.id}/update-requests/${pending?.id}`, '已撤回更新申请').then(loadRequests),
    directPublish: () => act('POST', `/api/admin/courses/${c.id}/publish`, '已公开到课程库'),
  };

  const notes = {
    publishRejected: c.visibility === 'private' && c.isUploader && c.publishState === 'rejected'
      ? <div className="reject-note"><b>公开申请被驳回</b>{c.rejectReason ? <p>{c.rejectReason}</p> : null}</div> : null,
    updateRejected: !pending && latest?.state === 'rejected'
      ? <div className="reject-note"><b>更新申请被驳回</b><p>{latest.reason}</p></div> : null,
  };

  const dialogs = (
    <>
      <Confirm open={dialog === 'unpublish'} title="下架课程" confirm="下架" onClose={() => setDialog('')}
        onConfirm={() => act('POST', `/api/admin/courses/${c.id}/unpublish`, '已下架, 课程回到上传者的私有课程')}>
        「{c.title}」会从课程库移除, 变回上传者的私有课程。订阅者仍可阅读各自保留的讲 (已学完或有标注的讲), 其余的讲看不到。
      </Confirm>
      <Confirm open={dialog === 'delete'} title="删除课程" confirm="删除" onClose={() => setDialog('')}
        onConfirm={async () => { try { await api('DELETE', `/api/courses/${c.id}`); toast.success('已删除'); navigate('/library'); } catch (ex) { fail(ex); } }}>
        确定删除「{c.title}」吗? 课程会从课程库和上传者那里消失, 无法恢复; 订阅者仍可阅读各自保留的讲 (已学完或有标注的讲)。
      </Confirm>
      <RejectDialog open={dialog === 'reject'} title="驳回公开申请" onClose={() => setDialog('')}
        onReject={(reason) => act('POST', `/api/admin/reviews/${review?.id}/reject`, '已驳回', { reason })} />
      <UploadDialog courseId={c.id} request open={dialog === 'request'} onClose={() => setDialog('')} onUploaded={() => { setDialog(''); void loadRequests(); }} />
      <EditDialog c={c} open={dialog === 'edit'} onClose={() => setDialog('')} onSaved={() => { setDialog(''); onChange(); }} />
    </>
  );

  return { actions, has: (a: CourseAction) => actions.includes(a), review, run, setDialog, notes, dialogs };
}

const publishLabel = (c: CourseDetail) => (c.publishState === 'rejected' ? '再次申请公开' : '申请公开');
const updateLabel = (c: CourseDetail) => (c.completion === 'incomplete' && !c.isUploader ? '申请补全' : '提交更新');
const editHref = (c: CourseDetail) => `/courses/${c.id}/edit`;

// CourseActions offers, in the desktop colophon, what the viewer may do
// with a course in its current state (see courseActions).
export function CourseActions({ c, onChange }: { c: CourseDetail; onChange: () => void }) {
  const { has, review, run, setDialog, notes, dialogs } = useCourseActions(c, onChange);
  return (
    <div className="stack">
      {notes.publishRejected}
      {has('publish-request') ? <Button onClick={run.publishRequest}>{publishLabel(c)}</Button> : null}
      {has('edit') ? (
        <Button variant="ghost" asChild><Link href={editHref(c)}><PenLine />编辑课程</Link></Button>
      ) : null}
      {has('withdraw-publish') ? <Button variant="ghost" onClick={run.withdrawPublish}>撤回公开申请</Button> : null}
      {has('review') && review ? (
        <div className="review-box">
          <div className="t">{review.requester.username} 申请公开这门课程</div>
          <div className="row">
            <Button variant="default" size="sm" onClick={run.approve}>批准</Button>
            <Button size="sm" onClick={() => setDialog('reject')}>驳回</Button>
          </div>
        </div>
      ) : null}
      {has('update-request') ? <Button variant="ghost" onClick={() => setDialog('request')}>{updateLabel(c)}</Button> : null}
      {has('withdraw-update') ? (
        <div className="request-note">
          <b>更新申请审核中</b>
          <Button variant="text" size="bare" onClick={run.withdrawUpdate}>撤回</Button>
        </div>
      ) : notes.updateRejected}
      {has('direct-publish') ? <Button onClick={run.directPublish}>直接公开</Button> : null}
      {has('delete') ? (
        <div className="admin-tools">
          {has('edit-info') ? <Button variant="text" size="bare" onClick={() => setDialog('edit')}>编辑信息</Button> : null}
          {has('unpublish') ? <Button variant="text" size="bare" onClick={() => setDialog('unpublish')}>下架</Button> : null}
          <Button variant="text" size="bare" onClick={() => setDialog('delete')}>删除课程</Button>
        </div>
      ) : null}
      {dialogs}
    </div>
  );
}

// CourseTools are the narrow course page's entries beside 课程介绍 / 目录:
// a gear into the course editor, for those who may open it, and a ⋯ menu
// rising from the bottom with every other action. Either is left out when
// the viewer has nothing behind it.
export function CourseTools({ c, onChange }: { c: CourseDetail; onChange: () => void }) {
  const { actions, has, review, run, setDialog, notes, dialogs } = useCourseActions(c, onChange);
  const [open, setOpen] = useState(false);
  const menu = menuActions(actions);
  // a row closes the menu first, so a dialog it opens is not stacked on it
  const row = (Icon: LucideIcon, label: ReactNode, onClick: () => void, danger = false) => (
    <button type="button" className={cn('sheet-row', danger && 'danger')} onClick={() => { setOpen(false); onClick(); }}>
      <Icon aria-hidden="true" />{label}
    </button>
  );
  const rows: Record<Exclude<CourseAction, 'edit'>, ReactNode> = {
    'publish-request': row(Send, publishLabel(c), run.publishRequest),
    'withdraw-publish': row(Undo2, '撤回公开申请', run.withdrawPublish),
    review: <>{row(Check, '批准公开申请', run.approve)}{row(X, '驳回公开申请', () => setDialog('reject'))}</>,
    'update-request': row(FilePenLine, updateLabel(c), () => setDialog('request')),
    'withdraw-update': row(Undo2, '撤回更新申请', run.withdrawUpdate),
    'direct-publish': row(Globe, '直接公开', run.directPublish),
    'edit-info': row(Pencil, '编辑信息', () => setDialog('edit')),
    unpublish: row(ArrowDown, '下架', () => setDialog('unpublish')),
    delete: row(Trash2, '删除课程', () => setDialog('delete'), true),
  };
  return (
    <div className="course-tools">
      {has('edit') ? (
        <Button variant="round" size="round" asChild><Link href={editHref(c)} aria-label="编辑课程" title="编辑课程"><Settings /></Link></Button>
      ) : null}
      {menu.length ? (
        <Button variant="round" size="round" aria-label="课程操作" title="课程操作" onClick={() => setOpen(true)}><Ellipsis /></Button>
      ) : null}
      <Sheet open={open} onOpenChange={setOpen} title="课程操作">
        {notes.publishRejected}
        {has('review') && review ? <div className="sheet-note">{review.requester.username} 申请公开这门课程</div> : null}
        {has('withdraw-update') ? <div className="sheet-note">更新申请审核中</div> : notes.updateRejected}
        {menu.map((a) => <div key={a} className="contents">{rows[a as Exclude<CourseAction, 'edit'>]}</div>)}
      </Sheet>
      {dialogs}
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

import { useEffect, useState } from 'react';
import { Upload } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Confirm } from '@/components/confirm';
import { Empty, ErrorBox, PageHead } from '@/components/common';
import { Facet } from '@/components/facet';
import { Shelf } from '@/components/shelf';
import { UploadDialog } from '@/components/upload-dialog';
import { api, ApiError } from '@/lib/api';
import { COMPLETION_LABEL, TYPE_LABEL, type CourseSummary } from '@/lib/courses';

export function MineView() {
  const [courses, setCourses] = useState<CourseSummary[] | null>(null);
  const [err, setErr] = useState('');
  const [state, setState] = useState('');
  const [type, setType] = useState('');
  const [completion, setCompletion] = useState('');
  const [uploading, setUploading] = useState(false);
  const [deleting, setDeleting] = useState<CourseSummary | null>(null);
  const [removing, setRemoving] = useState<CourseSummary | null>(null);

  const load = () => {
    const p = new URLSearchParams();
    if (state) p.set('state', state);
    if (type) p.set('type', type);
    if (completion) p.set('completion', completion);
    return api<{ courses: CourseSummary[] }>('GET', '/api/my/courses?' + p).then((r) => { setCourses(r.courses); setErr(''); }).catch((e) => setErr(e.message));
  };
  useEffect(() => { void load(); }, [state, type, completion]); // eslint-disable-line react-hooks/exhaustive-deps

  const act = async (method: string, path: string, done: string) => {
    try { await api(method, path); toast.success(done); setDeleting(null); setRemoving(null); void load(); } catch (ex) { toast.error(ex instanceof ApiError ? ex.message : '操作失败'); }
  };
  const filtered = !!(state || type || completion);

  return (
    <div className="page enter">
      <PageHead
        kicker="My courses"
        title="我的课程"
        sub="你正在学习的课程, 以及你上传的私有课程。"
        actions={<Button variant="default" onClick={() => setUploading(true)}><Upload />上传课程</Button>}
      />
      <div className="facets">
        <Facet label="学习" value={state} onPick={setState} items={[['', '全部'], ['studying', '学习中'], ['done', '已学完']]} />
        <Facet label="类型" value={type} onPick={setType} items={[['', '全部'], ['column', TYPE_LABEL.column], ['video', TYPE_LABEL.video]]} />
        <Facet label="状态" value={completion} onPick={setCompletion} items={[['', '全部'], ['finished', COMPLETION_LABEL.finished], ['ongoing', COMPLETION_LABEL.ongoing], ['incomplete', COMPLETION_LABEL.incomplete]]} />
      </div>
      {err ? <ErrorBox message={err} /> : null}
      {courses ? <div className="result-bar"><span className="count"><b>{courses.length}</b>门课程</span><span>按最近学习排序</span></div> : null}
      {courses && !courses.length ? (
        <Empty glyph={filtered ? '无' : '空'} title={filtered ? '没有符合条件的课程' : '还没有课程'}>
          <div>{filtered ? '换个筛选条件试试' : '上传一个课程包, 或去课程库挑一门课开始学习'}</div>
        </Empty>
      ) : null}
      {courses && courses.length ? <Shelf courses={courses} hrefOf={(c) => `/courses/${c.id}`} onDelete={setDeleting} onRemove={setRemoving} showProgress /> : null}
      <UploadDialog open={uploading} onClose={() => setUploading(false)} onUploaded={() => { setUploading(false); void load(); }} />
      <Confirm open={!!deleting} title="删除课程" confirm="删除" onClose={() => setDeleting(null)}
        onConfirm={() => act('DELETE', `/api/courses/${deleting!.id}`, `已删除「${deleting!.title}」`)}>
        确定删除「{deleting?.title}」吗? 课程内容、文件和学习记录都会被删除, 无法恢复。
      </Confirm>
      <Confirm open={!!removing} title="移出我的课程" confirm="移出" onClose={() => setRemoving(null)}
        onConfirm={() => act('DELETE', `/api/courses/${removing!.id}/enroll`, `已移出「${removing!.title}」`)}>
        「{removing?.title}」会从我的课程中移出, 你的学习进度会保留。以后在课程库重新开始学习, 进度会恢复。
      </Confirm>
    </div>
  );
}

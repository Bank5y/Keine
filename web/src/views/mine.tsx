import { useEffect, useState } from 'react';
import { Upload } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { Empty, ErrorBox, PageHead } from '@/components/common';
import { Shelf } from '@/components/shelf';
import { UploadDialog } from '@/components/upload-dialog';
import { api, ApiError } from '@/lib/api';
import type { CourseSummary } from '@/lib/courses';

export function MineView() {
  const [courses, setCourses] = useState<CourseSummary[] | null>(null);
  const [err, setErr] = useState('');
  const [uploading, setUploading] = useState(false);
  const [deleting, setDeleting] = useState<CourseSummary | null>(null);
  const [busy, setBusy] = useState(false);

  const load = () => api<{ courses: CourseSummary[] }>('GET', '/api/my/courses').then((r) => setCourses(r.courses)).catch((e) => setErr(e.message));
  useEffect(() => { void load(); }, []);

  async function remove(c: CourseSummary) {
    setBusy(true);
    try {
      await api('DELETE', `/api/courses/${c.id}`);
      toast.success(`已删除「${c.title}」`);
      setDeleting(null);
      void load();
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '删除失败');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="page enter">
      <PageHead
        kicker="My courses"
        title="我的课程"
        sub="你正在学习的课程, 以及你上传的私有课程。"
        actions={<Button variant="default" onClick={() => setUploading(true)}><Upload />上传课程</Button>}
      />
      {err ? <ErrorBox message={err} /> : null}
      {courses ? (
        <div className="result-bar"><span className="count"><b>{courses.length}</b>门课程</span></div>
      ) : null}
      {courses && !courses.length ? <Empty glyph="空" title="还没有课程"><div>上传一个课程包, 或去课程库挑一门课开始学习</div></Empty> : null}
      {courses && courses.length ? <Shelf courses={courses} hrefOf={(c) => `/courses/${c.id}`} onDelete={setDeleting} /> : null}
      <UploadDialog open={uploading} onClose={() => setUploading(false)} onUploaded={() => { setUploading(false); void load(); }} />
      <Dialog open={!!deleting} onOpenChange={(v) => { if (!v && !busy) setDeleting(null); }} title="删除课程">
        <p>确定删除「{deleting?.title}」吗? 课程内容、文件和学习记录都会被删除, 无法恢复。</p>
        <div className="head-actions">
          <Button onClick={() => setDeleting(null)} disabled={busy}>取消</Button>
          <Button variant="default" onClick={() => deleting && remove(deleting)} disabled={busy}>删除</Button>
        </div>
      </Dialog>
    </div>
  );
}

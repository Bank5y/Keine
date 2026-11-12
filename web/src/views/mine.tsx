import { useEffect, useState } from 'react';
import { Upload } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Empty, ErrorBox, PageHead } from '@/components/common';
import { UploadDialog } from '@/components/upload-dialog';
import { api } from '@/lib/api';

interface Summary { id: number; title: string }

export function MineView() {
  const [courses, setCourses] = useState<Summary[] | null>(null);
  const [err, setErr] = useState('');
  const [uploading, setUploading] = useState(false);

  const load = () => api<{ courses: Summary[] }>('GET', '/api/my/courses').then((r) => setCourses(r.courses)).catch((e) => setErr(e.message));
  useEffect(() => { void load(); }, []);

  return (
    <div className="page enter">
      <PageHead
        kicker="My courses"
        title="我的课程"
        sub="你正在学习的课程, 以及你上传的私有课程。"
        actions={<Button variant="default" onClick={() => setUploading(true)}><Upload />上传课程</Button>}
      />
      {err ? <ErrorBox message={err} /> : null}
      {courses && !courses.length ? <Empty glyph="空" title="还没有课程"><div>上传一个课程包, 或去课程库挑一门课开始学习</div></Empty> : null}
      <ul>{(courses || []).map((c) => <li key={c.id}>{c.title}</li>)}</ul>
      <UploadDialog open={uploading} onClose={() => setUploading(false)} onUploaded={() => { setUploading(false); void load(); }} />
    </div>
  );
}

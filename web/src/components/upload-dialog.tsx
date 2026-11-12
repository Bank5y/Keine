import { useEffect, useRef, useState, type DragEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { ErrorBox } from '@/components/common';
import { ApiError, upload, type Problem } from '@/lib/api';
import { cn } from '@/lib/utils';

const PROMPT_URL = '/api/manifest/prompt';

// UploadDialog sends a course package and shows its progress, then either
// reports success or lists every problem the server found. Without a
// courseId it imports a new course; with one it updates that course.
export function UploadDialog({ open, onClose, onUploaded, courseId }: { open: boolean; onClose: () => void; onUploaded: (id: number) => void; courseId?: number }) {
  const [file, setFile] = useState<File | null>(null);
  const [progress, setProgress] = useState<number | null>(null);
  const [err, setErr] = useState<{ message: string; problems: Problem[] } | null>(null);
  const [over, setOver] = useState(false);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => { if (open) { setFile(null); setProgress(null); setErr(null); } }, [open]);

  const pick = (f: File | undefined | null) => { if (f) { setFile(f); setErr(null); } };
  const drop = (e: DragEvent) => { e.preventDefault(); setOver(false); pick(e.dataTransfer.files[0]); };

  async function send() {
    if (!file) return;
    setErr(null);
    setProgress(0);
    try {
      if (courseId) {
        await upload(`/api/courses/${courseId}/package`, 'package', file, setProgress, 'PUT');
        toast.success('课程已更新');
        onUploaded(courseId);
      } else {
        const r = await upload<{ id: number }>('/api/courses', 'package', file, setProgress);
        toast.success('课程已导入');
        onUploaded(r.id);
      }
    } catch (ex) {
      setErr(ex instanceof ApiError ? { message: ex.message, problems: ex.problems } : { message: '上传失败', problems: [] });
    } finally {
      setProgress(null);
    }
  }

  const busy = progress !== null;
  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v && !busy) onClose(); }} title={courseId ? '更新课程' : '上传课程'} wide>
      {courseId ? (
        <p className="mt-2">
          上传新的课程包替换这门课的内容。章和讲按 <code>course.json</code> 里的 id 对应: id 不变的讲保留学习记录, 新增的讲为未学习, 清单里去掉的讲会被移除。
        </p>
      ) : (
        <p className="mt-2">
          上传一个 zip 课程包, 根目录必须包含 <code>course.json</code> 课程清单。上传后它是只有你能看到的私有课程。
          还没有清单? <a className="link" href={PROMPT_URL} download>下载提示词模板</a>, 交给 AI 帮你生成。
        </p>
      )}
      {err ? (
        <ErrorBox message={err.message} problems={err.problems}>
          {err.problems.length ? <div className="mt-2">不确定怎么写? <a className="link" href={PROMPT_URL} download>下载提示词模板</a>, 交给 AI 重新生成 course.json。</div> : null}
        </ErrorBox>
      ) : null}
      <div
        className={cn('drop', over && 'over')}
        role="button"
        tabIndex={0}
        onClick={() => input.current?.click()}
        onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') input.current?.click(); }}
        onDragOver={(e) => { e.preventDefault(); setOver(true); }}
        onDragLeave={() => setOver(false)}
        onDrop={drop}
      >
        <div className="t">{file ? file.name : '选择或拖入课程包'}</div>
        <div>{file ? `${(file.size / 1024 / 1024).toFixed(1)} MB` : '.zip 文件'}</div>
        <input ref={input} type="file" accept=".zip,application/zip" hidden onChange={(e) => pick(e.target.files?.[0])} />
      </div>
      {busy ? (
        <>
          <div className="bar"><i style={{ width: `${Math.round(progress * 100)}%` }} /></div>
          <div className="text-xs text-ink-3">{progress < 1 ? `上传中 ${Math.round(progress * 100)}%` : '上传完成, 正在校验和导入…'}</div>
        </>
      ) : null}
      <div className="head-actions mt-5">
        <Button onClick={onClose} disabled={busy}>取消</Button>
        <Button variant="default" onClick={send} disabled={!file || busy}>{courseId ? '更新' : '上传'}</Button>
      </div>
    </Dialog>
  );
}

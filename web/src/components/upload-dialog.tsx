import { useEffect, useRef, useState, type DragEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { ErrorBox } from '@/components/common';
import { ApiError, upload, type Problem } from '@/lib/api';
import { cn } from '@/lib/utils';
import { videosInZip } from '@/lib/videos';
import { useVideoNotice } from '@/components/video-notice';

export const PROMPT_URL = '/api/manifest/prompt';

// ManifestHint follows a package's problems: the prompt template lets an
// AI write course.json again.
export const ManifestHint = () => <div className="mt-2">不确定怎么写? <a className="link" href={PROMPT_URL} download>下载提示词模板</a>, 交给 AI 重新生成 course.json。</div>;

// UploadDialog sends a course package for an existing course and shows its
// progress, then either reports success or lists every problem the server
// found. It updates the course, or (request) asks for the update in an
// update request. New courses come in through ImportDialog.
export function UploadDialog({ open, onClose, onUploaded, courseId, request }: { open: boolean; onClose: () => void; onUploaded: (id: number) => void; courseId: number; request?: boolean }) {
  const [file, setFile] = useState<File | null>(null);
  const [progress, setProgress] = useState<number | null>(null);
  const [err, setErr] = useState<{ message: string; problems: Problem[] } | null>(null);
  const [over, setOver] = useState(false);
  const [checking, setChecking] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const { confirmVideos, notice } = useVideoNotice();

  useEffect(() => { if (open) { setFile(null); setProgress(null); setErr(null); } }, [open]);

  const pick = (f: File | undefined | null) => { if (f) { setFile(f); setErr(null); } };
  const drop = (e: DragEvent) => { e.preventDefault(); setOver(false); pick(e.dataTransfer.files[0]); };

  async function send() {
    if (!file) return;
    setErr(null);
    setChecking(true);
    const videos = await videosInZip(file);
    setChecking(false);
    if (!(await confirmVideos(videos))) return;
    setProgress(0);
    try {
      if (request) {
        await upload(`/api/courses/${courseId}/update-requests`, 'package', file, setProgress);
        toast.success('已提交更新申请, 等待管理员审核');
        onUploaded(courseId);
      } else {
        await upload(`/api/courses/${courseId}/package`, 'package', file, setProgress, 'PUT');
        toast.success('课程已更新');
        onUploaded(courseId);
      }
    } catch (ex) {
      setErr(ex instanceof ApiError ? { message: ex.message, problems: ex.problems } : { message: '上传失败', problems: [] });
    } finally {
      setProgress(null);
    }
  }

  const busy = progress !== null || checking;
  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v && !busy) onClose(); }} title={request ? '提交更新申请' : '更新课程'} wide>
      {request ? (
        <p className="mt-2">
          上传完整的新课程包。管理员批准前, 课程库里仍是原来的内容。章和讲按 <code>course.json</code> 里的 id 对应, 请保持已有讲的 id 不变, 这样大家的学习记录才能保留。
          <a className="link ml-1" href={PROMPT_URL} download>下载提示词模板</a>
        </p>
      ) : (
        <p className="mt-2">
          上传新的课程包替换这门课的内容。章和讲按 <code>course.json</code> 里的 id 对应: id 不变的讲保留学习记录, 新增的讲为未学习, 清单里去掉的讲会被移除。
        </p>
      )}
      {err ? (
        <ErrorBox message={err.message} problems={err.problems}>
          {err.problems.length ? <ManifestHint /> : null}
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
      {progress !== null ? (
        <>
          <div className="bar"><i style={{ width: `${Math.round(progress * 100)}%` }} /></div>
          <div className="text-xs text-ink-3">{progress < 1 ? `上传中 ${Math.round(progress * 100)}%` : '上传完成, 正在校验和导入…'}</div>
        </>
      ) : null}
      <div className="head-actions mt-5">
        <Button onClick={onClose} disabled={busy}>取消</Button>
        <Button variant="default" onClick={send} disabled={!file || busy}>{request ? '提交' : '更新'}</Button>
      </div>
      {notice}
    </Dialog>
  );
}

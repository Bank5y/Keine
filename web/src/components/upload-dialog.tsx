import { useEffect, useRef, useState, type DragEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { ErrorBox } from '@/components/common';
import { ApiError, upload, type Problem } from '@/lib/api';
import { cn } from '@/lib/utils';

// UploadDialog sends a course package and shows its progress, then either
// opens the new course or lists every problem the server found.
export function UploadDialog({ open, onClose, onUploaded }: { open: boolean; onClose: () => void; onUploaded: (id: number) => void }) {
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
      const r = await upload<{ id: number }>('/api/courses', 'package', file, setProgress);
      toast.success('课程已导入');
      onUploaded(r.id);
    } catch (ex) {
      setErr(ex instanceof ApiError ? { message: ex.message, problems: ex.problems } : { message: '上传失败', problems: [] });
    } finally {
      setProgress(null);
    }
  }

  const busy = progress !== null;
  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v && !busy) onClose(); }} title="上传课程" wide>
      <p className="mt-2">上传一个 zip 课程包, 根目录必须包含 <code>course.json</code> 课程清单。上传后它是只有你能看到的私有课程。</p>
      {err ? (
        <ErrorBox message={err.message} problems={err.problems} />
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
        <Button variant="default" onClick={send} disabled={!file || busy}>上传</Button>
      </div>
    </Dialog>
  );
}

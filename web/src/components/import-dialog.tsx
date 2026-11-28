import { useEffect, useRef, useState, type DragEvent } from 'react';
import { X } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { ErrorBox } from '@/components/common';
import { ApiError, upload, type Problem } from '@/lib/api';
import { runInTurn } from '@/lib/batch';
import { cn } from '@/lib/utils';
import { videosInZip } from '@/lib/videos';
import { useVideoNotice } from '@/components/video-notice';
import { ManifestHint, PROMPT_URL } from '@/components/upload-dialog';

// One course package picked for import, and how far it got.
interface Entry {
  file: File;
  state: 'waiting' | 'sending' | 'done' | 'failed';
  progress: number;
  error?: { message: string; problems: Problem[] };
}

const isZip = (f: File) => /\.zip$/i.test(f.name) || f.type === 'application/zip';

// ImportDialog uploads one or more course packages, each becoming a new
// private course. They go one after another; a package the server refuses
// stays in the list with every problem found, and the others go on. Once
// all are in, the dialog closes; until then, 上传 sends the ones left.
// onImported hears of each batch that brought in at least one course.
export function ImportDialog({ open, onClose, onImported }: { open: boolean; onClose: () => void; onImported: () => void }) {
  const [entries, setEntries] = useState<Entry[]>([]);
  const [sending, setSending] = useState(false);
  const [over, setOver] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const { confirmVideos, notice } = useVideoNotice();

  useEffect(() => { if (open) setEntries([]); }, [open]);

  const pick = (files: FileList | null | undefined) => {
    // a package picked again (or twice at once) replaces the earlier pick of the same name
    const byName = new Map(Array.from(files ?? []).filter(isZip).map((f) => [f.name, f]));
    const zips = [...byName.values()];
    if (!zips.length) return;
    setEntries((es) => [...es.filter((e) => e.state !== 'done' && !zips.some((z) => z.name === e.file.name)), ...zips.map((file): Entry => ({ file, state: 'waiting', progress: 0 }))]);
  };
  const drop = (e: DragEvent) => { e.preventDefault(); setOver(false); pick(e.dataTransfer.files); };
  const patch = (file: File, p: Partial<Entry>) => setEntries((es) => es.map((e) => (e.file === file ? { ...e, ...p } : e)));
  const remove = (file: File) => setEntries((es) => es.filter((e) => e.file !== file));

  const todo = entries.filter((e) => e.state !== 'done');

  async function send() {
    if (!todo.length) return;
    setSending(true);
    try {
      const sums = await Promise.all(todo.map((e) => videosInZip(e.file)));
      const videos = sums.reduce((a, v) => ({ count: a.count + v.count, bytes: a.bytes + v.bytes }), { count: 0, bytes: 0 });
      if (!(await confirmVideos(videos))) return;
      const results = await runInTurn(todo.map((e) => e.file), async (file) => {
        patch(file, { state: 'sending', progress: 0, error: undefined });
        await upload<{ id: number }>('/api/courses', 'package', file, (progress) => patch(file, { progress }));
      }, (o) => patch(o.item, o.ok ? { state: 'done' } : {
        state: 'failed',
        error: o.error instanceof ApiError ? { message: o.error.message, problems: o.error.problems } : { message: '上传失败', problems: [] },
      }));
      const done = results.filter((r) => r.ok).length;
      if (done) onImported();
      if (done === results.length) {
        toast.success(done > 1 ? `已导入 ${done} 门课程` : '课程已导入');
        onClose();
      } else {
        toast.error(`${done ? `已导入 ${done} 门课程, ` : ''}${results.length - done} 个课程包没有导入, 请修正后重新上传`);
      }
    } finally {
      setSending(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v && !sending) onClose(); }} title="上传课程" wide>
      <p className="mt-2">
        上传 zip 课程包, 可以一次选多个, 每个包是一门课程, 根目录必须包含 <code>course.json</code> 课程清单。上传后它们是只有你能看到的私有课程。
        还没有清单? <a className="link" href={PROMPT_URL} download>下载提示词模板</a>, 交给 AI 帮你生成。
      </p>
      <div
        className={cn('drop', over && 'over')}
        role="button"
        tabIndex={0}
        aria-disabled={sending}
        onClick={() => { if (!sending) input.current?.click(); }}
        onKeyDown={(e) => { if (!sending && (e.key === 'Enter' || e.key === ' ')) input.current?.click(); }}
        onDragOver={(e) => { e.preventDefault(); setOver(true); }}
        onDragLeave={() => setOver(false)}
        onDrop={(e) => { if (sending) { e.preventDefault(); return; } drop(e); }}
      >
        <div className="t">{entries.length ? '继续添加课程包' : '选择或拖入课程包'}</div>
        <div>.zip 文件, 可多选</div>
        <input ref={input} type="file" accept=".zip,application/zip" multiple hidden onChange={(e) => { pick(e.target.files); e.target.value = ''; }} />
      </div>
      {entries.length ? (
        <ul className="import-list">
          {entries.map((e) => (
            <li key={e.file.name} className={e.state}>
              <div className="row">
                <span className="name">{e.file.name}</span>
                <span className="size">{(e.file.size / 1024 / 1024).toFixed(1)} MB</span>
                <span className="state">{stateLabel(e)}</span>
                {!sending && e.state !== 'done' ? (
                  <Button variant="icon" size="icon" title="移除" aria-label={`移除 ${e.file.name}`} onClick={() => remove(e.file)}><X /></Button>
                ) : null}
              </div>
              {e.state === 'sending' ? <div className="bar"><i style={{ width: `${Math.round(e.progress * 100)}%` }} /></div> : null}
              {e.error ? (
                <ErrorBox message={e.error.message} problems={e.error.problems}>
                  {e.error.problems.length ? <ManifestHint /> : null}
                </ErrorBox>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
      <div className="head-actions mt-5">
        <Button onClick={onClose} disabled={sending}>{entries.some((e) => e.state === 'done') ? '关闭' : '取消'}</Button>
        <Button variant="default" onClick={send} disabled={!todo.length || sending}>{todo.length > 1 ? `上传 ${todo.length} 个` : '上传'}</Button>
      </div>
      {notice}
    </Dialog>
  );
}

function stateLabel(e: Entry) {
  switch (e.state) {
    case 'waiting': return '等待上传';
    case 'sending': return e.progress < 1 ? `上传中 ${Math.round(e.progress * 100)}%` : '正在校验和导入…';
    case 'done': return '已导入';
    case 'failed': return '未导入';
  }
}

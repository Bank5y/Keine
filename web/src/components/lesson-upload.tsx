import { useEffect, useRef, useState, type DragEvent, type FormEvent } from 'react';
import { RotateCw, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { ErrorBox } from '@/components/common';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { LessonVideoView } from '@/components/video';
import { useVideoNotice } from '@/components/video-notice';
import { ApiError, type Problem } from '@/lib/api';
import type { CourseType } from '@/lib/courses';
import { addOp, previewLesson, putUploadFile, startUpload, type Draft, type LessonContent, type LessonPreview, type Op } from '@/lib/drafts';
import { cn } from '@/lib/utils';
import { formatBytes, NO_VIDEOS, videosInFiles, videosInZip, type VideoSummary } from '@/lib/videos';

// What a lesson upload does: add a lesson to a chapter, replace a lesson's
// content, or give an upcoming lesson its content.
export type LessonTarget =
  | { kind: 'add'; chapter: string; chapterTitle: string }
  | { kind: 'replace'; lesson: string; title: string }
  | { kind: 'fill'; lesson: string; title: string };

type FileState = 'waiting' | 'sending' | 'done' | 'failed';
interface Item { file: File; state: FileState; progress: number; error?: string }

const DIALOG_TITLE = { add: '添加讲', replace: '替换讲', fill: '上传内容' } as const;

// LessonUploadDialog uploads one lesson's content into the draft: a .md
// text with its images (picked together, or as a single lesson's zip), a
// video file or link, or a text and a video. Files go one by one with
// their progress; a failed or cancelled file is sent again on its own, and
// files already sent are never sent twice. Missing images are listed by
// the server and can be added before trying again.
export function LessonUploadDialog({ courseId, courseType, target, onClose, onDone }: {
  courseId: number; courseType: CourseType; target: LessonTarget | null; onClose: () => void; onDone: (d: Draft) => void;
}) {
  const [title, setTitle] = useState('');
  const [items, setItems] = useState<Item[]>([]);
  const [videoUrl, setVideoUrl] = useState('');
  const [err, setErr] = useState<{ message: string; problems: Problem[] } | null>(null);
  const [busy, setBusy] = useState(false);
  const [over, setOver] = useState(false);
  const upload = useRef<string | null>(null);
  const abort = useRef<AbortController | null>(null);
  const confirmed = useRef(new WeakSet<File>());
  const input = useRef<HTMLInputElement>(null);
  const { confirmVideos, notice } = useVideoNotice();

  useEffect(() => {
    if (!target) return;
    setTitle(target.kind === 'add' ? '' : target.title);
    setItems([]); setVideoUrl(''); setErr(null); setBusy(false);
    upload.current = null;
  }, [target]);

  const video = courseType === 'video';
  const add = (files: FileList | null | undefined) => {
    if (!files?.length) return;
    const picked = Array.from(files);
    // a file picked again under the same name replaces the earlier one
    setItems((cur) => [...cur.filter((i) => !picked.some((f) => f.name === i.file.name)), ...picked.map((file) => ({ file, state: 'waiting' as const, progress: 0 }))]);
    setErr(null);
  };
  const drop = (e: DragEvent) => { e.preventDefault(); setOver(false); add(e.dataTransfer.files); };
  const patch = (file: File, p: Partial<Item>) => setItems((cur) => cur.map((i) => (i.file === file ? { ...i, ...p } : i)));
  const pending = items.filter((i) => i.state !== 'done');
  const needsTitle = target?.kind === 'add';
  const ready = (!needsTitle || title.trim()) && (items.length > 0 || videoUrl.trim());

  async function videosOf(list: Item[]): Promise<VideoSummary> {
    const sum = { ...NO_VIDEOS, ...videosInFiles(list.filter((i) => !/\.zip$/i.test(i.file.name)).map((i) => i.file)) };
    for (const i of list) {
      if (/\.zip$/i.test(i.file.name)) { const z = await videosInZip(i.file); sum.count += z.count; sum.bytes += z.bytes; }
    }
    return sum;
  }

  // send uploads the files not sent yet; false when one failed or was
  // cancelled (it stays in the list to send again).
  async function send(list: Item[]): Promise<boolean> {
    if (!list.length) return true;
    upload.current ??= await startUpload(courseId);
    for (const i of list) {
      const ctl = new AbortController();
      abort.current = ctl;
      patch(i.file, { state: 'sending', progress: 0, error: undefined });
      try {
        await putUploadFile(courseId, upload.current, i.file, (p) => patch(i.file, { progress: p }), ctl.signal);
        patch(i.file, { state: 'done', progress: 1 });
      } catch (ex) {
        patch(i.file, { state: 'failed', error: ex instanceof ApiError ? ex.message : '上传失败' });
        return false;
      } finally {
        abort.current = null;
      }
    }
    return true;
  }

  async function submit(e?: FormEvent) {
    e?.preventDefault();
    if (!target || !ready || busy) return;
    setErr(null);
    setBusy(true);
    try {
      // each video is announced once, not again when it is retried
      const fresh = pending.filter((i) => !confirmed.current.has(i.file));
      if (!(await confirmVideos(await videosOf(fresh)))) return;
      fresh.forEach((i) => confirmed.current.add(i.file));
      if (!(await send(pending))) return;
      const content: LessonContent = {};
      if (upload.current) content.upload = upload.current;
      if (videoUrl.trim()) content.videoUrl = videoUrl.trim();
      const t = title.trim();
      const op: Op = target.kind === 'add'
        ? { type: 'addLesson', chapter: target.chapter, title: t, content }
        : target.kind === 'replace'
          ? { type: 'replaceLesson', lesson: target.lesson, content, ...(t && t !== target.title ? { title: t } : {}) }
          : { type: 'uploadLessonContent', lesson: target.lesson, content };
      onDone(await addOp(courseId, op));
    } catch (ex) {
      setErr(ex instanceof ApiError ? { message: ex.message, problems: ex.problems } : { message: '上传失败', problems: [] });
    } finally {
      setBusy(false);
    }
  }

  const sending = items.some((i) => i.state === 'sending');
  return (
    <Dialog open={!!target} onOpenChange={(v) => { if (!v && !busy) onClose(); }} title={target ? DIALOG_TITLE[target.kind] : ''} wide>
      <form onSubmit={submit} className="lesson-upload">
        <p className="mt-2">
          {target?.kind === 'add' ? <>加到「{target.chapterTitle}」的末尾。</> : target ? <>「{target.title}」的 id 不变, 学习进度保留。</> : null}
          {video ? ' 上传一个视频文件或填视频链接, 可以附一个 .md 文字稿。' : ' 选择一个 .md 正文和它引用的图片 (可多选), 或一个只装这一讲的 zip; 也可以附一个视频。'}
        </p>
        {err ? <ErrorBox message={err.message} problems={err.problems}>{err.problems.length ? <div className="mt-2">补选缺少的图片后再提交, 已上传的文件不会重传。</div> : null}</ErrorBox> : null}
        {needsTitle || target?.kind === 'replace' ? (
          <div className="field mt-4"><label htmlFor="lu-title">标题</label><Input id="lu-title" value={title} maxLength={200} onChange={(e) => setTitle(e.target.value)} disabled={busy} /></div>
        ) : null}
        <div
          className={cn('drop mt-4', over && 'over')}
          role="button"
          tabIndex={0}
          aria-label="选择文件"
          onClick={() => input.current?.click()}
          onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') input.current?.click(); }}
          onDragOver={(e) => { e.preventDefault(); setOver(true); }}
          onDragLeave={() => setOver(false)}
          onDrop={drop}
        >
          <div className="t">选择或拖入文件</div>
          <div>{video ? '视频、.md 文字稿' : '.md 正文、图片、视频, 或单讲 .zip'}</div>
          <input ref={input} type="file" multiple hidden onChange={(e) => { add(e.target.files); e.target.value = ''; }} />
        </div>
        {items.length ? (
          <ul className="lu-files" aria-label="要上传的文件">
            {items.map((i) => (
              <li key={i.file.name} className={cn('lu-file', i.state)}>
                <span className="n" title={i.file.name}>{i.file.name}</span>
                <span className="s">{i.state === 'done' ? '已上传' : i.state === 'sending' ? `${Math.round(i.progress * 100)}%` : i.state === 'failed' ? i.error : formatBytes(i.file.size)}</span>
                {i.state === 'sending' ? <div className="bar"><i style={{ width: `${Math.round(i.progress * 100)}%` }} /></div> : null}
                {i.state === 'failed' && !busy ? (
                  <Button variant="round" size="round" aria-label={`重传 ${i.file.name}`} title="重传" onClick={() => void submit()}><RotateCw /></Button>
                ) : null}
                {i.state !== 'done' && i.state !== 'sending' && !busy ? (
                  <Button variant="round" size="round" aria-label={`移除 ${i.file.name}`} title="移除" onClick={() => setItems((cur) => cur.filter((x) => x !== i))}><X /></Button>
                ) : null}
              </li>
            ))}
          </ul>
        ) : null}
        <div className="field mt-4">
          <label htmlFor="lu-video">视频链接{video ? ' (没有视频文件时填写)' : ' (可选)'}</label>
          <Input id="lu-video" type="url" placeholder="https://" value={videoUrl} onChange={(e) => setVideoUrl(e.target.value)} disabled={busy} />
        </div>
        <div className="head-actions mt-5">
          {sending ? <Button onClick={() => abort.current?.abort()}>取消上传</Button> : <Button onClick={onClose} disabled={busy}>取消</Button>}
          <Button type="submit" variant="default" disabled={!ready || busy}>{pending.length ? '上传并保存到编辑稿' : '保存到编辑稿'}</Button>
        </div>
      </form>
      {notice}
    </Dialog>
  );
}

// LessonPreviewDialog shows a lesson of the draft as the reader will: its
// uploaded text with images, and its video.
export function LessonPreviewDialog({ courseId, lesson, onClose }: { courseId: number; lesson: string | null; onClose: () => void }) {
  const [p, setP] = useState<LessonPreview | null>(null);
  const [err, setErr] = useState('');
  useEffect(() => {
    setP(null); setErr('');
    if (lesson) previewLesson(courseId, lesson).then(setP, (e) => setErr(e instanceof ApiError ? e.message : '加载失败'));
  }, [courseId, lesson]);
  return (
    <Dialog open={!!lesson} onOpenChange={(v) => { if (!v) onClose(); }} title={p ? `预览: ${p.title}` : '预览'} wide="reader">
      <div className="lesson-preview mt-4">
        {err ? <ErrorBox message={err} /> : null}
        {p?.video ? <LessonVideoView video={p.video} position={0} onPosition={() => {}} onNearlyDone={() => {}} /> : null}
        {p?.markdown ? <LazyMarkdown text={p.markdown} base={p.base} className="reader-prose" /> : null}
        {p && !p.markdown && !p.video ? <p className="text-ink-3">这一讲没有文字内容。</p> : null}
      </div>
      <div className="head-actions mt-5"><Button onClick={onClose}>关闭</Button></div>
    </Dialog>
  );
}

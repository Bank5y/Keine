import { useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { ArrowDown, ArrowLeft, ArrowUp, ChevronRight, Eye, FilePlus2, FileUp, GripVertical, Pencil, Plus, Trash2, Undo2 } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { CommitDialog } from '@/components/commit-dialog';
import { Confirm } from '@/components/confirm';
import { ConflictList } from '@/components/conflict-list';
import { Empty, ErrorBox, PageHead } from '@/components/common';
import { Fold } from '@/components/fold';
import { Skeleton } from '@/components/skeleton';
import { TabPane, Tabs, useAddressTab, useTabFade } from '@/components/tabs';
import { UploadDialog } from '@/components/upload-dialog';
import { LessonPreviewDialog, LessonUploadDialog, type LessonTarget } from '@/components/lesson-upload';
import { useReorderDrag } from '@/components/reorder-drag';
import { ApiError } from '@/lib/api';
import { COMPLETION_LABEL, pad2, type Completion } from '@/lib/courses';
import {
  addOp, chapterDown, chapterUp, commitDraft, discardDraft, getDraft, hasUpcoming, lessonDown, lessonTo, lessonUp, undoOp,
  type Draft, type DraftChapter, type InfoChange, type Op,
} from '@/lib/drafts';
import { Link, navigate } from '@/lib/router';
import { cn } from '@/lib/utils';

const fail = (ex: unknown) => toast.error(ex instanceof ApiError ? ex.message : '操作失败');

// A dialog asking for one title: adding or renaming a chapter or lesson.
type Ask = { title: string; label: string; initial: string; done: (title: string) => Op };

const TABS = ['lessons', 'info'] as const;

// CourseEditView is the course editor (/courses/:id/edit). Every change goes
// into the viewer's draft on the server as one operation; the page shows the
// course the draft makes, and the draft takes effect only when committed.
// Replacing the whole course with a package is the other way out: it takes
// effect at once, without the draft.
export function CourseEditView({ id }: { id: number }) {
  const [draft, setDraft] = useState<Draft | null>(null);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [ask, setAsk] = useState<Ask | null>(null);
  const [dialog, setDialog] = useState<'' | 'commit' | 'discard' | 'package-ask' | 'package'>('');
  const [dropChapter, setDropChapter] = useState<DraftChapter | null>(null);
  const [lessonUpload, setLessonUpload] = useState<LessonTarget | null>(null);
  const [previewing, setPreviewing] = useState<string | null>(null);
  const [tab, setTab] = useAddressTab(TABS, 'lessons');
  const [shown, fading] = useTabFade(tab);
  // course information typed but not yet saved to the draft
  const [infoDirty, setInfoDirty] = useState(false);
  // chapters folded away; every chapter starts open, and nothing is kept
  const [folded, setFolded] = useState<ReadonlySet<string>>(() => new Set());
  const isFolded = (key: string) => folded.has(key);
  const toggleFold = (key: string) => setFolded((s) => { const n = new Set(s); if (!n.delete(key)) n.add(key); return n; });

  const load = () => getDraft(id).then(setDraft).catch((e) => setErr(e instanceof ApiError ? e.message : '加载失败'));
  useEffect(() => { void load(); }, [id]); // eslint-disable-line react-hooks/exhaustive-deps

  const run = async (f: () => Promise<Draft>) => {
    setBusy(true);
    try { setDraft(await f()); return true; } catch (ex) { fail(ex); return false; } finally { setBusy(false); }
  };
  const send = (op: Op | null) => (op ? run(() => addOp(id, op)) : Promise.resolve(false));
  const drag = useReorderDrag(draft?.course.chapters ?? [], (op) => void send(op), busy, isFolded);

  if (err) return <div className="page enter"><BackLink id={id} /><Empty glyph="阙" title={err} /></div>;
  if (!draft) return <div className="page enter"><BackLink id={id} /><div className="mt-6 space-y-4">{Array.from({ length: 8 }).map((_, i) => <Skeleton key={i} className="h-5" />)}</div></div>;

  const c = draft.course;
  const changes = draft.ops.length;
  const conflicts = draft.conflicts ?? [];
  const allFolded = c.chapters.length > 0 && c.chapters.every((ch) => folded.has(ch.key));
  let n = 0;
  return (
    <div className="page enter course-edit">
      <BackLink id={id} />
      <PageHead
        kicker="编辑课程"
        title={c.title}
        sub={<span className="draft-status" role="status">{changes ? <>编辑稿里有 <b>{changes}</b> 处修改, {draft.mode === 'request' ? '提交后成为一份更新申请' : '提交后一次生效'}</> : '还没有修改。修改会先存进编辑稿, 换台设备也还在'}</span>}
        actions={<>
          <Button variant="ghost" disabled={!changes || busy} onClick={() => run(() => undoOp(id))}><Undo2 />撤销上一步</Button>
          <Button variant="ghost" disabled={!changes || busy} onClick={() => setDialog('discard')}>放弃编辑稿</Button>
          <Button variant="default" disabled={!changes || busy} onClick={() => setDialog('commit')}>提交</Button>
        </>}
      />
      {draft.mode === 'direct' ? (
        // replacing the course while the draft has changes would stack two
        // sets of changes: commit or discard the draft first
        <div className="package-bar">
          <span>改动很多时, 可以直接用新课程包替换整门课。</span>
          <Button variant="default" disabled={changes > 0 || busy} onClick={() => setDialog('package-ask')}>上传课程包</Button>
        </div>
      ) : (
        <div className="request-note edit-note" role="note">
          <b>提交后成为更新申请</b>
          <span>这门课的修改需要管理员审核, 通过后才生效。再次提交会替换你还在审核中的申请。</span>
        </div>
      )}
      {conflicts.length ? (
        <div className="request-note edit-note conflict-note" role="alert">
          <b>有 {conflicts.length} 处修改无法应用到课程的最新版本</b>
          <span>课程在你编辑期间被更新过。下面的修改已不能应用, 页面显示的是不含它们的结果; 提交时确认后会跳过它们。也可以撤销或放弃编辑稿。</span>
          <ConflictList conflicts={conflicts} chapters={c.chapters} />
        </div>
      ) : null}

      <div className="toc-head ed-tabs">
        <Tabs className="toc-tabs" tabClassName="toc-tab" value={tab} onChange={setTab} items={[
          ['lessons', '章与讲'],
          ['info', <>课程信息{infoDirty ? <span className="tab-dot" role="img" aria-label="有未保存的修改" /> : null}</>],
        ]} />
        {tab === 'lessons' && c.chapters.length ? (
          <Button variant="text" size="bare" onClick={() => setFolded(allFolded ? new Set() : new Set(c.chapters.map((ch) => ch.key)))}>
            {allFolded ? '全部展开' : '全部折叠'}
          </Button>
        ) : null}
      </div>
      <TabPane fading={fading}>
        <div hidden={shown !== 'lessons'}>
          <p className="ed-tab-hint">{drag.fine ? '拖拽调整章和讲的顺序, 也可以把讲拖到别的章; 或用上移、下移、移到某章。' : '调整顺序用上移、下移, 或把讲移到别的章。'}点章名左侧的箭头可以折叠。</p>
          <div className="ed" {...drag.rootProps}>
            {c.chapters.map((ch, ci) => {
              const open = !isFolded(ch.key);
              return (
                <div className={cn('ed-chapter', drag.chapterMark(ci), drag.dragging(ch.key) && 'dragging')} key={ch.key} data-drag-chapter>
                  <div className={cn('ed-row ed-ch', drag.foldedMark(ci))} {...drag.chapterHandle(ch.key)}>
                    {drag.enabled ? <GripVertical className="ed-grip" aria-hidden /> : null}
                    <button type="button" className={cn('ed-fold', open && 'open')} aria-expanded={open} aria-label={open ? '折叠章' : '展开章'} title={open ? '折叠' : '展开'} onClick={() => toggleFold(ch.key)}>
                      <ChevronRight className="chev" />
                    </button>
                    <span className="c-no">第 {pad2(ci + 1)} 章</span>
                    <span className="ed-title">{ch.title}</span>
                    {ch.added ? <span className="tag new">新</span> : null}
                    {open ? null : <span className="ed-count">{ch.lessons.length} 讲</span>}
                    <div className="ed-tools">
                      <Tool label="章改名" onClick={() => setAsk({ title: '修改章名', label: '章名', initial: ch.title, done: (t) => ({ type: 'renameChapter', chapter: ch.key, title: t }) })} disabled={busy}><Pencil /></Tool>
                      <Tool label="章上移" onClick={() => send(chapterUp(c.chapters, ci))} disabled={busy || ci === 0}><ArrowUp /></Tool>
                      <Tool label="章下移" onClick={() => send(chapterDown(c.chapters, ci))} disabled={busy || ci === c.chapters.length - 1}><ArrowDown /></Tool>
                      <Tool label={c.chapters.length === 1 ? '最后一章不能删除' : '删除章'} onClick={() => setDropChapter(ch)} disabled={busy || c.chapters.length === 1}><Trash2 /></Tool>
                    </div>
                  </div>
                  <Fold open={open}>
                    <div className="ed-lessons">
                      {!ch.lessons.length ? <div className={cn('ed-empty', drag.emptyMark(ci))}>这一章还没有讲</div> : null}
                      {ch.lessons.map((l, li) => {
                        const up = lessonUp(c.chapters, ci, li);
                        const down = lessonDown(c.chapters, ci, li);
                        return (
                          <div className={cn('ed-row ed-l', l.upcoming && 'upcoming', drag.lessonMark(ci, li), drag.dragging(l.key) && 'dragging')} key={l.key}
                            data-drag-lesson {...drag.lessonHandle(l.key)}>
                            {drag.enabled ? <GripVertical className="ed-grip" aria-hidden /> : null}
                            <span className="e-no">{pad2(++n)}</span>
                            <span className="ed-title" title={l.title}>{l.title}</span>
                            {l.upcoming ? <span className="upcoming-mark">待更新</span> : null}
                            {l.added ? <span className="tag new">新</span> : l.changed ? <span className="tag new">已替换</span> : null}
                            <div className="ed-tools">
                              {l.changed ? <Tool label="预览" onClick={() => setPreviewing(l.key)}><Eye /></Tool> : null}
                              {l.upcoming
                                ? <Tool label="上传内容" onClick={() => setLessonUpload({ kind: 'fill', lesson: l.key, title: l.title })} disabled={busy}><FileUp /></Tool>
                                : <Tool label="替换讲" onClick={() => setLessonUpload({ kind: 'replace', lesson: l.key, title: l.title })} disabled={busy}><FileUp /></Tool>}
                              <Tool label="讲改名" onClick={() => setAsk({ title: '修改讲的标题', label: '标题', initial: l.title, done: (t) => ({ type: 'renameLesson', lesson: l.key, title: t }) })} disabled={busy}><Pencil /></Tool>
                              <Tool label="讲上移" onClick={() => send(up)} disabled={busy || !up}><ArrowUp /></Tool>
                              <Tool label="讲下移" onClick={() => send(down)} disabled={busy || !down}><ArrowDown /></Tool>
                              {c.chapters.length > 1 ? (
                                <select className="select ed-move" aria-label="移到某章" value="" disabled={busy}
                                  onChange={(e) => { if (e.target.value) void send(lessonTo(c.chapters, l.key, e.target.value)); }}>
                                  <option value="">移到…</option>
                                  {c.chapters.map((o, oi) => (o.key === ch.key ? null : <option key={o.key} value={o.key}>第 {pad2(oi + 1)} 章 {o.title}</option>))}
                                </select>
                              ) : null}
                              <Tool label="删除讲" onClick={() => send({ type: 'deleteLesson', lesson: l.key })} disabled={busy}><Trash2 /></Tool>
                            </div>
                          </div>
                        );
                      })}
                      <div className="ed-adds">
                        <Button variant="text" size="bare" className="ed-add" disabled={busy}
                          onClick={() => setLessonUpload({ kind: 'add', chapter: ch.key, chapterTitle: ch.title })}>
                          <FilePlus2 />添加讲
                        </Button>
                        <Button variant="text" size="bare" className="ed-add" disabled={busy}
                          onClick={() => setAsk({ title: '添加待更新的讲', label: '标题', initial: '', done: (t) => ({ type: 'addUpcomingLesson', chapter: ch.key, title: t }) })}>
                          <Plus />添加待更新的讲
                        </Button>
                      </div>
                    </div>
                  </Fold>
                </div>
              );
            })}
            <Button variant="ghost" className="mt-5" disabled={busy}
              onClick={() => setAsk({ title: '添加章', label: '章名', initial: '', done: (t) => ({ type: 'addChapter', title: t }) })}>
              <Plus />添加章
            </Button>
          </div>
        </div>
        {/* kept mounted while the other tab shows, so typing is not lost */}
        <div hidden={shown !== 'info'}>
          <p className="ed-tab-hint">课程名、完结状态、讲师和介绍。改完点「保存到编辑稿」才会记入编辑稿。</p>
          <InfoForm draft={draft} busy={busy} onSave={(info) => send({ type: 'setInfo', info })} onDirty={setInfoDirty} />
        </div>
      </TabPane>

      <TitleDialog ask={ask} onClose={() => setAsk(null)} onSubmit={async (t) => { if (ask && (await send(ask.done(t)))) setAsk(null); }} />
      <Confirm open={!!dropChapter} title="删除章" confirm="删除" onClose={() => setDropChapter(null)}
        onConfirm={async () => { if (dropChapter && (await send({ type: 'deleteChapter', chapter: dropChapter.key }))) setDropChapter(null); }}>
        {dropChapter ? <>删除「{dropChapter.title}」{dropChapter.lessons.length ? <>会连同其中的 <b>{dropChapter.lessons.length}</b> 讲一起删除</> : ''}。提交前可以撤销。</> : null}
      </Confirm>
      <Confirm open={dialog === 'discard'} title="放弃编辑稿" confirm="放弃" onClose={() => setDialog('')}
        onConfirm={async () => { try { await discardDraft(id); toast.success('已放弃编辑稿'); setDialog(''); await load(); } catch (ex) { fail(ex); } }}>
        编辑稿里的 {changes} 处修改都会丢弃, 无法恢复。
      </Confirm>
      <CommitDialog open={dialog === 'commit'} draft={draft} onClose={() => setDialog('')}
        onCommit={async () => {
          try {
            const r = await commitDraft(id, conflicts.map((x) => x.id));
            if (r.outcome === 'conflicts') {
              // the course changed again since the editor last looked
              setDraft({ ...draft, conflicts: r.conflicts });
              toast.error('课程刚被更新, 有修改无法应用, 请确认');
              return;
            }
            toast.success(r.outcome === 'requested' ? '已提交更新申请, 等待管理员审核' : '课程已更新');
            navigate(`/courses/${id}`);
          } catch (ex) { fail(ex); setDialog(''); }
        }} />
      <Confirm open={dialog === 'package-ask'} title="用课程包替换整门课" confirm="选择课程包" onClose={() => setDialog('')}
        onConfirm={() => setDialog('package')}>
        新课程包会立刻替换整门课: 章和讲按课程清单里的 id 对应, id 不变的讲保留学习记录。
      </Confirm>
      <LessonUploadDialog courseId={id} courseType={c.type} target={lessonUpload} onClose={() => setLessonUpload(null)}
        onDone={(d) => { setDraft(d); setLessonUpload(null); toast.success('已保存到编辑稿'); }} />
      <LessonPreviewDialog courseId={id} lesson={previewing} onClose={() => setPreviewing(null)} />
      {/* the upload says 课程已更新 itself */}
      <UploadDialog courseId={id} open={dialog === 'package'} onClose={() => setDialog('')} onUploaded={() => navigate(`/courses/${id}`)} />
    </div>
  );
}

function BackLink({ id }: { id: number }) {
  return <Link className="back" href={`/courses/${id}`}><ArrowLeft />返回课程</Link>;
}

function Tool({ label, onClick, disabled, children }: { label: string; onClick: () => void; disabled?: boolean; children: ReactNode }) {
  return <Button variant="round" size="round" aria-label={label} title={label} onClick={onClick} disabled={disabled}>{children}</Button>;
}

function TitleDialog({ ask, onClose, onSubmit }: { ask: Ask | null; onClose: () => void; onSubmit: (title: string) => Promise<void> }) {
  const [v, setV] = useState('');
  useEffect(() => { if (ask) setV(ask.initial); }, [ask]);
  const submit = (e: FormEvent) => { e.preventDefault(); if (v.trim()) void onSubmit(v.trim()); };
  return (
    <Dialog open={!!ask} onOpenChange={(o) => { if (!o) onClose(); }} title={ask?.title ?? ''}>
      <form onSubmit={submit} className="mt-4">
        <div className="field"><label htmlFor="ask-title">{ask?.label}</label><Input id="ask-title" autoFocus value={v} maxLength={200} onChange={(e) => setV(e.target.value)} /></div>
        <div className="head-actions">
          <Button onClick={onClose}>取消</Button>
          <Button type="submit" variant="default" disabled={!v.trim() || v.trim() === ask?.initial}>确定</Button>
        </div>
      </form>
    </Dialog>
  );
}

type InfoFields = { title: string; completion: Completion; authorName: string; authorIntro: string; intro: string };

// InfoForm edits the course information; saving adds one operation with
// the fields that changed.
// onDirty hears whether anything typed is not saved yet.
function InfoForm({ draft, busy, onSave, onDirty }: { draft: Draft; busy: boolean; onSave: (info: InfoChange) => Promise<boolean>; onDirty: (dirty: boolean) => void }) {
  const c = draft.course;
  const from = (): InfoFields => ({ title: c.title, completion: c.completion, authorName: c.authorName, authorIntro: c.authorIntro, intro: c.intro });
  const [f, setF] = useState<InfoFields>(from);
  useEffect(() => { setF(from()); }, [c.title, c.completion, c.authorName, c.authorIntro, c.intro]); // eslint-disable-line react-hooks/exhaustive-deps
  const set = (k: keyof InfoFields) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });
  const was = from();
  const changed = (Object.keys(f) as (keyof InfoFields)[]).filter((k) => f[k].trim() !== was[k]);
  const upcoming = hasUpcoming(c);
  const dirty = changed.length > 0;
  useEffect(() => { onDirty(dirty); }, [dirty, onDirty]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    const info: Record<string, string> = {};
    for (const k of changed) info[k] = f[k].trim();
    await onSave(info as InfoChange);
  }

  return (
    <form className="form-rows" onSubmit={submit}>
      {!f.title.trim() ? <ErrorBox message="课程名不能为空" /> : null}
      <div className="field"><label htmlFor="ed-title">课程名</label><Input id="ed-title" value={f.title} maxLength={200} onChange={set('title')} /></div>
      <div className="field"><label htmlFor="ed-completion">完结状态</label>
        <select id="ed-completion" className="select" value={f.completion} onChange={set('completion')}>
          {(Object.keys(COMPLETION_LABEL) as Completion[]).map((k) => (
            <option key={k} value={k} disabled={k === 'finished' && upcoming}>{COMPLETION_LABEL[k]}{k === 'finished' && upcoming ? ' (有待更新的讲)' : ''}</option>
          ))}
        </select>
        {upcoming ? <div className="ed-hint">课程里还有待更新的讲, 不能设为已完结。</div> : null}
      </div>
      <div className="field"><label htmlFor="ed-author">讲师</label><Input id="ed-author" value={f.authorName} onChange={set('authorName')} /></div>
      <div className="field"><label htmlFor="ed-author-intro">讲师简介</label><Input id="ed-author-intro" value={f.authorIntro} onChange={set('authorIntro')} /></div>
      <div className="field"><label htmlFor="ed-intro">课程介绍 (Markdown)</label><textarea id="ed-intro" className="textarea" rows={6} value={f.intro} onChange={set('intro')} /></div>
      <div className="form-foot">
        <Button type="submit" variant="ghost" disabled={busy || !changed.length || !f.title.trim()}>保存到编辑稿</Button>
        {changed.length ? <Button variant="text" size="bare" onClick={() => setF(from())}>还原</Button> : null}
      </div>
    </form>
  );
}

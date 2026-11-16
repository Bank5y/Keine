import { useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { ArrowDown, ArrowLeft, ArrowUp, GripVertical, Pencil, Plus, Trash2, Undo2 } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Confirm } from '@/components/confirm';
import { Empty, ErrorBox, PageHead } from '@/components/common';
import { Skeleton } from '@/components/skeleton';
import { UploadDialog } from '@/components/upload-dialog';
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

// CourseEditView is the course editor (/courses/:id/edit). Every change goes
// into the viewer's draft on the server as one operation; the page shows the
// course the draft makes, and the draft takes effect only when committed.
export function CourseEditView({ id }: { id: number }) {
  const [draft, setDraft] = useState<Draft | null>(null);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [ask, setAsk] = useState<Ask | null>(null);
  const [dialog, setDialog] = useState<'' | 'commit' | 'discard' | 'package'>('');
  const [dropChapter, setDropChapter] = useState<DraftChapter | null>(null);

  const load = () => getDraft(id).then(setDraft).catch((e) => setErr(e instanceof ApiError ? e.message : '加载失败'));
  useEffect(() => { void load(); }, [id]); // eslint-disable-line react-hooks/exhaustive-deps

  const run = async (f: () => Promise<Draft>) => {
    setBusy(true);
    try { setDraft(await f()); return true; } catch (ex) { fail(ex); return false; } finally { setBusy(false); }
  };
  const send = (op: Op | null) => (op ? run(() => addOp(id, op)) : Promise.resolve(false));
  const drag = useReorderDrag(draft?.course.chapters ?? [], (op) => void send(op), busy);

  if (err) return <div className="page enter"><BackLink id={id} /><Empty glyph="阙" title={err} /></div>;
  if (!draft) return <div className="page enter"><BackLink id={id} /><div className="mt-6 space-y-4">{Array.from({ length: 8 }).map((_, i) => <Skeleton key={i} className="h-5" />)}</div></div>;

  const c = draft.course;
  const changes = draft.ops.length;
  let n = 0;
  return (
    <div className="page enter course-edit">
      <BackLink id={id} />
      <PageHead
        kicker="编辑课程"
        title={c.title}
        sub={<span className="draft-status" role="status">{changes ? <>编辑稿里有 <b>{changes}</b> 处修改, 提交后一次生效</> : '还没有修改。修改会先存进编辑稿, 换台设备也还在'}</span>}
        actions={<>
          <Button variant="ghost" disabled={!changes || busy} onClick={() => run(() => undoOp(id))}><Undo2 />撤销上一步</Button>
          <Button variant="ghost" disabled={!changes || busy} onClick={() => setDialog('discard')}>放弃编辑稿</Button>
          <Button variant="default" disabled={!changes || busy} onClick={() => setDialog('commit')}>提交</Button>
        </>}
      />
      {draft.mode === 'request' ? (
        <div className="request-note edit-note" role="note">
          <b>提交后成为更新申请</b>
          <span>这门课的修改需要管理员审核, 通过后才生效。再次提交会替换你还在审核中的申请。</span>
        </div>
      ) : null}

      <div className="sections">
        <section className="section">
          <div className="section-head"><div className="no">01</div><h2>章与讲</h2><p>{drag.enabled ? '拖拽调整章和讲的顺序, 也可以把讲拖到别的章; 或用上移、下移、移到某章。' : '调整顺序用上移、下移, 或把讲移到别的章。'}</p></div>
          <div className="ed" {...drag.rootProps}>
            {c.chapters.map((ch, ci) => (
              <div className={cn('ed-chapter', drag.chapterMark(ci), drag.dragging(ch.key) && 'dragging')} key={ch.key} data-drag-chapter>
                <div className="ed-row ed-ch" {...drag.chapterHandle(ch.key)}>
                  {drag.enabled ? <GripVertical className="ed-grip" aria-hidden /> : null}
                  <span className="c-no">第 {pad2(ci + 1)} 章</span>
                  <span className="ed-title">{ch.title}</span>
                  {ch.added ? <span className="tag new">新</span> : null}
                  <div className="ed-tools">
                    <Tool label="章改名" onClick={() => setAsk({ title: '修改章名', label: '章名', initial: ch.title, done: (t) => ({ type: 'renameChapter', chapter: ch.key, title: t }) })} disabled={busy}><Pencil /></Tool>
                    <Tool label="章上移" onClick={() => send(chapterUp(c.chapters, ci))} disabled={busy || ci === 0}><ArrowUp /></Tool>
                    <Tool label="章下移" onClick={() => send(chapterDown(c.chapters, ci))} disabled={busy || ci === c.chapters.length - 1}><ArrowDown /></Tool>
                    <Tool label={c.chapters.length === 1 ? '最后一章不能删除' : '删除章'} onClick={() => setDropChapter(ch)} disabled={busy || c.chapters.length === 1}><Trash2 /></Tool>
                  </div>
                </div>
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
                        {l.added ? <span className="tag new">新</span> : null}
                        <div className="ed-tools">
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
                  <Button variant="text" size="bare" className="ed-add" disabled={busy}
                    onClick={() => setAsk({ title: '添加待更新的讲', label: '标题', initial: '', done: (t) => ({ type: 'addUpcomingLesson', chapter: ch.key, title: t }) })}>
                    <Plus />添加待更新的讲
                  </Button>
                </div>
              </div>
            ))}
            <Button variant="ghost" className="mt-5" disabled={busy}
              onClick={() => setAsk({ title: '添加章', label: '章名', initial: '', done: (t) => ({ type: 'addChapter', title: t }) })}>
              <Plus />添加章
            </Button>
          </div>
        </section>

        <section className="section">
          <div className="section-head"><div className="no">02</div><h2>课程信息</h2><p>课程名、完结状态、讲师和介绍。</p></div>
          <InfoForm draft={draft} busy={busy} onSave={(info) => send({ type: 'setInfo', info })} />
        </section>

        {draft.mode === 'direct' ? (
          <section className="section">
            <div className="section-head"><div className="no">03</div><h2>用课程包替换整门课</h2><p>改动很多时, 上传完整的新课程包。</p></div>
            <div className="form-rows">
              <p className="ed-hint">新课程包会立刻替换整门课 (整包更新)。章和讲按 <code>course.json</code> 里的 id 对应, id 不变的讲保留学习记录。{changes ? ' 你的编辑稿会保留, 提交时应用到新版本上。' : ''}</p>
              <div><Button variant="ghost" onClick={() => setDialog('package')}>上传课程包</Button></div>
            </div>
          </section>
        ) : null}
      </div>

      <TitleDialog ask={ask} onClose={() => setAsk(null)} onSubmit={async (t) => { if (ask && (await send(ask.done(t)))) setAsk(null); }} />
      <Confirm open={!!dropChapter} title="删除章" confirm="删除" onClose={() => setDropChapter(null)}
        onConfirm={async () => { if (dropChapter && (await send({ type: 'deleteChapter', chapter: dropChapter.key }))) setDropChapter(null); }}>
        {dropChapter ? <>删除「{dropChapter.title}」{dropChapter.lessons.length ? <>会连同其中的 <b>{dropChapter.lessons.length}</b> 讲一起删除</> : ''}。提交前可以撤销。</> : null}
      </Confirm>
      <Confirm open={dialog === 'discard'} title="放弃编辑稿" confirm="放弃" onClose={() => setDialog('')}
        onConfirm={async () => { try { await discardDraft(id); toast.success('已放弃编辑稿'); setDialog(''); await load(); } catch (ex) { fail(ex); } }}>
        编辑稿里的 {changes} 处修改都会丢弃, 无法恢复。
      </Confirm>
      <Confirm open={dialog === 'commit'} title="提交编辑稿" confirm="提交" onClose={() => setDialog('')}
        onConfirm={async () => {
          try {
            const r = await commitDraft(id);
            toast.success(r.outcome === 'requested' ? '已提交更新申请, 等待管理员审核' : '课程已更新');
            navigate(`/courses/${id}`);
          } catch (ex) { fail(ex); setDialog(''); }
        }}>
        {draft.mode === 'request'
          ? <>{changes} 处修改会成为一份更新申请, 管理员批准后生效; 你对这门课还在审核中的申请会被替换。</>
          : <>{changes} 处修改会作为一次课程更新生效。学习进度按讲保留, 订阅者会收到一条「课程有更新」。</>}
      </Confirm>
      <UploadDialog courseId={id} open={dialog === 'package'} onClose={() => setDialog('')} onUploaded={() => { setDialog(''); void load(); }} />
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
function InfoForm({ draft, busy, onSave }: { draft: Draft; busy: boolean; onSave: (info: InfoChange) => Promise<boolean> }) {
  const c = draft.course;
  const from = (): InfoFields => ({ title: c.title, completion: c.completion, authorName: c.authorName, authorIntro: c.authorIntro, intro: c.intro });
  const [f, setF] = useState<InfoFields>(from);
  useEffect(() => { setF(from()); }, [c.title, c.completion, c.authorName, c.authorIntro, c.intro]); // eslint-disable-line react-hooks/exhaustive-deps
  const set = (k: keyof InfoFields) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });
  const was = from();
  const changed = (Object.keys(f) as (keyof InfoFields)[]).filter((k) => f[k].trim() !== was[k]);
  const upcoming = hasUpcoming(c);

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

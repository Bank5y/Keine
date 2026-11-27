import { useCallback, useEffect, useState } from 'react';
import { ArrowLeft } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Empty, ErrorBox } from '@/components/common';
import { useAddressTab } from '@/components/tabs';
import { api, ApiError } from '@/lib/api';
import { conceptRows, filterConcepts, hitPlace, splitAliases, WEIGHTS, type ConceptRow, type LessonSections, type Revision, indexAction, indexError, indexState, lessonNote, lessonState, pathScore, roleName, sourceName, sourceNote, stillBuilding, summaryStage, type ConceptFilter, type IndexConcept, type IndexDetail, type IndexedCourse, type IndexLesson, type RecallTrace, type SummaryTree } from '@/lib/concept-index';
import type { CourseDetail } from '@/lib/courses';
import { Link, match } from '@/lib/router';
import { cn } from '@/lib/utils';

// how often the page looks again while an index is being built
const FOLLOW_MS = 2000;

const at = (s: string) => new Date(s).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });

// AdminConcepts is the concept index page: the list of library courses,
// or one course's index (/admin/concepts/:id).
export function AdminConcepts({ path }: { path: string }) {
  const id = match('/admin/concepts/:id', path)?.id;
  return id ? <ConceptCourse id={Number(id)} /> : <ConceptCourses />;
}

// ConceptCourses lists the concept index of each library course's current
// version (#158): never built on its own, a new version starts without
// one. Building it extracts the lessons that changed since the last
// version and is charged to the site.
function ConceptCourses() {
  const [courses, setCourses] = useState<IndexedCourse[] | null>(null);
  const [err, setErr] = useState('');
  const [starting, setStarting] = useState<number | null>(null);

  const load = () => api<{ courses: IndexedCourse[] }>('GET', '/api/admin/concept-indexes').then((r) => { setCourses(r.courses); setErr(''); }).catch((e) => setErr(e.message));
  useEffect(() => { void load(); }, []);
  const following = !!courses && stillBuilding(courses);
  useEffect(() => {
    if (!following) return;
    const t = setInterval(() => void load(), FOLLOW_MS);
    return () => clearInterval(t);
  }, [following]);

  async function build(c: IndexedCourse) {
    setStarting(c.id);
    try {
      await api('POST', `/api/admin/courses/${c.id}/concept-index`);
      toast.success(`开始为「${c.title}」建立概念索引`);
      await load();
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '操作失败');
    } finally {
      setStarting(null);
    }
  }

  return (
    <>
      <p className="tab-intro">概念索引是课程里各个概念和它们所在讲、小节的对照表, 探索对话用它和课程的段落一起关联课程里的相关内容。段落随新版本自动切好; 概念索引要管理员建立: 新版本从「未建立」开始, 建立时正文、提示词和模型都没变的讲沿用之前的条目。费用记在站点上, 不扣读者的积分。</p>
      <div className="result-bar" style={{ marginTop: 0 }}>
        <span className="count">{courses ? <><b>{courses.length}</b>门课程库课程</> : '正在加载…'}</span>
      </div>
      {err ? <ErrorBox message={err} /> : null}
      {courses && !courses.length ? <Empty title="课程库还没有课程">课程公开后, 在这里为它建立概念索引。</Empty> : null}
      {courses?.length ? (
        <div className="ledger concepts">
          {courses.map((c) => {
            const st = indexState(c.status, c.stale);
            const action = indexAction(c.status, c.stale);
            return (
              <div className="ledger-row" key={c.id} data-course={c.id}>
                <span className="name"><Link href={`/admin/concepts/${c.id}`}>{c.title}</Link></span>
                <span className={cn('st', st.tone)}>{st.label}</span>
                <span className="meta">
                  {c.status === 'built' || c.status === 'partial' ? `${c.entries} 个条目` : `${c.lessons} 讲`}
                  {c.failed ? `  ·  ${c.failed} 讲失败` : ''}
                  {' · '}版本 {at(c.updatedAt)}
                </span>
                <span className="acts">
                  {action ? <Button size="sm" variant={c.status === 'none' ? 'default' : 'ghost'} disabled={starting === c.id} onClick={() => void build(c)}>{action}</Button> : null}
                </span>
              </div>
            );
          })}
        </div>
      ) : null}
    </>
  );
}

const TABS = ['overview', 'tree', 'concepts', 'recall'] as const;
type Tab = (typeof TABS)[number];
const TAB_NAMES: Record<Tab, string> = { overview: '概况', tree: '结构', concepts: '概念', recall: '召回测试' };

// ConceptCourse is one library course's index, its views as tabs. While
// the index is being built the page follows it.
function ConceptCourse({ id }: { id: number }) {
  const [course, setCourse] = useState<CourseDetail | null>(null);
  const [index, setIndex] = useState<IndexDetail | null>(null);
  const [err, setErr] = useState('');
  const [tab, setTab] = useAddressTab(TABS, 'overview');
  const [busy, setBusy] = useState(false);
  const load = useCallback(() => api<IndexDetail>('GET', `/api/admin/courses/${id}/concept-index`).then(setIndex).catch((e) => setErr(e.message)), [id]);
  useEffect(() => {
    api<CourseDetail>('GET', `/api/courses/${id}`).then(setCourse).catch((e) => setErr(e.message));
    void load();
  }, [id, load]);
  const following = index?.status === 'building';
  useEffect(() => {
    if (!following) return;
    const t = setInterval(() => void load(), FOLLOW_MS);
    return () => clearInterval(t);
  }, [following, load]);

  // start posts a build of the whole index, or of one lesson again
  async function start(url: string, done: string) {
    setBusy(true);
    try {
      await api('POST', url);
      toast.success(done);
      await load();
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '操作失败');
    } finally {
      setBusy(false);
    }
  }
  const build = () => start(`/api/admin/courses/${id}/concept-index`, index?.status === 'none' ? '开始建立概念索引' : '开始重建概念索引');
  const retry = (l: IndexLesson) => start(`/api/admin/courses/${id}/concept-index/lessons/${encodeURIComponent(l.key)}`, `开始重试「${l.title}」`);

  const st = index ? indexState(index.status, index.stale) : null;
  const entries = index?.lessons.reduce((n, l) => n + l.entries, 0) ?? 0;
  return (
    <>
      <Link href="/admin/concepts" keepScroll className="usage-back"><ArrowLeft />概念索引</Link>
      {err ? <ErrorBox message={err} /> : null}
      {course ? (
        <div className="concept-head">
          <h2>{course.title}</h2>
          {st ? <span className={cn('st', st.tone)}>{st.label}</span> : null}
          <span className="meta">版本 {at(course.updatedAt)}  ·  {course.chapters.reduce((n, ch) => n + ch.lessons.filter((l) => l.hasText).length, 0)} 讲{entries ? `  ·  ${entries} 个概念` : ''}</span>
        </div>
      ) : null}
      <div className="concept-bar">
        <div className="seg concept-tabs" role="tablist" aria-label="概念索引">
          {TABS.map((t) => <button key={t} type="button" role="tab" aria-selected={tab === t} className={cn(tab === t && 'on')} onClick={() => setTab(t)}>{TAB_NAMES[t]}</button>)}
        </div>
        {tab === 'overview' && index && index.status !== 'building' ? (
          <Button size="sm" variant={index.status === 'none' || index.stale ? 'default' : 'outline'} disabled={busy} onClick={() => void build()}>{index.status === 'none' ? '建立索引' : '重建索引'}</Button>
        ) : null}
      </div>
      {index && tab === 'overview' ? <IndexOverview index={index} busy={busy} retry={retry} /> : null}
      {index && tab === 'tree' ? <IndexTree id={id} title={course?.title ?? ''} version={index.finishedAt} /> : null}
      {index && tab === 'concepts' ? <IndexConcepts id={id} version={index.finishedAt} /> : null}
      {course && tab === 'recall' ? <RecallTest course={course} /> : null}
    </>
  );
}

// IndexOverview is how the index's build went (#224): how far extraction
// got, and each lesson's status, with a retry for the ones that failed.
function IndexOverview({ index, busy, retry }: { index: IndexDetail; busy: boolean; retry: (l: IndexLesson) => void }) {
  if (index.status === 'none') {
    return <Empty title="还没有建立概念索引">建立后, 这里显示每一讲的抽取进度和结果。没有概念索引时, 探索对话仍会用段落关联课程内容。</Empty>;
  }
  const p = index.progress;
  const through = p.done + p.failed;
  const summary = summaryStage(index);
  return (
    <div className="index-overview">
      {index.stale ? <div className="index-stale">索引已过期: 建立时用的提示词版本或模型和现在的设置不一致。重建之前照常用于关联。</div> : null}
      <div className="index-stage">
        <div className="stages">
          <div>
            <div className="stage-head">
              <b>逐讲抽取</b>
              <span className="num">{through} / {p.total}</span>
              {p.failed ? <span className="st warn">{p.failed} 讲失败</span> : null}
            </div>
            <div className="stage-bar" role="progressbar" aria-label="逐讲抽取" aria-valuemin={0} aria-valuemax={p.total} aria-valuenow={through}>
              <span style={{ width: `${p.total ? (through / p.total) * 100 : 0}%` }} />
            </div>
          </div>
          <div>
            <div className="stage-head"><b>课程汇总</b><span className={cn('st', summary.tone)}>{summary.label}</span></div>
            <div className={cn('stage-bar', summary.tone === 'warn' && 'running')}><span style={{ width: summary.tone === 'ok' ? '100%' : summary.tone === 'warn' ? '50%' : '0%' }} /></div>
          </div>
        </div>
        <p className="meta">
          提示词 {index.promptVersion}{index.modelName ? `  ·  模型 ${index.modelName}` : ''}
          {index.startedAt ? `  ·  ${at(index.startedAt)} 开始` : ''}{index.finishedAt ? `  ·  ${at(index.finishedAt)} 结束` : ''}
          {'  ·  费用记在站点上'}
        </p>
      </div>
      <h3 className="usage-h">各讲<small>失败的讲可以单独重试; 正文、提示词和模型都没变的讲沿用之前的条目。</small></h3>
      <div className="ledger index-lessons">
        {index.lessons.map((l) => {
          const ls = lessonState(l);
          return (
            <div className="ledger-row" key={l.key}>
              <span className="min-w-0"><span className="name">{l.title}</span><small>{l.chapter}</small></span>
              <span className={cn('st', ls.tone)}>{ls.label}</span>
              <span className={cn('note', l.status === 'failed' && 'why')}>{lessonNote(l)}</span>
              <span className="acts">
                {l.status === 'failed' && index.status !== 'building' ? <Button size="sm" variant="outline" disabled={busy} onClick={() => retry(l)}>重试</Button> : null}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// useIndexPart loads a part of the index (its concepts, its tree), again
// whenever a build finishes.
function useIndexPart<T>(url: string, version: string | null): [T | null, string] {
  const [data, setData] = useState<T | null>(null);
  const [err, setErr] = useState('');
  useEffect(() => {
    let live = true;
    api<T>('GET', url).then((d) => { if (live) setData(d); }).catch((e) => { if (live) setErr(e.message); });
    return () => { live = false; };
  }, [url, version]);
  return [data, err];
}

// IndexTree is the summary tree (#225): the course's summary, then each
// chapter's with its lessons'.
function IndexTree({ id, title, version }: { id: number; title: string; version: string | null }) {
  const [tree, err] = useIndexPart<SummaryTree>(`/api/admin/courses/${id}/concept-index/tree`, version);
  if (err) return <ErrorBox message={err} />;
  if (!tree) return <p className="usage-note">正在加载…</p>;
  if (!tree.chapters.length) return <Empty title="还没有摘要">建立概念索引后, 这里按「课程 → 章 → 讲」列出各级摘要。</Empty>;
  return (
    <div className="index-tree">
      <p className="tab-intro">课程 → 章 → 讲的摘要。章摘要由这一章各讲的摘要写成, 课程摘要由各章摘要写成; 一章太长时只标出来, 不切分。</p>
      <section className="tree-course">
        <h3><small>课程</small>{title}</h3>
        <p>{tree.summary || (tree.error ? `没有写成: ${indexError(tree.error)}` : '还没有摘要。')}</p>
      </section>
      {tree.chapters.map((ch, i) => (
        <section className="tree-chapter" key={ch.key}>
          <h3>
            <small>第 {i + 1} 章</small>{ch.title}
            {ch.tooLong ? <span className="tree-long" title="这一章的讲太多, 一份摘要概括不好; 不会自动切分">这一章太长</span> : null}
          </h3>
          <p className={cn(!ch.summary && 'none')}>{ch.summary || (ch.error ? `没有写成: ${indexError(ch.error)}` : '还没有摘要。')}</p>
          <ol>
            {ch.lessons.map((l) => (
              <li key={l.key}><span className="name">{l.title}</span><span className={cn('sum', !l.summary && 'none')}>{l.summary || (l.status === 'failed' ? '抽取失败, 没有摘要。' : '没有摘要。')}</span></li>
            ))}
          </ol>
        </section>
      ))}
    </div>
  );
}

const FILTERS: [ConceptFilter, string][] = [['all', '全部'], ['specific', '非泛词'], ['generic', '泛词'], ['revised', '已修订'], ['disabled', '禁用'], ['stale', '可能过时']];

// IndexConcepts lists the index's concepts merged across lessons (#225),
// with the admin's revisions laid over them (#226): searchable, filtered,
// each opening to where it appears; the admin revises a concept, adds one
// and leaves lessons or sections out of recall, which takes effect at once.
function IndexConcepts({ id, version }: { id: number; version: string | null }) {
  const base = `/api/admin/courses/${id}/concept-index`;
  const [data, err] = useIndexPart<{ concepts: IndexConcept[] }>(`${base}/concepts`, version);
  const [revisions, setRevisions] = useState<Revision[]>([]);
  const [lessons, setLessons] = useState<LessonSections[]>([]);
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState<ConceptFilter>('all');
  const [open, setOpen] = useState<string | null>(null);
  const [editing, setEditing] = useState<ConceptRow | 'new' | null>(null);
  const [excluding, setExcluding] = useState(false);
  const loadRevisions = useCallback(() => api<{ revisions: Revision[] }>('GET', `${base}/revisions`).then((r) => setRevisions(r.revisions)).catch((e) => toast.error(e.message)), [base]);
  useEffect(() => { void loadRevisions(); }, [loadRevisions]);
  useEffect(() => { api<{ lessons: LessonSections[] }>('GET', `${base}/sections`).then((r) => setLessons(r.lessons)).catch(() => {}); }, [base]);

  async function act(method: string, url: string, body: unknown, done: string) {
    try {
      await api(method, url, body);
      toast.success(done);
      await loadRevisions();
      return true;
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '操作失败');
      return false;
    }
  }

  if (err) return <ErrorBox message={err} />;
  if (!data) return <p className="usage-note">正在加载…</p>;
  const rows = conceptRows(data.concepts, revisions);
  const shown = filterConcepts(rows, query, filter);
  return (
    <div className="index-concepts">
      <div className="concepts-bar">
        <Input className="concepts-search h-9" type="search" placeholder="搜索概念名或别名" aria-label="搜索概念" value={query} onChange={(e) => setQuery(e.target.value)} />
        <div className="seg" role="group" aria-label="筛选概念">
          {FILTERS.map(([f, label]) => <button key={f} type="button" className={cn(filter === f && 'on')} aria-pressed={filter === f} onClick={() => setFilter(f)}>{label}</button>)}
        </div>
        <span className="count"><b>{shown.length}</b>个概念</span>
        <Button size="sm" variant="outline" onClick={() => setExcluding(true)}>排除讲或小节</Button>
        <Button size="sm" variant="default" onClick={() => setEditing('new')}>新增概念</Button>
      </div>
      {!rows.length ? <Empty title="还没有概念">建立概念索引后, 各讲抽到的概念在这里合并列出; 也可以手动新增。</Empty> : (
        <div className="ledger concept-list">
          <div className="concept-row head"><span>概念</span><span>主讲解处</span><span>出现</span><span /></div>
          {shown.map((r) => {
            const where = r.concept ? r.concept.primary : r.revision?.lesson ? { lesson: r.revision.lesson, section: r.revision.section } : null;
            return (
              <div key={r.key} className={cn('concept-item', open === r.key && 'open', r.disabled && 'off')}>
                <div className="concept-row" role="button" tabIndex={0} aria-expanded={open === r.key}
                  onClick={() => setOpen(open === r.key ? null : r.key)} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setOpen(open === r.key ? null : r.key); } }}>
                  <span className="min-w-0"><span className="name">{r.name}</span>{r.aliases.length ? <small>{r.aliases.join('  ·  ')}</small> : null}</span>
                  <span className="where">{where ? hitPlace(where) : ''}</span>
                  <span className="num">{r.concept ? `${r.concept.lessons} 讲` : ''}</span>
                  <span className="tags">
                    {r.generic ? <span className="tag">泛词</span> : null}
                    {!r.concept ? <span className="tag rev">手动新增</span> : r.revision ? <span className="tag rev">已修订</span> : null}
                    {r.weight !== 1 ? <span className="tag rev">×{r.weight}</span> : null}
                    {r.disabled ? <span className="tag off">禁用</span> : null}
                    {r.stale ? <span className="tag stale" title="它指向的那一讲正文变了, 或小节没有了; 仍然生效">可能过时</span> : null}
                    {r.stale && r.revision ? <Button size="sm" variant="outline" onClick={(e) => { e.stopPropagation(); void act('POST', `${base}/revisions/${r.revision!.id}/confirm`, undefined, '已确认'); }}>确认</Button> : null}
                    <span className="caret" aria-hidden>{open === r.key ? '▾' : '▸'}</span>
                  </span>
                </div>
                {open === r.key ? (
                  <div className="concept-open">
                    {r.summary ? <p className="concept-sum">{r.summary}</p> : null}
                    {r.concept ? (
                      <ol className="occurrences">
                        {r.concept.occurrences.map((o, i) => (
                          <li key={i}>
                            <span className="where">{hitPlace(o)}</span>
                            <span className={cn('role', o.role)}>{roleName(o.role)}</span>
                            <span className="primary">{o.lesson.key === r.concept!.primary.lesson.key && o.section?.id === r.concept!.primary.section?.id ? '主讲解处' : ''}</span>
                            <span className="sum">{o.summary}</span>
                          </li>
                        ))}
                      </ol>
                    ) : null}
                    <div className="concept-acts"><Button size="sm" variant="outline" onClick={() => setEditing(r)}>{r.concept ? '修订' : '编辑'}</Button></div>
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
      )}
      {editing ? <ConceptDialog row={editing === 'new' ? null : editing} lessons={lessons} onClose={() => setEditing(null)} act={act} base={base} /> : null}
      <ExcludeDialog open={excluding} onClose={() => setExcluding(false)} revisions={revisions.filter((r) => r.kind === 'exclude')} lessons={lessons} act={act} base={base} />
    </div>
  );
}

type Act = (method: string, url: string, body: unknown, done: string) => Promise<boolean>;

// ConceptDialog revises a concept extracted, edits one added, or adds one
// at a section of a lesson.
function ConceptDialog({ row, lessons, onClose, act, base }: { row: ConceptRow | null; lessons: LessonSections[]; onClose: () => void; act: Act; base: string }) {
  const extracted = row?.concept;
  const [name, setName] = useState(row?.name ?? '');
  const [aliases, setAliases] = useState((row?.aliases ?? []).join(', '));
  const [summary, setSummary] = useState(row?.summary ?? '');
  const [weight, setWeight] = useState(row?.weight ?? 1);
  const [generic, setGeneric] = useState(row?.generic ?? false);
  const [disabled, setDisabled] = useState(row?.disabled ?? false);
  const [lessonKey, setLessonKey] = useState(row?.revision?.lesson?.key ?? lessons[0]?.lesson.key ?? '');
  const [sectionTitle, setSectionTitle] = useState(row?.revision?.section?.title ?? '');
  const sections = lessons.find((l) => l.lesson.key === lessonKey)?.sections ?? [];

  async function save(e: React.FormEvent) {
    e.preventDefault();
    const ok = extracted
      ? await act('PUT', `${base}/revisions/concept`, {
        concept: extracted.name, weight, disabled,
        name: name.trim() !== extracted.name ? name.trim() : undefined,
        aliases: aliases !== extracted.aliases.join(', ') ? splitAliases(aliases) : undefined,
        summary: summary !== extracted.summary ? summary : undefined,
        generic: generic !== extracted.generic ? generic : undefined,
      }, '修订已保存, 立即生效')
      : await act('POST', `${base}/revisions/new`, { name, aliases: splitAliases(aliases), summary, weight, lessonKey, sectionTitle }, row ? '已保存' : '已新增概念');
    if (ok) onClose();
  }
  async function takeBack() {
    if (row?.revision && await act('DELETE', `${base}/revisions/${row.revision.id}`, undefined, extracted ? '已撤销修订' : '已删除')) onClose();
  }

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }} title={extracted ? '修订概念' : row ? '编辑新增的概念' : '新增概念'} wide>
      <form onSubmit={save} className="mt-4 concept-form">
        <p className="hint">{extracted ? `抽取时叫「${extracted.name}」。修订挂在课程上, 改完立即生效, 新版本重建后照样生效。` : '抽取漏掉的概念可以手动加上, 挂到它被讲解的讲和小节上。'}</p>
        <div className="field"><label htmlFor="cn">名称</label><Input id="cn" autoFocus value={name} onChange={(e) => setName(e.target.value)} /></div>
        <div className="field"><label htmlFor="ca">别名</label><Input id="ca" value={aliases} onChange={(e) => setAliases(e.target.value)} /><div className="hint">用逗号分开; 回答里出现任何一个都算提到这个概念。</div></div>
        <div className="field"><label htmlFor="cs">摘要</label><textarea id="cs" className="textarea" rows={3} value={summary} onChange={(e) => setSummary(e.target.value)} /></div>
        {!extracted ? (
          <div className="field-pair">
            <div className="field"><label htmlFor="cl">讲</label>
              <select id="cl" className="select" value={lessonKey} onChange={(e) => { setLessonKey(e.target.value); setSectionTitle(''); }}>
                {lessons.map((l) => <option key={l.lesson.key} value={l.lesson.key}>{l.lesson.title}</option>)}
              </select>
            </div>
            <div className="field"><label htmlFor="ct">小节标题</label>
              <select id="ct" className="select" value={sectionTitle} onChange={(e) => setSectionTitle(e.target.value)}>
                <option value="">讲的开头</option>
                {sections.map((sec) => <option key={sec.id} value={sec.title}>{sec.title}</option>)}
              </select>
            </div>
          </div>
        ) : null}
        <div className="field">
          <label>权重<span className="hint inline">作用在概念这一路的 RRF 分数上, 不强制置顶。</span></label>
          <div className="seg" role="group" aria-label="权重">
            {WEIGHTS.map(([w, label]) => <button key={w} type="button" className={cn(weight === w && 'on')} aria-pressed={weight === w} onClick={() => setWeight(w)}>{label}</button>)}
          </div>
        </div>
        {extracted ? (
          <div className="field checks">
            <label className="check"><input type="checkbox" checked={generic} onChange={(e) => setGeneric(e.target.checked)} />标为泛词<span className="hint inline">泛词不参与召回</span></label>
            <label className="check"><input type="checkbox" checked={disabled} onChange={(e) => setDisabled(e.target.checked)} />禁用<span className="hint inline">禁用后这个概念不参与召回, 也不出现在关联里</span></label>
          </div>
        ) : null}
        <div className="head-actions">
          {row?.revision ? <Button type="button" variant="ghost" className="mr-auto text-brand" onClick={() => void takeBack()}>{extracted ? '撤销修订' : '删除'}</Button> : null}
          <Button type="button" onClick={onClose}>取消</Button>
          <Button type="submit" variant="default" disabled={!name.trim() || (!extracted && !lessonKey)}>{row ? '保存' : '添加'}</Button>
        </div>
      </form>
    </Dialog>
  );
}

// ExcludeDialog lists the lessons and sections left out of recall, and
// leaves out another.
function ExcludeDialog({ open, onClose, revisions, lessons, act, base }: { open: boolean; onClose: () => void; revisions: Revision[]; lessons: LessonSections[]; act: Act; base: string }) {
  const [lessonKey, setLessonKey] = useState('');
  const [sectionId, setSectionId] = useState('');
  const key = lessonKey || lessons[0]?.lesson.key || '';
  const sections = lessons.find((l) => l.lesson.key === key)?.sections ?? [];
  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) onClose(); }} title="排除讲或小节" wide>
      <div className="mt-4">
        <p className="hint">被排除的讲或小节不出现在两路召回里, 比如开篇词、结束语、直播加餐。</p>
        {revisions.length ? (
          <ul className="exclusions">
            {revisions.map((r) => (
              <li key={r.id}>
                <span className="name">{r.lesson?.title}</span>
                <span className="what">{r.section ? r.section.title : '整讲'}</span>
                <span>{r.stale ? <span className="tag stale">可能过时</span> : null}</span>
                <Button size="sm" variant="ghost" className="text-brand" onClick={() => void act('DELETE', `${base}/revisions/${r.id}`, undefined, '已移除')}>移除</Button>
              </li>
            ))}
          </ul>
        ) : <p className="usage-note">还没有排除任何讲或小节。</p>}
        <div className="field">
          <label htmlFor="xl">添加</label>
          <div className="exclude-add">
            <select id="xl" className="select" value={key} onChange={(e) => { setLessonKey(e.target.value); setSectionId(''); }}>
              {lessons.map((l) => <option key={l.lesson.key} value={l.lesson.key}>{l.lesson.title}</option>)}
            </select>
            <select className="select" aria-label="小节" value={sectionId} onChange={(e) => setSectionId(e.target.value)}>
              <option value="">整讲</option>
              {sections.map((sec) => <option key={sec.id} value={sec.id}>{sec.title}</option>)}
            </select>
            <Button disabled={!key} onClick={() => void act('POST', `${base}/revisions/exclude`, { lessonKey: key, sectionId }, '已排除')}>排除</Button>
          </div>
        </div>
        <div className="head-actions"><Button variant="default" onClick={onClose}>完成</Button></div>
      </div>
    </Dialog>
  );
}

// how many hits of each path the recall test lists
const SHOWN_HITS = 10;

// RecallTest shows what the linking step would be offered for a text the
// admin pastes, leaving out the lesson it is from (#222). It calls no
// model, so it costs nothing.
function RecallTest({ course }: { course: CourseDetail }) {
  const lessons = course.chapters.flatMap((ch) => ch.lessons.filter((l) => l.hasText));
  const [text, setText] = useState('');
  const [except, setExcept] = useState('');
  const [trace, setTrace] = useState<RecallTrace | null>(null);
  const [busy, setBusy] = useState(false);

  async function run() {
    setBusy(true);
    try {
      setTrace(await api<RecallTrace>('POST', `/api/admin/courses/${course.id}/concept-index/recall`, { text, excludeLesson: except }));
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '召回失败');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="recall-test">
      <p className="tab-intro">粘贴一段回答, 选它所在的讲 (这一讲不参与召回), 看各路召回的排名和分数, 以及融合后的前 5 个候选。不调用模型, 不计用量。</p>
      <div className="field">
        <label htmlFor="recall-text">回答文本</label>
        <textarea id="recall-text" className="textarea" rows={4} value={text} onChange={(e) => setText(e.target.value)} />
      </div>
      <div className="field">
        <label htmlFor="recall-except">排除的讲</label>
        <div className="recall-go">
          <select id="recall-except" className="select" value={except} onChange={(e) => setExcept(e.target.value)}>
            <option value="">不排除</option>
            {lessons.map((l) => <option key={l.key} value={l.key}>{l.title}</option>)}
          </select>
          <Button disabled={busy || !text.trim()} onClick={() => void run()}>召回</Button>
        </div>
      </div>
      {trace ? (
        <>
          <h3 className="usage-h">融合结果<small>RRF k=60  ·  每讲最多一个  ·  前 5</small></h3>
          {trace.candidates.length ? (
            <ol className="ledger recall-fused">
              {trace.candidates.map((c, i) => (
                <li key={`${c.lesson.key}/${c.section?.id ?? ''}`} className="ledger-row">
                  <span className="rank">{i + 1}</span>
                  <span className="min-w-0"><span className="name">{c.label}</span><small>{hitPlace(c)}</small></span>
                  <span className="sources">{c.sources.map((s) => <span key={s} className={cn('src', s)}>{sourceName(s, true)}</span>)}</span>
                  <span className="meta">{c.score.toFixed(4)}</span>
                </li>
              ))}
            </ol>
          ) : <p className="usage-note">没有召回到候选。</p>}
          {trace.revisions.length ? (
            <div className="recall-revisions">
              <b>修订的影响</b>
              <ul>{trace.revisions.map((r) => <li key={r.id}><span className="name">{r.label}</span>{r.effect}</li>)}</ul>
            </div>
          ) : null}
          <h3 className="usage-h recall-paths-h">分路排名</h3>
          <div className="recall-paths">
            {trace.paths.map((p) => (
              <section key={p.source} className="recall-path">
                <h4>{sourceName(p.source)}<small>{sourceNote(p.source)}</small></h4>
                {p.hits.length ? (
                  <ol>
                    {p.hits.slice(0, SHOWN_HITS).map((h, i) => (
                      <li key={i}>
                        <span className="rank">{i + 1}</span>
                        <span className="what">{h.label || h.section?.title || h.lesson.title}{h.weight && h.weight !== 1 ? <small className="weight">×{h.weight}</small> : null}</span>
                        <span className="where">{h.lesson.title}</span>
                        <span className="score">{pathScore(p.source, h.score)}</span>
                      </li>
                    ))}
                  </ol>
                ) : <p className="usage-note">这一路没有命中。</p>}
                {p.hits.length > SHOWN_HITS ? <p className="usage-note">共 {p.hits.length} 条, 只显示前 {SHOWN_HITS} 条。</p> : null}
              </section>
            ))}
          </div>
        </>
      ) : null}
    </div>
  );
}

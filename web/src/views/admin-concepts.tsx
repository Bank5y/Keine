import { useCallback, useEffect, useState } from 'react';
import { ArrowLeft } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Empty, ErrorBox } from '@/components/common';
import { useAddressTab } from '@/components/tabs';
import { api, ApiError } from '@/lib/api';
import { filterConcepts, hitPlace, indexAction, indexError, indexState, lessonNote, lessonState, pathScore, roleName, sourceName, sourceNote, stillBuilding, summaryStage, type ConceptFilter, type IndexConcept, type IndexDetail, type IndexedCourse, type IndexLesson, type RecallTrace, type SummaryTree } from '@/lib/concept-index';
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

const FILTERS: [ConceptFilter, string][] = [['all', '全部'], ['specific', '非泛词'], ['generic', '泛词']];

// IndexConcepts lists the index's concepts merged across lessons (#225):
// searchable, filtered by whether they are generic, each opening to where
// it appears.
function IndexConcepts({ id, version }: { id: number; version: string | null }) {
  const [data, err] = useIndexPart<{ concepts: IndexConcept[] }>(`/api/admin/courses/${id}/concept-index/concepts`, version);
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState<ConceptFilter>('all');
  const [open, setOpen] = useState<number | null>(null);
  if (err) return <ErrorBox message={err} />;
  if (!data) return <p className="usage-note">正在加载…</p>;
  const shown = filterConcepts(data.concepts, query, filter);
  return (
    <div className="index-concepts">
      <div className="concepts-bar">
        <Input className="concepts-search h-9" type="search" placeholder="搜索概念名或别名" aria-label="搜索概念" value={query} onChange={(e) => setQuery(e.target.value)} />
        <div className="seg" role="group" aria-label="是否泛词">
          {FILTERS.map(([f, label]) => <button key={f} type="button" className={cn(filter === f && 'on')} aria-pressed={filter === f} onClick={() => setFilter(f)}>{label}</button>)}
        </div>
        <span className="count"><b>{shown.length}</b>个概念</span>
      </div>
      {!data.concepts.length ? <Empty title="还没有概念">建立概念索引后, 各讲抽到的概念在这里合并列出。</Empty> : (
        <div className="ledger concept-list">
          <div className="concept-row head"><span>概念</span><span>主讲解处</span><span>出现</span><span /></div>
          {shown.map((c) => (
            <div key={c.id} className={cn('concept-item', open === c.id && 'open')}>
              <button type="button" className="concept-row" aria-expanded={open === c.id} onClick={() => setOpen(open === c.id ? null : c.id)}>
                <span className="min-w-0"><span className="name">{c.name}</span>{c.aliases.length ? <small>{c.aliases.join('  ·  ')}</small> : null}</span>
                <span className="where">{hitPlace(c.primary)}</span>
                <span className="num">{c.lessons} 讲</span>
                <span className="tags">{c.generic ? <span className="tag">泛词</span> : null}<span className="caret" aria-hidden>{open === c.id ? '▾' : '▸'}</span></span>
              </button>
              {open === c.id ? (
                <ol className="occurrences">
                  {c.occurrences.map((o, i) => (
                    <li key={i}>
                      <span className="where">{hitPlace(o)}</span>
                      <span className={cn('role', o.role)}>{roleName(o.role)}</span>
                      <span className="primary">{o.lesson.key === c.primary.lesson.key && o.section?.id === c.primary.section?.id ? '主讲解处' : ''}</span>
                      <span className="sum">{o.summary}</span>
                    </li>
                  ))}
                </ol>
              ) : null}
            </div>
          ))}
        </div>
      )}
    </div>
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
                        <span className="what">{h.label || h.section?.title || h.lesson.title}</span>
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

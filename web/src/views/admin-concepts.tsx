import { useEffect, useState } from 'react';
import { ArrowLeft } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Empty, ErrorBox } from '@/components/common';
import { useAddressTab } from '@/components/tabs';
import { api, ApiError } from '@/lib/api';
import { hitPlace, indexAction, indexError, indexState, pathScore, sourceName, sourceNote, stillBuilding, type IndexedCourse, type RecallTrace } from '@/lib/concept-index';
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
      <p className="tab-intro">概念索引是课程里各个概念和它们所在讲、小节的对照表, 探索对话用它关联课程里的相关内容。索引不会自动建立: 课程发了新版本, 新版本从「未建立」开始; 建立时没改动的讲沿用上一版的条目, 只抽取改动过的讲。费用记在站点上, 不扣读者的积分。</p>
      <div className="result-bar" style={{ marginTop: 0 }}>
        <span className="count">{courses ? <><b>{courses.length}</b>门课程库课程</> : '正在加载…'}</span>
      </div>
      {err ? <ErrorBox message={err} /> : null}
      {courses && !courses.length ? <Empty title="课程库还没有课程">课程公开后, 在这里为它建立概念索引。</Empty> : null}
      {courses?.length ? (
        <div className="ledger concepts">
          {courses.map((c) => {
            const st = indexState(c.status);
            const action = indexAction(c.status);
            return (
              <div className="ledger-row" key={c.id} data-course={c.id}>
                <span className="name"><Link href={`/admin/concepts/${c.id}`}>{c.title}</Link></span>
                <span className={cn('st', st.tone)}>{st.label}</span>
                <span className="meta">
                  {c.status === 'built' ? `${c.entries} 个条目` : c.status === 'failed' && c.error ? `原因: ${indexError(c.error)}` : `${c.lessons} 讲`}
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

const TABS = ['recall'] as const;
type Tab = (typeof TABS)[number];
const TAB_NAMES: Record<Tab, string> = { recall: '召回测试' };

// ConceptCourse is one library course's index, its views as tabs.
function ConceptCourse({ id }: { id: number }) {
  const [course, setCourse] = useState<CourseDetail | null>(null);
  const [index, setIndex] = useState<IndexedCourse | null>(null);
  const [err, setErr] = useState('');
  const [tab, setTab] = useAddressTab(TABS, 'recall');
  useEffect(() => {
    api<CourseDetail>('GET', `/api/courses/${id}`).then(setCourse).catch((e) => setErr(e.message));
    api<{ courses: IndexedCourse[] }>('GET', '/api/admin/concept-indexes').then((r) => setIndex(r.courses.find((c) => c.id === id) ?? null)).catch(() => {});
  }, [id]);
  const st = index ? indexState(index.status) : null;
  return (
    <>
      <Link href="/admin/concepts" keepScroll className="usage-back"><ArrowLeft />概念索引</Link>
      {err ? <ErrorBox message={err} /> : null}
      {course ? (
        <div className="concept-head">
          <h2>{course.title}</h2>
          {st ? <span className={cn('st', st.tone)}>{st.label}</span> : null}
          {index ? <span className="meta">版本 {at(index.updatedAt)}  ·  {index.lessons} 讲{index.status === 'built' ? `  ·  ${index.entries} 个概念` : ''}</span> : null}
        </div>
      ) : null}
      <div className="seg concept-tabs" role="tablist" aria-label="概念索引">
        {TABS.map((t) => <button key={t} type="button" role="tab" aria-selected={tab === t} className={cn(tab === t && 'on')} onClick={() => setTab(t)}>{TAB_NAMES[t]}</button>)}
      </div>
      {course && tab === 'recall' ? <RecallTest course={course} /> : null}
    </>
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

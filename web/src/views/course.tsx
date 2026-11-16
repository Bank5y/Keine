import { useEffect, useMemo, useRef, useState } from 'react';
import { ArrowLeft, BookOpen, ChevronRight, PlayCircle, Search } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Empty } from '@/components/common';
import { CourseActions } from '@/components/course-actions';
import { Fold, useChapterFolds } from '@/components/fold';
import { SubscribeButton } from '@/components/peek';
import { Highlight } from '@/components/highlight';
import { ProgressBar, StateMark } from '@/components/progress';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { Cover } from '@/components/shelf';
import { Skeleton } from '@/components/skeleton';
import { api } from '@/lib/api';
import { COMPLETION_LABEL, PUBLISH_LABEL, TYPE_LABEL, lessonURL, pad2, type ChapterItem, type CourseDetail } from '@/lib/courses';
import { Link, navigate } from '@/lib/router';
import { useScrollFade } from '@/lib/use-scroll-fade';
import { cn } from '@/lib/utils';

export function useCourse(id: number) {
  const [course, setCourse] = useState<CourseDetail | null>(null);
  const [err, setErr] = useState('');
  const reload = () => api<CourseDetail>('GET', `/api/courses/${id}`).then(setCourse).catch((e) => setErr(e.message));
  useEffect(() => { setCourse(null); setErr(''); void reload(); }, [id]); // eslint-disable-line react-hooks/exhaustive-deps
  return { course, err, reload };
}

type Tab = 'intro' | 'toc';

// readTab is the tab named in the address (?tab=), if any.
const readTab = (): Tab | null => {
  const t = new URLSearchParams(location.search).get('tab');
  return t === 'intro' || t === 'toc' ? t : null;
};

const hasIntro = (c: CourseDetail) => !!(c.intro || c.authorName);

export function CourseView({ id }: { id: number }) {
  const { course, err, reload } = useCourse(id);
  // the introduction comes first unless there is none; the chosen tab is kept
  // in the address so coming back from a lesson returns to it
  const [picked, setPicked] = useState<Tab | null>(readTab);
  const tab: Tab = picked ?? (!course || hasIntro(course) ? 'intro' : 'toc');
  const setTab = (t: Tab) => {
    setPicked(t);
    history.replaceState(history.state, '', `${location.pathname}?tab=${t}${location.hash}`);
  };
  const [q, setQ] = useState('');
  const colophonBody = useRef<HTMLDivElement>(null);
  useScrollFade(colophonBody);

  if (err) return <div className="page enter"><BackLink /><Empty glyph="阙" title={err} /></div>;
  return (
    <div className="page enter">
      <div className="course">
        <aside className="colophon">
          <BackLink library={!!course && course.visibility === 'public' && !course.subscribed} />
          {course ? <SubscribeButton c={course} onSubscribed={() => void reload()} className="side-subscribe" /> : null}
          <div className="colophon-body side-scroll side-fade" ref={colophonBody}>{course ? <Colophon c={course} onChange={reload} /> : <Skeleton className="aspect-[3/4] w-full" />}</div>
        </aside>
        <section>
          <div className="toc-head">
            <div className="toc-tabs" role="tablist">
              <button role="tab" aria-selected={tab === 'intro'} className={cn('toc-tab', tab === 'intro' && 'active')} onClick={() => setTab('intro')}>课程介绍</button>
              <button role="tab" aria-selected={tab === 'toc'} className={cn('toc-tab', tab === 'toc' && 'active')} onClick={() => setTab('toc')}>
                目录{course ? <sup>{course.lessonCount}</sup> : null}
              </button>
            </div>
            {tab === 'toc' ? (
              <label className="search-sm">
                <Search />
                <input placeholder="查找讲或章" aria-label="查找讲或章" value={q} onChange={(e) => setQ(e.target.value)} />
              </label>
            ) : null}
          </div>
          {!course ? <Lines /> : (tab === 'toc' ? <Toc c={course} q={q.trim()} /> : <Intro c={course} />)}
        </section>
      </div>
    </div>
  );
}

// ContinueButton opens the lesson studied last, or the first lesson.
function ContinueButton({ c }: { c: CourseDetail }) {
  const first = c.chapters[0]?.lessons[0];
  const target = c.progress.lastLessonId ?? first?.id;
  if (!target) return null;
  const started = c.progress.lastLessonId != null;
  return (
    <Button variant="default" size="block" onClick={() => navigate(lessonURL(c.id, target))}>
      <BookOpen />{started ? '继续学习' : '开始阅读'}
    </Button>
  );
}

function BackLink({ library }: { library?: boolean }) {
  return library ? <Link className="back" href="/library"><ArrowLeft />课程库</Link> : <Link className="back" href="/mine"><ArrowLeft />我的课程</Link>;
}

function Lines() {
  return <div className="mt-4 space-y-4">{Array.from({ length: 8 }).map((_, i) => <Skeleton key={i} className="h-4" />)}</div>;
}

function Colophon({ c, onChange }: { c: CourseDetail; onChange: () => void }) {
  return (
    <>
      <div className="book-cover"><Cover course={c} /></div>
      <div>
        <h1>{c.title}</h1>
        <div className="tags">
          <span className="tag">{TYPE_LABEL[c.type]}</span>
          <span className={cn('tag', c.completion)}>{COMPLETION_LABEL[c.completion]}</span>
          {c.visibility === 'private' ? <span className={cn('tag private', c.publishState)}>{PUBLISH_LABEL[c.publishState]}</span> : null}
        </div>
        <dl className="spec">
          <dt>讲数</dt><dd className="num">{c.lessonCount}</dd>
          <dt>章数</dt><dd className="num">{c.chapters.length}</dd>
          {c.authorName ? <><dt>讲师</dt><dd>{c.authorName}</dd></> : null}
          {c.contributors.length ? <><dt>贡献者</dt><dd>{c.contributors.join('、')}</dd></> : null}
        </dl>
        <div className="course-progress">
          <div className="cp-head"><span>学习进度</span><b>{c.progress.done} / {c.progress.total}</b></div>
          <ProgressBar done={c.progress.done} total={c.progress.total} />
        </div>
        <div className="stack"><ContinueButton c={c} /></div>
        <CourseActions c={c} onChange={onChange} />
      </div>
    </>
  );
}

// filterToc keeps lessons whose title matches; a matching chapter keeps all
// of its lessons.
function filterToc(chapters: ChapterItem[], q: string) {
  if (!q) return chapters;
  const ql = q.toLowerCase();
  return chapters
    .map((ch) => (ch.title.toLowerCase().includes(ql) ? ch : { ...ch, lessons: ch.lessons.filter((l) => l.title.toLowerCase().includes(ql)) }))
    .filter((ch) => ch.lessons.length);
}

// progressChapter is the chapter of the lesson studied last, or the first.
function progressChapter(c: CourseDetail) {
  const last = c.progress.lastLessonId;
  return (c.chapters.find((ch) => ch.lessons.some((l) => l.id === last)) ?? c.chapters[0])?.id;
}

// Toc opens the chapter the reader has got to. A search opens every chapter
// with a match; clearing it brings back the chapters opened before.
function Toc({ c, q }: { c: CourseDetail; q: string }) {
  const folds = useChapterFolds(() => { const ch = progressChapter(c); return ch == null ? [] : [ch]; });
  const [closed, setClosed] = useState<{ q: string; ids: Set<number> }>({ q: '', ids: new Set() });
  const searchClosed = closed.q === q ? closed.ids : new Set<number>();
  const isOpen = (id: number) => (q ? !searchClosed.has(id) : folds.isOpen(id));
  const toggle = (id: number) => {
    if (!q) return folds.toggle(id);
    const ids = new Set(searchClosed);
    if (!ids.delete(id)) ids.add(id);
    setClosed({ q, ids });
  };

  // lesson numbers run through the whole course
  const numbers = useMemo(() => {
    const m = new Map<number, number>();
    let n = 0;
    for (const ch of c.chapters) for (const l of ch.lessons) m.set(l.id, ++n);
    return m;
  }, [c]);
  const chapterNo = useMemo(() => new Map(c.chapters.map((ch, i) => [ch.id, i + 1])), [c]);
  const shown = filterToc(c.chapters, q);
  if (!shown.length) return <Empty title="没有匹配的讲" />;
  return (
    <div>
      {shown.map((ch) => (
        <div className="chapter" key={ch.id}>
          <button type="button" className={cn('chapter-head', isOpen(ch.id) && 'open')} aria-expanded={isOpen(ch.id)} onClick={() => toggle(ch.id)}>
            <ChevronRight className="chev" />
            <span className="c-no">第 {pad2(chapterNo.get(ch.id)!)} 章</span>
            <span className="c-title"><Highlight text={ch.title} q={q} /></span>
            <span className={cn('c-meta', ch.done === ch.lessons.length && 'complete')}>{ch.done} / {ch.lessons.length}</span>
          </button>
          <Fold open={isOpen(ch.id)}>
            <div className="lessons">
              {ch.lessons.map((l) => (
                <Link className="entry" key={l.id} href={lessonURL(c.id, l.id)}>
                  <span className="e-no">{pad2(numbers.get(l.id)!)}</span>
                  {l.video ? <span className="e-icon" title="视频"><PlayCircle /></span> : null}
                  <span className="e-title" title={l.title}><Highlight text={l.title} q={q} /></span>
                  {l.duration ? <span className="e-dur">{l.duration}</span> : null}
                  <span className="e-lead" />
                  <span className="e-state"><StateMark state={l.state} /></span>
                </Link>
              ))}
            </div>
          </Fold>
        </div>
      ))}
    </div>
  );
}

function Intro({ c }: { c: CourseDetail }) {
  if (!c.intro && !c.authorName) return <Empty glyph="简" title="暂无课程介绍"><div>课程清单里没有提供介绍</div></Empty>;
  return (
    <>
      {c.intro ? <LazyMarkdown text={c.intro} className="mt-6" /> : null}
      {c.authorName ? (
        <div className="author-card">
          <div className="kicker">Author</div>
          <span className="name">{c.authorName}</span>
          {c.authorIntro ? <span className="role">{c.authorIntro}</span> : null}
        </div>
      ) : null}
    </>
  );
}

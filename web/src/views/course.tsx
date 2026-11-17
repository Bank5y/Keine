import { useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ArrowLeft, BookOpen, ChevronRight, PlayCircle, Plus } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Empty } from '@/components/common';
import { CourseActions, CourseTools } from '@/components/course-actions';
import { Fold, useChapterFolds } from '@/components/fold';
import { SubscribeButton, subscribe } from '@/components/peek';
import { TabPane, Tabs, useAddressTab, useTabFade } from '@/components/tabs';
import { ProgressBar, StateMark } from '@/components/progress';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { Cover } from '@/components/shelf';
import { Skeleton } from '@/components/skeleton';
import { api } from '@/lib/api';
import { readTarget, readerBar } from '@/lib/course-actions';
import { COMPLETION_LABEL, PUBLISH_LABEL, RETENTION_LABEL, TYPE_LABEL, lessonURL, pad2, type CourseDetail } from '@/lib/courses';
import { Link, navigate } from '@/lib/router';
import { useNarrow } from '@/lib/use-narrow';
import { useScrollFade } from '@/lib/use-scroll-fade';
import { useStuck } from '@/lib/use-stuck';
import { cn } from '@/lib/utils';

export function useCourse(id: number) {
  const [course, setCourse] = useState<CourseDetail | null>(null);
  const [err, setErr] = useState('');
  const reload = () => api<CourseDetail>('GET', `/api/courses/${id}`).then(setCourse).catch((e) => setErr(e.message));
  useEffect(() => { setCourse(null); setErr(''); void reload(); }, [id]); // eslint-disable-line react-hooks/exhaustive-deps
  return { course, err, reload };
}

const TABS = ['intro', 'toc'] as const;

export function CourseView({ id }: { id: number }) {
  const { course, err, reload } = useCourse(id);
  // the table of contents comes first; the chosen tab is kept in the
  // address so coming back from a lesson returns to it
  const [tab, setTab] = useAddressTab(TABS, 'toc');
  const [shown, fading] = useTabFade(tab);
  const colophonBody = useRef<HTMLDivElement>(null);
  useScrollFade(colophonBody);
  // a phone gets the course's actions as a bar at the bottom and tools
  // beside the tabs; the colophon keeps only the title, tags and progress
  const narrow = useNarrow();

  if (err) return <div className="page enter"><BackLink /><Empty glyph="阙" title={err} /></div>;
  return (
    <div className="page enter">
      <div className="course">
        <aside className="colophon">
          <BackLink library={!!course && course.visibility === 'public' && !course.subscribed} />
          {course && !narrow ? <SubscribeButton c={course} onSubscribed={() => void reload()} className="side-subscribe" /> : null}
          <div className="colophon-body side-scroll side-fade" ref={colophonBody}>{course ? <Colophon c={course} onChange={reload} narrow={narrow} /> : <Skeleton className={narrow ? 'h-24 w-full' : 'aspect-[3/4] w-full'} />}</div>
        </aside>
        <section>
          <div className="toc-head">
            <Tabs className="toc-tabs" tabClassName="toc-tab" value={tab} onChange={setTab} items={[
              ['intro', '课程介绍'],
              ['toc', <>目录{course ? <sup>{course.lessonCount}</sup> : null}</>],
            ]} />
            {course && narrow ? <CourseTools c={course} onChange={reload} /> : null}
          </div>
          {course?.retention ? (
            <div className="retention-note" role="status">
              该课程{RETENTION_LABEL[course.retention]}，你保留的 {course.retainedCount ?? 0} 讲仍可阅读
            </div>
          ) : null}
          {!course ? <Lines /> : <TabPane fading={fading}>{shown === 'toc' ? <Toc c={course} /> : <Intro c={course} />}</TabPane>}
        </section>
      </div>
      {course && narrow ? <CourseBar c={course} onSubscribed={() => void reload()} /> : null}
    </div>
  );
}

// CourseBar is the narrow course page's bottom bar, in place of the nav:
// subscribing and reading (see readerBar). It is drawn on the body: the
// page's entrance animation moves it, which would hold a fixed bar to the
// page instead of the window.
function CourseBar({ c, onSubscribed }: { c: CourseDetail; onSubscribed: () => void }) {
  const { subscribe: canSubscribe, read } = readerBar(c);
  return createPortal(
    <div className="tabbar course-bar">
      {canSubscribe ? (
        <Button variant="outline" size="block" onClick={() => void subscribe(c).then((ok) => { if (ok) onSubscribed(); })}><Plus />订阅课程</Button>
      ) : null}
      {read ? <Button variant="default" size="block" onClick={() => navigate(lessonURL(c.id, read.lessonId))}><BookOpen />{read.label}</Button> : null}
    </div>
,
    document.body,
  );
}

// ContinueButton opens the lesson studied last, or the first readable lesson
// (see readTarget). Unlike the narrow course bar, it says 开始阅读 until the
// reader has studied a lesson, subscribed or not.
function ContinueButton({ c }: { c: CourseDetail }) {
  const target = readTarget(c);
  if (!target) return null;
  return (
    <Button variant="default" size="block" onClick={() => navigate(lessonURL(c.id, target.lessonId))}>
      <BookOpen />{target.started ? '继续学习' : '开始阅读'}
    </Button>
  );
}

// BackLink stays in view: in the sticky column on a wide screen, and as a
// bar under the masthead on a middling one (which shows its edge once
// stuck). A phone lets it scroll away: the top bar and the course bar
// already frame the page.
function BackLink({ library }: { library?: boolean }) {
  const ref = useRef<HTMLAnchorElement>(null);
  useStuck(ref);
  return library ? <Link ref={ref} className="back" href="/library"><ArrowLeft />课程库</Link> : <Link ref={ref} className="back" href="/mine"><ArrowLeft />我的课程</Link>;
}

function Lines() {
  return <div className="mt-4 space-y-4">{Array.from({ length: 8 }).map((_, i) => <Skeleton key={i} className="h-4" />)}</div>;
}

function Colophon({ c, onChange, narrow }: { c: CourseDetail; onChange: () => void; narrow: boolean }) {
  return (
    <>
      {narrow ? null : <div className="book-cover"><Cover course={c} /></div>}
      <div>
        <h1>{c.title}</h1>
        <div className="tags">
          <span className="tag">{TYPE_LABEL[c.type]}</span>
          <span className={cn('tag', c.completion)}>{COMPLETION_LABEL[c.completion]}</span>
          {c.retention ? <span className="tag retained">{RETENTION_LABEL[c.retention]}</span>
            : c.visibility === 'private' ? <span className={cn('tag private', c.publishState)}>{PUBLISH_LABEL[c.publishState]}</span> : null}
        </div>
        {narrow ? null : <dl className="spec">
          <dt>讲数</dt><dd className="num">{c.lessonCount}</dd>
          <dt>章数</dt><dd className="num">{c.chapters.length}</dd>
          {c.authorName ? <><dt>讲师</dt><dd>{c.authorName}</dd></> : null}
          {c.contributors.length ? <><dt>贡献者</dt><dd>{c.contributors.join('、')}</dd></> : null}
        </dl>}
        <div className="course-progress">
          <div className="cp-head"><span>学习进度</span><b>{c.progress.done} / {c.progress.total}</b></div>
          <ProgressBar done={c.progress.done} total={c.progress.total} />
        </div>
        {narrow ? null : <>
          <div className="stack"><ContinueButton c={c} /></div>
          {c.retention ? null : <CourseActions c={c} onChange={onChange} />}
        </>}
      </div>
    </>
  );
}

// progressChapter is the chapter of the lesson studied last, or the first.
function progressChapter(c: CourseDetail) {
  const last = c.progress.lastLessonId;
  return (c.chapters.find((ch) => ch.lessons.some((l) => l.id === last)) ?? c.chapters[0])?.id;
}

// Toc opens the chapter the reader has got to; the reader opens or closes
// the others.
function Toc({ c }: { c: CourseDetail }) {
  const folds = useChapterFolds(() => { const ch = progressChapter(c); return ch == null ? [] : [ch]; });

  // lesson numbers run through the whole course
  const numbers = useMemo(() => {
    const m = new Map<number, number>();
    let n = 0;
    for (const ch of c.chapters) for (const l of ch.lessons) m.set(l.id, ++n);
    return m;
  }, [c]);
  const chapterNo = useMemo(() => new Map(c.chapters.map((ch, i) => [ch.id, i + 1])), [c]);
  return (
    <div>
      {c.chapters.map((ch) => (
        <div className="chapter" key={ch.id}>
          <button type="button" className={cn('chapter-head', folds.isOpen(ch.id) && 'open')} aria-expanded={folds.isOpen(ch.id)} onClick={() => folds.toggle(ch.id)}>
            <ChevronRight className="chev" />
            <span className="c-no">第 {pad2(chapterNo.get(ch.id)!)} 章</span>
            <span className="c-title">{ch.title}</span>
            {ch.total ? <span className={cn('c-meta', ch.done === ch.total && 'complete')}>{ch.done} / {ch.total}</span> : <span className="c-meta upcoming-mark">待更新</span>}
          </button>
          <Fold open={folds.isOpen(ch.id)}>
            <div className="lessons">
              {!ch.lessons.length ? <span className="entry upcoming empty-chapter" aria-disabled="true"><span className="e-title">这一章还没有讲</span><span className="e-lead" /><span className="e-state upcoming-mark">待更新</span></span> : null}
              {ch.lessons.map((l) => l.upcoming ? (
                <span className="entry upcoming" key={l.id} aria-disabled="true">
                  <span className="e-no">{pad2(numbers.get(l.id)!)}</span>
                  <span className="e-title" title={l.title}>{l.title}</span>
                  <span className="e-lead" />
                  <span className="e-state upcoming-mark">待更新</span>
                </span>
              ) : l.unavailable ? (
                <span className="entry unavailable" key={l.id} aria-disabled="true">
                  <span className="e-no">{pad2(numbers.get(l.id)!)}</span>
                  {l.video ? <span className="e-icon" title="视频"><PlayCircle /></span> : null}
                  <span className="e-title" title={l.title}>{l.title}</span>
                  <span className="e-lead" />
                  <span className="e-state gone-mark">已下架</span>
                </span>
              ) : (
                <Link className="entry" key={l.id} href={lessonURL(c.id, l.id)}>
                  <span className="e-no">{pad2(numbers.get(l.id)!)}</span>
                  {l.video ? <span className="e-icon" title="视频"><PlayCircle /></span> : null}
                  <span className="e-title" title={l.title}>{l.title}</span>
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

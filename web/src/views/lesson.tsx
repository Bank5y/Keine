import { useEffect, useMemo, useRef, useState, type MouseEvent } from 'react';
import { ArrowLeft, Check, ChevronLeft, ChevronRight, ListTree, PanelLeft } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Empty } from '@/components/common';
import { Drawer } from '@/components/drawer';
import { ProgressBar, StateMark } from '@/components/progress';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { Skeleton } from '@/components/skeleton';
import { ImageZoom } from '@/components/zoom';
import { api } from '@/lib/api';
import { lessonURL, pad2, type CourseDetail, type LessonItem, type LessonState, type LessonText } from '@/lib/courses';
import { useReadingProgress } from '@/lib/use-reading-progress';
import { Link, navigate } from '@/lib/router';
import { useHeadings, type Heading } from '@/lib/use-headings';
import { cn } from '@/lib/utils';
import { useCourse } from '@/views/course';

// a text that opens with its own "# title" shows that instead of ours
const opensWithTitle = (md: string) => /^\s*#\s/.test(md);

// neighbours finds the lessons before and after one, across chapters.
function neighbours(c: CourseDetail | null, lessonId: number) {
  const flat: LessonItem[] = c ? c.chapters.flatMap((ch) => ch.lessons) : [];
  const i = flat.findIndex((l) => l.id === lessonId);
  return { prev: i > 0 ? flat[i - 1] : null, next: i >= 0 && i < flat.length - 1 ? flat[i + 1] : null };
}

const isField = (t: EventTarget | null) => t instanceof HTMLElement && /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName);

export function LessonView({ courseId, lessonId }: { courseId: number; lessonId: number }) {
  const { course, reload } = useCourse(courseId);
  const [lesson, setLesson] = useState<LessonText | null>(null);
  const [err, setErr] = useState('');
  const [zoom, setZoom] = useState<string | null>(null);
  const [drawer, setDrawer] = useState<'' | 'toc' | 'headings'>('');
  const doc = useRef<HTMLElement>(null);
  const { headings, active } = useHeadings(doc, lesson?.id);
  const { prev, next } = useMemo(() => neighbours(course, lessonId), [course, lessonId]);
  const { state, setDone } = useReadingProgress(courseId, lesson, doc, reload);

  useEffect(() => {
    setLesson(null);
    setErr('');
    setDrawer('');
    api<LessonText>('GET', `/api/courses/${courseId}/lessons/${lessonId}`).then((l) => {
      setLesson(l);
      document.title = `${l.title} · Keine`;
    }).catch((e) => setErr(e.message));
    return () => { document.title = 'Keine'; };
  }, [courseId, lessonId]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.altKey || e.ctrlKey || e.metaKey || isField(e.target)) return;
      const to = e.key === 'ArrowLeft' ? prev : (e.key === 'ArrowRight' ? next : null);
      if (to) navigate(lessonURL(courseId, to.id));
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [courseId, prev, next]);

  const onClick = (e: MouseEvent) => {
    const t = e.target as HTMLElement;
    if (t.tagName === 'IMG' && t.closest('.prose')) setZoom((t as HTMLImageElement).src);
  };

  const toc = course ? <CourseToc c={course} current={lessonId} /> : <Skeleton className="h-40" />;
  const onThisPage = <HeadingToc headings={headings} active={active} onPick={() => setDrawer('')} />;

  return (
    <div className="page enter docs" onClick={onClick}>
      <aside className="docs-side docs-left" aria-label="课程目录">
        <Link className="back" href={`/courses/${courseId}`}><ArrowLeft />返回课程</Link>
        {toc}
      </aside>

      <div className="docs-main">
        <div className="docs-bar">
          <Button variant="ghost" size="sm" onClick={() => setDrawer('toc')}><PanelLeft />目录</Button>
          {headings.length ? <Button variant="ghost" size="sm" className="docs-bar-headings" onClick={() => setDrawer('headings')}><ListTree />本讲</Button> : null}
        </div>
        {err ? <Empty glyph="阙" title={err} /> : null}
        {!err && !lesson ? <ReaderSkeleton /> : null}
        {lesson ? (
          <article className="reader-doc" ref={doc}>
            {course ? <div className="kicker">{course.title}</div> : null}
            {opensWithTitle(lesson.markdown) ? null : <h1>{lesson.title}</h1>}
            {lesson.markdown ? <LazyMarkdown text={lesson.markdown} base={lesson.base} className="reader-prose" />
              : <p className="text-ink-3">这一讲没有文字内容。</p>}
          </article>
        ) : null}
        {lesson ? <DoneBar state={state} onToggle={() => setDone(state !== 'done')} /> : null}
        {lesson ? (
          <nav className="reader-nav" aria-label="翻页">
            <NavCard courseId={courseId} item={prev} dir="prev" />
            <NavCard courseId={courseId} item={next} dir="next" />
          </nav>
        ) : null}
      </div>

      <aside className="docs-side docs-right" aria-label="本讲目录">{headings.length ? onThisPage : null}</aside>

      <Drawer open={drawer === 'toc'} onOpenChange={(v) => setDrawer(v ? 'toc' : '')} side="left" title="课程目录">{toc}</Drawer>
      <Drawer open={drawer === 'headings'} onOpenChange={(v) => setDrawer(v ? 'headings' : '')} side="right" title="本讲目录">{onThisPage}</Drawer>
      <ImageZoom src={zoom} onClose={() => setZoom(null)} />
    </div>
  );
}

function DoneBar({ state, onToggle }: { state: LessonState; onToggle: () => void }) {
  return (
    <div className="done-bar">
      {state === 'done'
        ? <><span className="ok"><Check />这一讲已学完</span><Button variant="text" size="bare" onClick={onToggle}>标记为未学完</Button></>
        : <><span>读到这里了?</span><Button variant="outline" size="sm" onClick={onToggle}><Check />标记为已学完</Button></>}
    </div>
  );
}

function CourseToc({ c, current }: { c: CourseDetail; current: number }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => { ref.current?.querySelector('.current')?.scrollIntoView({ block: 'nearest' }); }, [current]);
  let n = 0;
  return (
    <div className="side-toc" ref={ref}>
      <div className="side-title">{c.title}</div>
      <div className="side-progress"><ProgressBar done={c.progress.done} total={c.progress.total} /><span>{c.progress.done} / {c.progress.total}</span></div>
      {c.chapters.map((ch, i) => (
        <div className="side-ch" key={ch.id}>
          <div className="side-ch-head"><span className="no">{pad2(i + 1)}</span>{ch.title}</div>
          {ch.lessons.map((l) => {
            n++;
            return (
              <Link key={l.id} href={lessonURL(c.id, l.id)} className={cn('side-l', l.id === current && 'current')} aria-current={l.id === current ? 'page' : undefined}>
                <span className="no">{pad2(n)}</span>
                <span className="t">{l.title}</span>
                <StateMark state={l.state} compact />
              </Link>
            );
          })}
        </div>
      ))}
    </div>
  );
}

function HeadingToc({ headings, active, onPick }: { headings: Heading[]; active: string; onPick: () => void }) {
  return (
    <div className="on-page">
      <div className="on-page-title">本讲目录</div>
      {headings.map((h) => (
        <a key={h.id} href={'#' + h.id} className={cn('op-item', h.level === 3 && 'sub', h.id === active && 'active')}
          onClick={(e) => { e.preventDefault(); document.getElementById(h.id)?.scrollIntoView({ behavior: 'smooth' }); history.replaceState(null, '', '#' + h.id); onPick(); }}>
          {h.text}
        </a>
      ))}
    </div>
  );
}

function NavCard({ courseId, item, dir }: { courseId: number; item: LessonItem | null; dir: 'prev' | 'next' }) {
  if (!item) return <span className={`rn ${dir} none`}><span className="lbl">{dir === 'prev' ? '已是第一讲' : '已是最后一讲'}</span></span>;
  return (
    <Link className={`rn ${dir}`} href={lessonURL(courseId, item.id)}>
      <span className="lbl">{dir === 'prev' ? <><ChevronLeft />上一讲</> : <>下一讲<ChevronRight /></>}</span>
      <span className="t">{item.title}</span>
    </Link>
  );
}

function ReaderSkeleton() {
  return (
    <div className="reader-doc space-y-4">
      <Skeleton className="h-3 w-1/3" />
      <Skeleton className="my-6 h-8 w-4/5" />
      {Array.from({ length: 8 }).map((_, i) => <Skeleton key={i} className="h-4" />)}
    </div>
  );
}

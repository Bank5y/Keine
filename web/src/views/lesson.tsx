import { useEffect, useMemo, useRef, useState, type MouseEvent } from 'react';
import { ArrowDown, ArrowLeft, ArrowUp, Check, ChevronLeft, ChevronRight, ListTree, PanelLeft } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Empty } from '@/components/common';
import { Drawer } from '@/components/drawer';
import { FOLD_MS, Fold, useChapterFolds } from '@/components/fold';
import { ProgressBar, StateMark } from '@/components/progress';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { Skeleton } from '@/components/skeleton';
import { LessonVideoView } from '@/components/video';
import { ImageZoom } from '@/components/zoom';
import { api } from '@/lib/api';
import { lessonURL, pad2, type CourseDetail, type LessonItem, type LessonState, type LessonText } from '@/lib/courses';
import { useReadingProgress } from '@/lib/use-reading-progress';
import { Link, navigate } from '@/lib/router';
import { useHeadings, type Heading } from '@/lib/use-headings';
import { useScrollFade } from '@/lib/use-scroll-fade';
import { cn } from '@/lib/utils';
import { useCourse } from '@/views/course';

// a text that opens with its own "# title" shows that instead of ours; a
// video lesson keeps our title above the player and drops the text's
const opensWithTitle = (md: string) => /^\s*#\s/.test(md);
const withoutTitle = (md: string) => md.replace(/^\s*#\s[^\n]*\n?/, '');

// neighbours finds the lessons before and after one, across chapters.
function neighbours(c: CourseDetail | null, lessonId: number) {
  const flat: LessonItem[] = c ? c.chapters.flatMap((ch) => ch.lessons) : [];
  const i = flat.findIndex((l) => l.id === lessonId);
  return { prev: i > 0 ? flat[i - 1] : null, next: i >= 0 && i < flat.length - 1 ? flat[i + 1] : null };
}

// how long a switch may keep the previous lesson on screen before the
// skeleton takes its place
const STALE_MS = 300;

const isField = (t: EventTarget | null) => t instanceof HTMLElement && /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName);

export function LessonView({ courseId, lessonId }: { courseId: number; lessonId: number }) {
  const { course, reload } = useCourse(courseId);
  // switching lessons keeps this view (and the course sidebar) mounted: the
  // previous lesson stays on screen, faded, until the next one arrives
  const [lesson, setLesson] = useState<LessonText | null>(null);
  const [slow, setSlow] = useState(false);
  const [err, setErr] = useState('');
  const [zoom, setZoom] = useState<string | null>(null);
  const [drawer, setDrawer] = useState<'' | 'toc' | 'headings'>('');
  const doc = useRef<HTMLElement>(null);
  const right = useRef<HTMLElement>(null);
  const current = lesson?.id === lessonId ? lesson : null;
  const shown = current ?? (slow ? null : lesson);
  const { headings: found, active } = useHeadings(doc, shown?.id);
  const headings = current ? found : [];
  const { prev, next } = useMemo(() => neighbours(course, lessonId), [course, lessonId]);
  const { state, setDone, autoDone, savePosition } = useReadingProgress(courseId, current, doc, reload);
  useScrollFade(right);

  useEffect(() => {
    let alive = true;
    setErr('');
    setDrawer('');
    setZoom(null);
    setSlow(false);
    const t = setTimeout(() => setSlow(true), STALE_MS);
    api<LessonText>('GET', `/api/courses/${courseId}/lessons/${lessonId}`).then((l) => {
      if (!alive) return;
      setLesson(l);
      document.title = `${l.title} · Keine`;
    }).catch((e) => { if (alive) setErr(e.message); }).finally(() => clearTimeout(t));
    return () => { alive = false; clearTimeout(t); document.title = 'Keine'; };
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
        <Link className="back" href={`/courses/${courseId}?tab=toc`}><ArrowLeft />返回课程</Link>
        {course ? <CourseToc c={course} current={lessonId} docked /> : <Skeleton className="h-40" />}
      </aside>

      <div className="docs-main">
        <div className="docs-bar">
          <Button variant="ghost" size="sm" onClick={() => setDrawer('toc')}><PanelLeft />目录</Button>
          {headings.length ? <Button variant="ghost" size="sm" className="docs-bar-headings" onClick={() => setDrawer('headings')}><ListTree />本讲</Button> : null}
        </div>
        {err ? <Empty glyph="阙" title={err} /> : null}
        {!err && !shown ? <ReaderSkeleton /> : null}
        {!err && shown ? (
          <article key={shown.id} className={cn('reader-doc', current ? 'fresh' : 'stale')} ref={doc} aria-busy={!current}>
            {course ? <div className="kicker">{course.title}</div> : null}
            {shown.video || !opensWithTitle(shown.markdown) ? <h1>{shown.title}</h1> : null}
            {shown.video ? <LessonVideoView video={shown.video} position={shown.position} onPosition={savePosition} onNearlyDone={autoDone} /> : null}
            {shown.markdown ? <LazyMarkdown text={shown.video ? withoutTitle(shown.markdown) : shown.markdown} base={shown.base} className="reader-prose" />
              : (shown.video ? null : <p className="text-ink-3">这一讲没有文字内容。</p>)}
          </article>
        ) : null}
        {current ? <DoneBar state={state} onToggle={() => setDone(state !== 'done')} /> : null}
        {!err && shown ? (
          <nav className="reader-nav" aria-label="翻页">
            <NavCard courseId={courseId} item={prev} dir="prev" />
            <NavCard courseId={courseId} item={next} dir="next" />
          </nav>
        ) : null}
      </div>

      <aside className="docs-side docs-right side-fade" ref={right} aria-label="本讲目录">{headings.length ? onThisPage : null}</aside>

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

// CourseToc lists the course by chapter; the chapter of the current lesson
// opens by itself. Docked in the sidebar it scrolls on its own, fades at the
// edges and offers a button back to the current lesson once it is out of view.
function CourseToc({ c, current, docked }: { c: CourseDetail; current: number; docked?: boolean }) {
  const box = useRef<HTMLDivElement>(null);
  const ref = useRef<HTMLDivElement>(null);
  const chapterOf = useMemo(() => new Map(c.chapters.flatMap((ch) => ch.lessons.map((l) => [l.id, ch.id] as const))), [c]);
  const folds = useChapterFolds(() => { const ch = chapterOf.get(current); return ch == null ? [] : [ch]; });
  const [locate, setLocate] = useState<'up' | 'down' | null>(null);
  const currentCh = chapterOf.get(current);
  const currentOpen = currentCh != null && folds.isOpen(currentCh);

  // centre brings the current lesson to the middle of the box, after its
  // chapter has finished opening
  const centre = (behavior: ScrollBehavior, wait: number) => setTimeout(() => {
    const b = box.current;
    const row = b?.querySelector<HTMLElement>('.side-l.current');
    if (!b || !row) return;
    const r = row.getBoundingClientRect();
    const br = b.getBoundingClientRect();
    b.scrollTo({ top: b.scrollTop + r.top - br.top - (b.clientHeight - r.height) / 2, behavior });
  }, wait);

  // a new current lesson: open its chapter and, if it is out of view, bring
  // it in; the drawer just shows it
  const first = useRef(true);
  useEffect(() => {
    const wasOpen = currentOpen;
    if (currentCh != null) folds.expand(currentCh);
    if (!docked) {
      ref.current?.querySelector('.current')?.scrollIntoView({ block: 'nearest' });
      return undefined;
    }
    if (first.current) { first.current = false; const t = centre('instant', 0); return () => clearTimeout(t); }
    const b = box.current;
    const row = b?.querySelector('.side-l.current');
    if (!b || !row) return undefined;
    const r = row.getBoundingClientRect();
    const br = b.getBoundingClientRect();
    if (wasOpen && r.top >= br.top && r.bottom <= br.bottom) return undefined;
    const t = centre('smooth', wasOpen ? 0 : FOLD_MS);
    return () => clearTimeout(t);
  }, [current]); // eslint-disable-line react-hooks/exhaustive-deps

  // the button shows while the current lesson is hidden in a closed chapter
  // or scrolled out of view, pointing the way to it
  const check = () => {
    const b = box.current;
    if (!b || currentCh == null) return;
    const target = b.querySelector<HTMLElement>(currentOpen ? '.side-l.current' : `[data-chapter="${currentCh}"]`);
    if (!target) return;
    const r = target.getBoundingClientRect();
    const br = b.getBoundingClientRect();
    const dir = r.top < br.top ? 'up' : (r.bottom > br.bottom ? 'down' : null);
    setLocate(currentOpen ? dir : (dir ?? 'down'));
  };
  useScrollFade(docked ? box : noBox, check);
  useEffect(check); // the open chapters may have changed

  let n = 0;
  const list = (
    <div className="side-toc" ref={ref}>
      <div className="side-title">{c.title}</div>
      <div className="side-progress"><ProgressBar done={c.progress.done} total={c.progress.total} /><span>{c.progress.done} / {c.progress.total}</span></div>
      {c.chapters.map((ch, i) => {
        const open = folds.isOpen(ch.id);
        return (
          <div className="side-ch" key={ch.id}>
            <button type="button" className={cn('side-ch-head', open && 'open')} data-chapter={ch.id} aria-expanded={open} onClick={() => folds.toggle(ch.id)}>
              <ChevronRight className="chev" /><span className="no">{pad2(i + 1)}</span><span className="t">{ch.title}</span>
            </button>
            <Fold open={open}>
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
            </Fold>
          </div>
        );
      })}
    </div>
  );
  if (!docked) return list;
  return (
    <>
      <div className="side-scroll side-fade" ref={box}>{list}</div>
      {locate ? (
        <button type="button" className="side-locate" onClick={() => { if (currentCh != null) folds.expand(currentCh); centre('smooth', currentOpen ? 0 : FOLD_MS); }}>
          {locate === 'up' ? <ArrowUp /> : <ArrowDown />}当前讲
        </button>
      ) : null}
    </>
  );
}

const noBox = { current: null };

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

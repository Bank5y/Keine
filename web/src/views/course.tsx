import { useEffect, useMemo, useState } from 'react';
import { ArrowLeft, BookOpen, PlayCircle, Search } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Empty } from '@/components/common';
import { CourseActions } from '@/components/course-actions';
import { startLearning } from '@/components/peek';
import { Highlight } from '@/components/highlight';
import { ProgressBar, StateMark } from '@/components/progress';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { Cover } from '@/components/shelf';
import { Skeleton } from '@/components/skeleton';
import { api } from '@/lib/api';
import { COMPLETION_LABEL, PUBLISH_LABEL, TYPE_LABEL, lessonURL, pad2, type ChapterItem, type CourseDetail } from '@/lib/courses';
import { Link, navigate } from '@/lib/router';
import { cn } from '@/lib/utils';

export function useCourse(id: number) {
  const [course, setCourse] = useState<CourseDetail | null>(null);
  const [err, setErr] = useState('');
  const reload = () => api<CourseDetail>('GET', `/api/courses/${id}`).then(setCourse).catch((e) => setErr(e.message));
  useEffect(() => { setCourse(null); setErr(''); void reload(); }, [id]); // eslint-disable-line react-hooks/exhaustive-deps
  return { course, err, reload };
}

export function CourseView({ id }: { id: number }) {
  const { course, err, reload } = useCourse(id);
  const [tab, setTab] = useState<'toc' | 'intro'>('toc');
  const [q, setQ] = useState('');

  if (err) return <div className="page enter"><BackLink /><Empty glyph="阙" title={err} /></div>;
  return (
    <div className="page enter">
      <BackLink library={!!course && course.visibility === 'public' && !course.enrolled} />
      <div className="course">
        <aside className="colophon">{course ? <Colophon c={course} onChange={reload} /> : <Skeleton className="aspect-[3/4] w-full" />}</aside>
        <section>
          <div className="toc-head">
            <div className="toc-tabs" role="tablist">
              <button role="tab" aria-selected={tab === 'toc'} className={cn('toc-tab', tab === 'toc' && 'active')} onClick={() => setTab('toc')}>
                目录{course ? <sup>{course.lessonCount}</sup> : null}
              </button>
              <button role="tab" aria-selected={tab === 'intro'} className={cn('toc-tab', tab === 'intro' && 'active')} onClick={() => setTab('intro')}>课程介绍</button>
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
  if (!c.enrolled) return <Button variant="default" size="block" onClick={() => void startLearning(c)}><BookOpen />开始学习</Button>;
  const first = c.chapters[0]?.lessons[0];
  const target = c.progress.lastLessonId ?? first?.id;
  if (!target) return null;
  const started = c.progress.lastLessonId != null;
  return (
    <Button variant="default" size="block" onClick={() => navigate(lessonURL(c.id, target))}>
      <BookOpen />{started ? '继续学习' : '开始学习'}
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

function Toc({ c, q }: { c: CourseDetail; q: string }) {
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
          <div className="chapter-head">
            <span className="c-no">第 {pad2(chapterNo.get(ch.id)!)} 章</span>
            <span className="c-title"><Highlight text={ch.title} q={q} /></span>
            <span className={cn('c-meta', ch.done === ch.lessons.length && 'complete')}>{ch.done} / {ch.lessons.length}</span>
          </div>
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

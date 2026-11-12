import { useEffect, useState, type MouseEvent } from 'react';
import { ArrowLeft } from 'lucide-react';
import { Empty } from '@/components/common';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { Skeleton } from '@/components/skeleton';
import { ImageZoom } from '@/components/zoom';
import { api } from '@/lib/api';
import type { LessonText } from '@/lib/courses';
import { Link } from '@/lib/router';
import { useCourse } from '@/views/course';

// a text that opens with its own "# title" shows that instead of ours
const opensWithTitle = (md: string) => /^\s*#\s/.test(md);

export function LessonView({ courseId, lessonId }: { courseId: number; lessonId: number }) {
  const { course } = useCourse(courseId);
  const [lesson, setLesson] = useState<LessonText | null>(null);
  const [err, setErr] = useState('');
  const [zoom, setZoom] = useState<string | null>(null);

  useEffect(() => {
    setLesson(null);
    setErr('');
    api<LessonText>('GET', `/api/courses/${courseId}/lessons/${lessonId}`).then((l) => {
      setLesson(l);
      document.title = `${l.title} · Keine`;
    }).catch((e) => setErr(e.message));
    return () => { document.title = 'Keine'; };
  }, [courseId, lessonId]);

  const onClick = (e: MouseEvent) => {
    const t = e.target as HTMLElement;
    if (t.tagName === 'IMG' && t.closest('.prose')) setZoom((t as HTMLImageElement).src);
  };

  return (
    <div className="page enter reader" onClick={onClick}>
      <Link className="back" href={`/courses/${courseId}`}><ArrowLeft />{course ? course.title : '返回目录'}</Link>
      {err ? <Empty glyph="阙" title={err} /> : null}
      {!err && !lesson ? <ReaderSkeleton /> : null}
      {lesson ? (
        <article className="reader-doc">
          {course ? <div className="kicker">{course.title}</div> : null}
          {opensWithTitle(lesson.markdown) ? null : <h1>{lesson.title}</h1>}
          {lesson.markdown ? (
            <LazyMarkdown text={lesson.markdown} base={lesson.base} className="reader-prose" />
          ) : <p className="text-ink-3">这一讲没有文字内容。</p>}
        </article>
      ) : null}
      <ImageZoom src={zoom} onClose={() => setZoom(null)} />
    </div>
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

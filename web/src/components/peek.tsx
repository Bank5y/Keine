import { useEffect, useState, type ReactNode } from 'react';
import { BookOpen, PlayCircle } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { LazyMarkdown } from '@/components/lazy-markdown';
import { Skeleton } from '@/components/skeleton';
import { api, ApiError } from '@/lib/api';
import { COMPLETION_LABEL, TYPE_LABEL, lessonURL, pad2, type CourseDetail, type CourseSummary } from '@/lib/courses';
import { navigate } from '@/lib/router';
import { cn } from '@/lib/utils';

// detail is shared by every peek of a course while the page lives
const cache = new Map<number, Promise<CourseDetail>>();
const loadDetail = (id: number) => {
  if (!cache.has(id)) cache.set(id, api<CourseDetail>('GET', `/api/courses/${id}`).catch((e) => { cache.delete(id); throw e; }));
  return cache.get(id)!;
};

// startLearning adds a library course to my courses and opens it.
export async function startLearning(c: Pick<CourseSummary, 'id' | 'enrolled'>) {
  try {
    if (!c.enrolled) {
      await api('POST', `/api/courses/${c.id}/enroll`);
      cache.delete(c.id);
      toast.success('已加入我的课程');
    }
    navigate(`/courses/${c.id}`);
  } catch (ex) {
    toast.error(ex instanceof ApiError ? ex.message : '操作失败');
  }
}

// Peek previews a library course: its introduction and its table of
// contents, with a way to start (or continue) learning it.
export function Peek({ course }: { course: CourseSummary }) {
  const [tab, setTab] = useState<'intro' | 'toc'>('intro');
  const [d, setD] = useState<CourseDetail | null>(null);
  const [err, setErr] = useState('');
  useEffect(() => { loadDetail(course.id).then(setD).catch((e) => setErr(e.message)); }, [course.id]);

  let body: ReactNode;
  if (err) body = <div className="peek-note err">{err}</div>;
  else if (!d) body = <div className="space-y-3 px-[18px] py-4">{Array.from({ length: 5 }).map((_, i) => <Skeleton key={i} className="h-3" />)}</div>;
  else if (tab === 'intro') {
    body = (
      <div className="peek-body">
        {d.intro ? <LazyMarkdown text={d.intro} className="peek-prose" /> : <p className="text-ink-3">暂无课程介绍</p>}
        {d.authorName ? <div className="peek-author"><b>{d.authorName}</b>{d.authorIntro ? <span>{d.authorIntro}</span> : null}</div> : null}
      </div>
    );
  } else {
    body = (
      <div className="peek-list">
        {d.chapters.map((ch, i) => (
          <div key={ch.id}>
            <div className="peek-ch"><span className="no">{pad2(i + 1)}</span><span className="t">{ch.title}</span><span className="n">{ch.lessons.length}</span></div>
            {ch.lessons.map((l) => (
              <a key={l.id} className="peek-a" href={lessonURL(d.id, l.id)} onClick={(e) => { e.preventDefault(); navigate(lessonURL(d.id, l.id)); }}>
                {l.video ? <PlayCircle className="vid" /> : null}<span className="t">{l.title}</span>
              </a>
            ))}
          </div>
        ))}
      </div>
    );
  }

  return (
    <div className="peek">
      <div className="peek-kicker">{TYPE_LABEL[course.type]} · {COMPLETION_LABEL[course.completion]} · {course.lessonCount} 讲</div>
      <div className="peek-title">{course.title}</div>
      <div className="peek-tabs" role="tablist">
        <button role="tab" aria-selected={tab === 'intro'} className={cn(tab === 'intro' && 'active')} onClick={() => setTab('intro')}>课程介绍</button>
        <button role="tab" aria-selected={tab === 'toc'} className={cn(tab === 'toc' && 'active')} onClick={() => setTab('toc')}>课程目录</button>
      </div>
      {body}
      <div className="peek-foot">
        <Button variant="default" size="sm" className="w-full" onClick={() => {
          if (course.enrolled && d?.progress.lastLessonId) navigate(lessonURL(course.id, d.progress.lastLessonId));
          else void startLearning(course);
        }}>
          <BookOpen />{course.enrolled ? '继续学习' : '开始学习'}
        </Button>
      </div>
    </div>
  );
}

import { useState, type ReactNode } from 'react';
import { Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { ProgressBar } from '@/components/progress';
import { COMPLETION_LABEL, PUBLISH_LABEL, TYPE_LABEL, coverURL, type CourseSummary } from '@/lib/courses';
import { Link } from '@/lib/router';
import { cn } from '@/lib/utils';

// deterministic muted "book cloth" color for courses without a cover image
const CLOTH = ['#7a3b2e', '#2f5446', '#34466b', '#6b5530', '#5a3f5e', '#2d5c6b', '#5c4a3a', '#3f5a3a'];
function clothColor(s: string) {
  let h = 0;
  for (const ch of s) h = (h * 31 + ch.codePointAt(0)!) >>> 0;
  return CLOTH[h % CLOTH.length];
}

export function Plate({ title }: { title: string }) {
  return (
    <div className="plate" style={{ background: clothColor(title) }}>
      <span className="p-letter">{(title || '?').slice(0, 1)}</span>
      <span className="p-title">{title}</span>
    </div>
  );
}

// Cover shows the course's own image, falling back to a cloth plate.
export function Cover({ course }: { course: Pick<CourseSummary, 'id' | 'title' | 'hasCover'> }) {
  const [broken, setBroken] = useState(false);
  if (course.hasCover && !broken) return <img src={coverURL(course.id)} alt="" loading="lazy" onError={() => setBroken(true)} />;
  return <Plate title={course.title} />;
}

// href is where the card leads; without it the card does not navigate.
export function CourseCard({ course, href, onDelete, showProgress }: { course: CourseSummary; href?: string; onDelete?: (c: CourseSummary) => void; showProgress?: boolean }) {
  const isPrivate = course.visibility === 'private';
  const Go = ({ className, children, tabIndex }: { className: string; children: ReactNode; tabIndex?: number }) =>
    href ? <Link href={href} className={className} tabIndex={tabIndex}>{children}</Link> : <div className={className}>{children}</div>;
  return (
    <div className="book">
      <div className="book-cover-wrap">
        <Go className="block" tabIndex={-1}>
          <div className="book-cover">
            <Cover course={course} />
            {isPrivate ? <span className={cn('book-badge', course.publishState)}>{PUBLISH_LABEL[course.publishState]}</span> : null}
            {!isPrivate && course.enrolled && !showProgress ? <span className="book-badge enrolled">学习中</span> : null}
            {course.completion !== 'finished' ? <span className={cn('book-flag', course.completion)}>{COMPLETION_LABEL[course.completion]}</span> : null}
          </div>
        </Go>
        {onDelete && isPrivate && course.isUploader ? (
          <div className="book-tools">
            <Button variant="icon" size="icon" title="删除课程" aria-label={`删除 ${course.title}`} onClick={() => onDelete(course)}><Trash2 /></Button>
          </div>
        ) : null}
      </div>
      <Go className="contents">
        <div className="book-meta">
          <div className="book-kicker">
            <span>{TYPE_LABEL[course.type]}</span>
            <span className="dot" />
            <span className="k-muted">{course.lessonCount} 讲</span>
          </div>
          <h3 className="book-title" title={course.title}>{course.title}</h3>
          {course.authorName ? <div className="book-author">{course.authorName}</div> : null}
          {showProgress ? (
            <div className="book-progress">
              <ProgressBar done={course.doneCount} total={course.lessonCount} />
              <span>{course.doneCount ? `已学 ${course.doneCount} / ${course.lessonCount}` : '尚未开始'}</span>
            </div>
          ) : null}
        </div>
      </Go>
    </div>
  );
}

// hrefOf says where each card leads; course pages arrive with the course detail ticket.
export function Shelf({ courses, hrefOf, onDelete, showProgress }: { courses: CourseSummary[]; hrefOf?: (c: CourseSummary) => string; onDelete?: (c: CourseSummary) => void; showProgress?: boolean }) {
  return <div className="shelf">{courses.map((c) => <CourseCard key={c.id} course={c} href={hrefOf?.(c)} onDelete={onDelete} showProgress={showProgress} />)}</div>;
}

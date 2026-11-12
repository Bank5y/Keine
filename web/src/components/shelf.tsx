import { useState } from 'react';
import { Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
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

export function CourseCard({ course, onDelete }: { course: CourseSummary; onDelete?: (c: CourseSummary) => void }) {
  const isPrivate = course.visibility === 'private';
  return (
    <div className="book">
      <Link href={`/courses/${course.id}`} className="contents" aria-label={course.title}>
        <div className="book-cover">
          <Cover course={course} />
          {isPrivate ? <span className={cn('book-badge', course.publishState)}>{PUBLISH_LABEL[course.publishState]}</span> : null}
          {course.completion !== 'finished' ? <span className={cn('book-flag', course.completion)}>{COMPLETION_LABEL[course.completion]}</span> : null}
        </div>
        <div className="book-meta">
          <div className="book-kicker">
            <span>{TYPE_LABEL[course.type]}</span>
            <span className="dot" />
            <span className="k-muted">{course.lessonCount} 讲</span>
          </div>
          <h3 className="book-title" title={course.title}>{course.title}</h3>
          {course.authorName ? <div className="book-author">{course.authorName}</div> : null}
        </div>
      </Link>
      {onDelete && isPrivate && course.isUploader ? (
        <div className="book-tools">
          <Button variant="icon" size="icon" title="删除课程" aria-label={`删除 ${course.title}`} onClick={() => onDelete(course)}><Trash2 /></Button>
        </div>
      ) : null}
    </div>
  );
}

export function Shelf({ courses, onDelete }: { courses: CourseSummary[]; onDelete?: (c: CourseSummary) => void }) {
  return <div className="shelf">{courses.map((c) => <CourseCard key={c.id} course={c} onDelete={onDelete} />)}</div>;
}

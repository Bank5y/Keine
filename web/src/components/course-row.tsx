import type { ReactNode } from 'react';
import { Hit } from '@/components/page-search';
import { Cover } from '@/components/shelf';
import { COMPLETION_LABEL, TYPE_LABEL, type CourseSummary } from '@/lib/courses';
import { Link } from '@/lib/router';
import { cn } from '@/lib/utils';

// CourseRow is a course in a narrow list: a small cover, then the title,
// the facts without column heads (类型 · N 讲 · 完结状态) and the author.
// The whole row leads to href. children add lines below the author (a
// 已订阅 tag, a progress bar); action sits at the row's end, outside the
// link (a ⋯ menu). mark: words searched for, marked in the title and the
// author.
export function CourseRow({ course, href, children, action, mark = '' }: { course: CourseSummary; href: string; children?: ReactNode; action?: ReactNode; mark?: string }) {
  return (
    <div className="course-row">
      <Link href={href} className="cr-link">
        <div className="cr-cover"><Cover course={course} /></div>
        <div className="cr-body">
          <h3 className="cr-title"><Hit text={course.title} q={mark} /></h3>
          <div className="cr-facts">
            <span className="cr-type">{TYPE_LABEL[course.type]}</span>
            <span className="dot" />
            <span>{course.lessonCount} 讲</span>
            <span className="dot" />
            <span className={cn('cr-completion', course.completion)}>{COMPLETION_LABEL[course.completion]}</span>
          </div>
          {course.authorName ? <div className="cr-author"><Hit text={course.authorName} q={mark} /></div> : null}
          {children}
        </div>
      </Link>
      {action ? <div className="cr-action">{action}</div> : null}
    </div>
  );
}

// CourseList stacks rows between hairlines, under a heavier rule.
export function CourseList({ children }: { children: ReactNode }) {
  return <div className="course-list">{children}</div>;
}

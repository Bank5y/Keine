import type { Conflict } from '@/lib/drafts';
export type CourseType = 'column' | 'video' | 'paper';
export type Completion = 'finished' | 'ongoing' | 'incomplete';
export type PublishState = 'unpublished' | 'pending' | 'rejected';

export interface CourseSummary {
  id: number;
  title: string;
  type: CourseType;
  completion: Completion;
  authorName: string;
  hasCover: boolean;
  visibility: 'private' | 'public';
  publishState: PublishState;
  uploaderId: number;
  isUploader: boolean;
  lessonCount: number;
  doneCount: number;
  // subscribed: the library course is in the viewer's my courses
  subscribed: boolean;
  // retention: the course left the library; the viewer, a subscriber, reads
  // only their retained lessons (retainedCount of them)
  retention?: Retention;
  retainedCount?: number;
  createdAt: string;
  updatedAt: string;
}

export type Retention = 'unpublished' | 'deleted';
export const RETENTION_LABEL: Record<Retention, string> = { unpublished: '已下架', deleted: '已删除' };

export const TYPE_LABEL: Record<CourseType, string> = { column: '专栏', video: '视频课', paper: '论文' };
export const COMPLETION_LABEL: Record<Completion, string> = { finished: '已完结', ongoing: '连载中', incomplete: '缺失' };
export const PUBLISH_LABEL: Record<PublishState, string> = { unpublished: '未公开', pending: '审核中', rejected: '已驳回' };

export const coverURL = (id: number) => `/api/courses/${id}/cover`;

export type LessonState = 'unstarted' | 'done';
export interface LessonItem { id: number; key: string; title: string; video: '' | 'file' | 'url'; duration: string; hasText: boolean; state: LessonState; upcoming?: boolean; unavailable?: boolean }
// readable tells whether a lesson can be opened: not upcoming, not gone.
export const readable = (l: LessonItem) => !l.upcoming && !l.unavailable;
// total counts the chapter's lessons in progress: all but the upcoming ones.
export interface ChapterItem { id: number; key: string; title: string; done: number; total: number; lessons: LessonItem[] }
export interface CourseDetail extends CourseSummary {
  intro: string;
  authorIntro: string;
  chapters: ChapterItem[];
  progress: { done: number; total: number; lastLessonId: number | null };
  rejectReason?: string;
  contributors: string[];
}

export interface UpdateRequest { id: number; state: 'pending' | 'approved' | 'rejected' | 'withdrawn'; reason: string; createdAt: string; decidedAt: string | null }

export interface Review {
  id: number;
  kind: 'publish' | 'update';
  course: CourseSummary;
  requester: { id: number; username: string };
  createdAt: string;
  // changes: missing for an update request whose files could not be read
  changes?: Changes;
  stale: boolean;
  // conflicts: the steps of a request made in the editor that no longer
  // apply to the course's latest version; it cannot be approved then.
  conflicts?: Conflict[];
}

// What a course update changes (internal/course/changes.go). Counts are of
// lessons with content; chapters lists every change, chapter by chapter in
// the new order, then the removed chapters.
export type ChangeKind = 'added' | 'removed' | 'modified' | 'renamed' | 'moved';
export interface LessonChange { key: string; title: string; previousTitle?: string; fromChapter?: string; upcoming?: boolean; changes: ChangeKind[] }
export interface ChapterChange { key: string; title: string; previousTitle?: string; changes: ChangeKind[]; lessons: LessonChange[] }
export interface Changes {
  added: number; removed: number; modified: number; renamed: number; moved: number; kept: number;
  chapters: ChapterChange[];
}
export const CHANGE_LABEL: Record<ChangeKind, string> = { added: '新增', removed: '删除', modified: '修改', renamed: '改名', moved: '移动' };

export const pad2 = (n: number) => String(n).padStart(2, '0');
export const lessonURL = (courseId: number, lessonId: number) => `/courses/${courseId}/lessons/${lessonId}`;

export interface LessonVideo { mode: 'player' | 'embed' | 'link'; src: string }
export interface LessonText { id: number; key: string; title: string; chapterId: number; markdown: string; base: string; state: LessonState; position: number; video: LessonVideo | null }

export const STATE_LABEL: Record<LessonState, string> = { unstarted: '未学习', done: '已学完' };
export const percent = (done: number, total: number) => (total ? Math.round((done / total) * 100) : 0);

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
  createdAt: string;
  updatedAt: string;
}

export const TYPE_LABEL: Record<CourseType, string> = { column: '专栏', video: '视频课', paper: '论文' };
export const COMPLETION_LABEL: Record<Completion, string> = { finished: '已完结', ongoing: '连载中', incomplete: '缺失' };
export const PUBLISH_LABEL: Record<PublishState, string> = { unpublished: '未公开', pending: '审核中', rejected: '已驳回' };

export const coverURL = (id: number) => `/api/courses/${id}/cover`;

export interface LessonItem { id: number; key: string; title: string; video: '' | 'file' | 'url'; duration: string; hasText: boolean }
export interface ChapterItem { id: number; key: string; title: string; lessons: LessonItem[] }
export interface CourseDetail extends CourseSummary {
  intro: string;
  authorIntro: string;
  chapters: ChapterItem[];
}

export const pad2 = (n: number) => String(n).padStart(2, '0');
export const lessonURL = (courseId: number, lessonId: number) => `/courses/${courseId}/lessons/${lessonId}`;

export interface LessonText { id: number; key: string; title: string; chapterId: number; markdown: string; base: string }

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

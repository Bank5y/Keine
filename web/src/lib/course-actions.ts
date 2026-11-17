import { readable, type CourseDetail } from '@/lib/courses';

// ReaderBar is what the narrow course page's bottom bar offers: subscribing
// to a library course the reader reads without subscribing, and opening a
// lesson (null when none can be opened).
export interface ReaderBar {
  subscribe: boolean;
  read: { lessonId: number; label: '开始阅读' | '继续学习' } | null;
}

export function readerBar(c: CourseDetail): ReaderBar {
  const subscribe = c.visibility === 'public' && !c.subscribed && !c.retention;
  const lessons = c.chapters.flatMap((ch) => ch.lessons).filter(readable);
  const last = lessons.find((l) => l.id === c.progress.lastLessonId);
  const target = last?.id ?? lessons[0]?.id;
  // a course that is the reader's own (subscribed, private, retained) is
  // always 继续学习; 开始阅读 sits beside 订阅课程 only, for a first look
  const label = subscribe && !last ? '开始阅读' : '继续学习';
  return { subscribe, read: target == null ? null : { lessonId: target, label } };
}

// CourseAction is one thing the viewer may do with a course, beyond reading
// it. 'edit' opens the course editor; 'review' decides a publish request.
export type CourseAction =
  | 'publish-request' | 'edit' | 'withdraw-publish' | 'review' | 'update-request'
  | 'withdraw-update' | 'direct-publish' | 'edit-info' | 'unpublish' | 'delete';

// What else the actions depend on, beyond the course: whether the viewer is
// an admin, whether there is a publish request for them to decide, and
// whether the viewer has an update request waiting.
export interface ActionContext { admin: boolean; reviewing: boolean; updatePending: boolean }

// courseActions lists what the viewer may do with a course in its current
// state, in the order the desktop colophon shows them: those who may change
// it open the course editor; the uploader asks for it to be published (or
// withdraws, or asks again after a rejection); an admin reviews, publishes,
// unpublishes, edits and deletes. A course the viewer only keeps retained
// lessons of offers nothing.
export function courseActions(c: CourseDetail, { admin, reviewing, updatePending }: ActionContext): CourseAction[] {
  if (c.retention) return [];
  const isPrivate = c.visibility === 'private';
  const own = isPrivate && c.isUploader;
  const canRequest = !isPrivate && !admin && (c.isUploader || c.completion !== 'finished');
  // the course editor: directly for a private course's uploader and for an
  // admin on a public course; as an update request for the others who may
  // ask for one
  const canEdit = own || (admin && !isPrivate) || canRequest;
  const out: CourseAction[] = [];
  if (own && c.publishState !== 'pending' && !admin) out.push('publish-request');
  if (canEdit) out.push('edit');
  if (own && c.publishState === 'pending') out.push('withdraw-publish');
  if (reviewing) out.push('review');
  if (canRequest && !updatePending) out.push('update-request');
  if (updatePending) out.push('withdraw-update');
  if (admin && isPrivate && !reviewing) out.push('direct-publish');
  if (admin && !isPrivate) out.push('edit-info', 'unpublish');
  if (admin) out.push('delete');
  return out;
}

// menuActions are the actions of the narrow course page's ⋯ menu: all but
// the course editor, which has its own gear.
export const menuActions = (actions: CourseAction[]) => actions.filter((a) => a !== 'edit');

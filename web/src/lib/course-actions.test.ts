import { describe, expect, it } from 'vitest';
import { courseActions, menuActions, readerBar } from '@/lib/course-actions';
import type { CourseDetail, LessonItem } from '@/lib/courses';

const lesson = (id: number, more: Partial<LessonItem> = {}): LessonItem => ({ id, key: String(id), title: `第 ${id} 讲`, video: '', duration: '', hasText: true, state: 'unstarted', ...more });

const course = (more: Partial<CourseDetail> = {}): CourseDetail => ({
  id: 1, title: '课程', type: 'column', completion: 'ongoing', authorName: '', hasCover: false,
  visibility: 'public', publishState: 'unpublished', uploaderId: 9, isUploader: false,
  lessonCount: 3, doneCount: 0, subscribed: false, createdAt: '', updatedAt: '',
  intro: '', authorIntro: '', contributors: [],
  chapters: [{ id: 10, key: 'a', title: '开篇', done: 0, total: 3, lessons: [lesson(1, { upcoming: true }), lesson(2), lesson(3)] }],
  progress: { done: 0, total: 3, lastLessonId: null },
  ...more,
});

describe('readerBar', () => {
  it('offers a library course the reader has not subscribed to, and to start at its first readable lesson', () => {
    expect(readerBar(course())).toEqual({ subscribe: true, read: { lessonId: 2, label: '开始阅读' } });
  });

  it('continues where a reader without a subscription left off', () => {
    expect(readerBar(course({ progress: { done: 1, total: 3, lastLessonId: 3 } }))).toEqual({ subscribe: true, read: { lessonId: 3, label: '继续学习' } });
  });

  it('gives a subscribed course, a private course and a course with only retained lessons just 继续学习', () => {
    const only = { subscribe: false, read: { lessonId: 2, label: '继续学习' } };
    expect(readerBar(course({ subscribed: true }))).toEqual(only);
    expect(readerBar(course({ visibility: 'private', isUploader: true }))).toEqual(only);
    expect(readerBar(course({ visibility: 'private', subscribed: true, retention: 'unpublished' }))).toEqual(only);
  });

  it('offers no reading when no lesson can be opened', () => {
    const gone = course({ subscribed: true, chapters: [{ id: 10, key: 'a', title: '开篇', done: 0, total: 1, lessons: [lesson(1, { unavailable: true })] }] });
    expect(readerBar(gone)).toEqual({ subscribe: false, read: null });
  });
});

describe('courseActions', () => {
  const reader = { admin: false, reviewing: false, updatePending: false };
  const admin = { ...reader, admin: true };

  it('lets an admin edit a library course, change its information, unpublish and delete it', () => {
    expect(courseActions(course(), admin)).toEqual(['edit', 'edit-info', 'unpublish', 'delete']);
  });

  it('lets an admin decide a course under review, or publish a private course directly', () => {
    expect(courseActions(course({ visibility: 'private', publishState: 'pending' }), { ...admin, reviewing: true })).toEqual(['review', 'delete']);
    expect(courseActions(course({ visibility: 'private' }), admin)).toEqual(['direct-publish', 'delete']);
  });

  it('lets the uploader of a private course ask for it to be published, or withdraw the request', () => {
    const own = { visibility: 'private' as const, isUploader: true };
    expect(courseActions(course(own), reader)).toEqual(['publish-request', 'edit']);
    expect(courseActions(course({ ...own, publishState: 'rejected' }), reader)).toEqual(['publish-request', 'edit']);
    expect(courseActions(course({ ...own, publishState: 'pending' }), reader)).toEqual(['edit', 'withdraw-publish']);
  });

  it('lets the uploader, or anyone on an unfinished course, ask to update a library course', () => {
    expect(courseActions(course({ isUploader: true, completion: 'finished' }), reader)).toEqual(['edit', 'update-request']);
    expect(courseActions(course({ completion: 'incomplete' }), reader)).toEqual(['edit', 'update-request']);
    expect(courseActions(course(), { ...reader, updatePending: true })).toEqual(['edit', 'withdraw-update']);
  });

  it('gives a reader of a finished library course, or of retained lessons, nothing to do', () => {
    expect(courseActions(course({ completion: 'finished' }), reader)).toEqual([]);
    expect(courseActions(course({ visibility: 'private', subscribed: true, retention: 'deleted' }), reader)).toEqual([]);
  });
});

describe('menuActions', () => {
  it('leaves the course editor to the gear', () => {
    expect(menuActions(['publish-request', 'edit', 'withdraw-update'])).toEqual(['publish-request', 'withdraw-update']);
    expect(menuActions(['edit'])).toEqual([]);
  });
});

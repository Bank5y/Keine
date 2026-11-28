import { describe, expect, it } from 'vitest';
import { publishTargets, type CourseSummary } from '@/lib/courses';

const course = (id: number, more: Partial<CourseSummary> = {}): CourseSummary => ({
  id, title: `课程 ${id}`, type: 'column', completion: 'finished', authorName: '', hasCover: false,
  visibility: 'private', publishState: 'unpublished', uploaderId: 9, isUploader: true,
  lessonCount: 3, doneCount: 0, subscribed: false, createdAt: '', updatedAt: '', ...more,
});

describe('publishTargets', () => {
  const mine = [
    course(1),
    course(2, { publishState: 'pending' }),
    course(3, { publishState: 'rejected' }),
    course(4, { visibility: 'public', isUploader: true }),
    course(5, { visibility: 'public', isUploader: false, subscribed: true }),
    course(6, { visibility: 'private', isUploader: false, retention: 'unpublished' }),
  ];
  const ids = (cs: CourseSummary[]) => cs.map((c) => c.id);

  it('offers a reader their own private courses not already under review, to ask for publishing', () => {
    expect(ids(publishTargets(mine, false))).toEqual([1, 3]);
  });

  it('offers an admin every private course of their own, under review or not, to publish directly', () => {
    expect(ids(publishTargets(mine, true))).toEqual([1, 2, 3]);
  });
});

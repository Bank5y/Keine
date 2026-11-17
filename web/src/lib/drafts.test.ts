import { describe, expect, it } from 'vitest';
import { describeOp, lessonDown, lessonDrop, lessonTo, lessonUp, type DraftChapter } from '@/lib/drafts';

const lesson = (key: string) => ({ key, title: key, video: '' as const, hasText: true });
const chapters: DraftChapter[] = [
  { key: 'opening', title: '开篇', lessons: [lesson('l00')] },
  { key: 'intent', title: '意图识别', lessons: [lesson('l01'), lesson('l02'), lesson('l03')] },
  { key: 'plan', title: '规划与执行', lessons: [] },
];

describe('describeOp', () => {
  it('says a lesson moved to another chapter, naming that chapter', () => {
    expect(describeOp({ type: 'moveLesson', lesson: 'l00', chapter: 'intent', after: '', target: '开篇词', from: 'opening' }, chapters))
      .toBe('把讲「开篇词」移到章「意图识别」');
  });

  it('says a lesson moved within its chapter was reordered', () => {
    expect(describeOp({ type: 'moveLesson', lesson: 'l02', chapter: 'intent', after: '', target: '02｜三层意图识别', from: 'intent' }, chapters))
      .toBe('调整讲「02｜三层意图识别」的顺序');
  });

  it('leaves out where a lesson went when that chapter is gone', () => {
    expect(describeOp({ type: 'moveLesson', lesson: 'l00', chapter: 'deleted', after: '', target: '开篇词', from: 'opening' }, chapters))
      .toBe('移动讲「开篇词」');
  });

  it('says a moved chapter was reordered', () => {
    expect(describeOp({ type: 'moveChapter', chapter: 'plan', after: '', target: '规划与执行' })).toBe('调整章「规划与执行」的顺序');
  });

  it('lists which course information changed', () => {
    expect(describeOp({ type: 'setInfo', info: { intro: '新介绍', title: '新课程名' } })).toBe('修改课程信息：课程名、介绍');
  });
});

describe('lesson moves', () => {
  it('lessonTo puts a lesson at the end of the chapter', () => {
    expect(lessonTo(chapters, 'l00', 'intent')).toEqual({ type: 'moveLesson', lesson: 'l00', chapter: 'intent', after: 'l03' });
    expect(lessonTo(chapters, 'l00', 'plan')).toEqual({ type: 'moveLesson', lesson: 'l00', chapter: 'plan', after: '' });
  });

  it('lessonDrop at the end of a chapter is the same move as lessonTo', () => {
    expect(lessonDrop(chapters, 'l00', 'intent', 3)).toEqual(lessonTo(chapters, 'l00', 'intent'));
  });

  it('lessonDrop where the lesson already is changes nothing', () => {
    expect(lessonDrop(chapters, 'l02', 'intent', 1)).toBeNull();
    expect(lessonDrop(chapters, 'l02', 'intent', 2)).toBeNull();
  });

  it('moving past a chapter edge crosses into the neighbouring chapter', () => {
    expect(lessonUp(chapters, 1, 0)).toEqual({ type: 'moveLesson', lesson: 'l01', chapter: 'opening', after: 'l00' });
    expect(lessonDown(chapters, 1, 2)).toEqual({ type: 'moveLesson', lesson: 'l03', chapter: 'plan', after: '' });
    expect(lessonUp(chapters, 0, 0)).toBeNull();
  });
});

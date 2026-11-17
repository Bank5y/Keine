import type { Completion, CourseType } from '@/lib/courses';
import { COMPLETIONS, COURSE_TYPES, filterParams, oneOf } from '@/lib/course-filter';

// My courses keeps its filter in the address (?state=&type=&completion=),
// the way the library does, so a reload or the back button keeps it.
// '' is 全部: no filter on that dimension, and no parameter.
export type StudyState = 'unfinished' | 'done';
export interface MineFilter { state: StudyState | ''; type: CourseType | ''; completion: Completion | '' }

// The choices 学习 offers, 全部 aside (type and completion: course-filter.ts).
export const MINE_STATES: StudyState[] = ['unfinished', 'done'];
export const STUDY_LABEL: Record<StudyState, string> = { unfinished: '未学完', done: '已学完' };

// An unknown value reads as 全部 rather than an empty list.
export function readMineFilter(search: string): MineFilter {
  const p = new URLSearchParams(search);
  return { state: oneOf(p.get('state'), MINE_STATES), type: oneOf(p.get('type'), COURSE_TYPES), completion: oneOf(p.get('completion'), COMPLETIONS) };
}

// mineParams are the parameters of a filter for the my-courses API; the
// address uses the same names.
export const mineParams = (f: MineFilter) => filterParams({ state: f.state, type: f.type, completion: f.completion });

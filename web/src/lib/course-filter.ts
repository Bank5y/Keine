import { COMPLETION_LABEL, TYPE_LABEL, type Completion, type CourseType } from '@/lib/courses';

// The vocabulary a course list is filtered by, shared by the library and
// my courses: the course types and completion states a reader may pick, and
// how a filter goes into and out of an address. '' is 全部: no filter on
// that dimension, and no parameter.

// The choices each dimension offers, 全部 aside.
export const COURSE_TYPES: CourseType[] = ['column', 'video'];
export const COMPLETIONS: Completion[] = ['finished', 'ongoing', 'incomplete'];

export interface Option<T extends string> { value: T; label: string }
export const COURSE_TYPE_OPTIONS: Option<CourseType>[] = COURSE_TYPES.map((v) => ({ value: v, label: TYPE_LABEL[v] }));
export const COMPLETION_OPTIONS: Option<Completion>[] = COMPLETIONS.map((v) => ({ value: v, label: COMPLETION_LABEL[v] }));

// oneOf reads a parameter as one of the allowed values; an unknown value
// reads as 全部 rather than an empty list.
export const oneOf = <T extends string>(v: string | null, allowed: T[]): T | '' => (allowed as string[]).includes(v ?? '') ? v as T : '';

// filterParams are the parameters of a filter, in the order given, for the
// address and the API alike: 全部 and blank values leave theirs out.
export function filterParams(f: Record<string, string | undefined>): URLSearchParams {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(f)) {
    const s = v?.trim();
    if (s) p.set(k, s);
  }
  return p;
}

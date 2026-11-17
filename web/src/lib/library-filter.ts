import { COMPLETIONS, COURSE_TYPES, filterParams, oneOf } from '@/lib/course-filter';
import type { Completion, CourseType } from '@/lib/courses';

// The library's filter lives in its address (?type=&completion=&q=), so a
// shared link or a search typed in the top bar opens the same results.
// '' is 全部: no filter on that dimension, and no parameter in the address.
export interface LibraryFilter { type: CourseType | ''; completion: Completion | ''; q: string }

// An unknown value reads as 全部 rather than an empty library.
export function readLibraryFilter(search: string): LibraryFilter {
  const p = new URLSearchParams(search);
  return { type: oneOf(p.get('type'), COURSE_TYPES), completion: oneOf(p.get('completion'), COMPLETIONS), q: p.get('q') ?? '' };
}

// libraryParams are the parameters of a filter, for the address and the
// library API alike: 全部 and blank keywords leave theirs out.
export const libraryParams = (f: Partial<LibraryFilter>) => filterParams({ type: f.type, completion: f.completion, q: f.q });

export function libraryHref(f: Partial<LibraryFilter>): string {
  const s = libraryParams(f).toString();
  return s ? `/library?${s}` : '/library';
}

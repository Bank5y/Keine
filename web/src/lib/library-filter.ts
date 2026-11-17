import type { Completion, CourseType } from '@/lib/courses';

// The library's filter lives in its address (?type=&completion=&q=), so a
// shared link or a search typed in the top bar opens the same results.
// '' is 全部: no filter on that dimension, and no parameter in the address.
export interface LibraryFilter { type: CourseType | ''; completion: Completion | ''; q: string }

// The choices each dimension offers, 全部 aside.
export const LIBRARY_TYPES: CourseType[] = ['column', 'video'];
export const LIBRARY_COMPLETIONS: Completion[] = ['finished', 'ongoing', 'incomplete'];

export const oneOf = <T extends string>(v: string | null, allowed: T[]): T | '' => (allowed as string[]).includes(v ?? '') ? v as T : '';

// An unknown value reads as 全部 rather than an empty library.
export function readLibraryFilter(search: string): LibraryFilter {
  const p = new URLSearchParams(search);
  return { type: oneOf(p.get('type'), LIBRARY_TYPES), completion: oneOf(p.get('completion'), LIBRARY_COMPLETIONS), q: p.get('q') ?? '' };
}

// libraryParams are the parameters of a filter, for the address and the
// library API alike: 全部 and blank keywords leave theirs out.
export function libraryParams(f: Partial<LibraryFilter>): URLSearchParams {
  const p = new URLSearchParams();
  if (f.type) p.set('type', f.type);
  if (f.completion) p.set('completion', f.completion);
  const q = f.q?.trim();
  if (q) p.set('q', q);
  return p;
}

export function libraryHref(f: Partial<LibraryFilter>): string {
  const s = libraryParams(f).toString();
  return s ? `/library?${s}` : '/library';
}

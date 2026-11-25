import { pad2, type ChapterItem, type CourseSummary } from '@/lib/courses';

// A narrow screen's top bar searches the page it is on (#206): the
// library its courses (by the address's q, see library-filter.ts), a
// course page its contents, my courses the courses subscribed to. Any
// other page searches the library, going there. Notifications and the
// admin pages have no search: their bar is ← and the page's name. Only
// what the page already holds is filtered; nothing is asked of the server.

export type TopBar =
  | { kind: 'search'; scope: 'library' | 'course' | 'mine'; label: string; placeholder: string; inPlace: boolean }
  | { kind: 'back'; title: string; fallback: string };

const LIBRARY = { kind: 'search', scope: 'library', label: '课程库', placeholder: '搜索课程名或讲师' } as const;

// the pages whose bar is ← and their name, and where ← goes when nothing
// came before them
const SECTIONS = [
  { root: '/notifications', title: '通知', fallback: '/library' },
  { root: '/admin', title: '管理', fallback: '/account' },
];
const sectionOf = (path: string) => SECTIONS.find((s) => path === s.root || path.startsWith(s.root + '/')) ?? null;

export function topBarFor(path: string): TopBar {
  const section = sectionOf(path);
  if (section) return { kind: 'back', title: section.title, fallback: section.fallback };
  if (path === '/library') return { ...LIBRARY, inPlace: true };
  // a course page, not a lesson or its editor (the router reads the window, so not match())
  if (/^\/courses\/[^/]+$/.test(path)) return { kind: 'search', scope: 'course', label: '本课程', placeholder: '搜索讲名或章节', inPlace: true };
  if (path === '/mine') return { kind: 'search', scope: 'mine', label: '我的课程', placeholder: '搜索已订阅的课程', inPlace: true };
  return { ...LIBRARY, inPlace: false };
}

const norm = (s: string) => s.trim().toLowerCase();
const has = (text: string, k: string) => text.toLowerCase().includes(k);

// filterToc keeps the lessons of a course's contents that match q: by
// title, or by number through the course (as the contents number them);
// a chapter whose title matches keeps all its lessons. Chapters left
// without a lesson go. matches is how many lessons are kept.
export function filterToc(chapters: ChapterItem[], q: string): { chapters: ChapterItem[]; matches: number } {
  const k = norm(q);
  const count = (chs: ChapterItem[]) => chs.reduce((n, ch) => n + ch.lessons.length, 0);
  if (!k) return { chapters, matches: count(chapters) };
  let no = 0;
  const kept: ChapterItem[] = [];
  for (const ch of chapters) {
    const whole = has(ch.title, k);
    const lessons = ch.lessons.filter((l) => { no++; return whole || has(l.title, k) || pad2(no).includes(k); });
    if (lessons.length) kept.push({ ...ch, lessons });
  }
  return { chapters: kept, matches: count(kept) };
}

// filterMine keeps the courses whose name or author matches q.
export function filterMine<T extends Pick<CourseSummary, 'title' | 'authorName'>>(courses: T[], q: string): T[] {
  const k = norm(q);
  if (!k) return courses;
  return courses.filter((c) => has(c.title, k) || has(c.authorName ?? '', k));
}

// hitParts splits text where q is in it, to mark what matched.
export function hitParts(text: string, q: string): { text: string; hit: boolean }[] {
  const k = norm(q);
  if (!k) return [{ text, hit: false }];
  const out: { text: string; hit: boolean }[] = [];
  const low = text.toLowerCase();
  let at = 0;
  for (let i = low.indexOf(k); i >= 0; i = low.indexOf(k, at)) {
    if (i > at) out.push({ text: text.slice(at, i), hit: false });
    out.push({ text: text.slice(i, i + k.length), hit: true });
    at = i + k.length;
  }
  if (at < text.length) out.push({ text: text.slice(at), hit: false });
  return out;
}

// nextFrom is the page a reader came into notifications or the admin pages
// from, kept as they move from page `left` to page `to`: entering, the
// page left; moving within, as it was; elsewhere, none.
export function nextFrom(from: string | null, left: string, to: string): string | null {
  const section = sectionOf(to);
  if (!section) return null;
  return sectionOf(left) === section ? from : left;
}

// backTarget is where ← goes from path: the page come in from, else the
// page's default.
export function backTarget(path: string, from: string | null): string {
  const section = sectionOf(path);
  return from ?? section?.fallback ?? '/library';
}

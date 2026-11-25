import { describe, expect, it } from 'vitest';
import { backFrom, filterMine, filterToc, hitParts, nextEntered, noEntry, topBarFor } from '@/lib/page-search';
import type { ChapterItem, CourseSummary, LessonItem } from '@/lib/courses';

const lesson = (id: number, title: string): LessonItem => ({ id, key: `l${id}`, title, video: '', duration: '', hasText: true, state: 'unstarted' });
const chapter = (id: number, title: string, lessons: LessonItem[]): ChapterItem => ({ id, key: `c${id}`, title, done: 0, total: lessons.length, lessons });
const toc = [
  chapter(1, '开篇', [lesson(1, '为什么 Agent 值得认真对待')]),
  chapter(2, '意图识别', [lesson(2, '用户输入规范化'), lesson(3, '三层意图识别'), lesson(4, '槽位抽取与追问')]),
  chapter(3, '规划与执行', [lesson(5, '静态规划'), lesson(6, '动态规划'), lesson(7, '工具调用')]),
];
const titles = (r: ReturnType<typeof filterToc>) => r.chapters.map((ch) => [ch.title, ch.lessons.map((l) => l.title)]);

describe('the narrow top bar on each page', () => {
  it('searches the library in place on the library', () => {
    expect(topBarFor('/library')).toEqual({ kind: 'search', scope: 'library', label: '课程库', placeholder: '搜索课程名或讲师', inPlace: true });
  });
  it('searches the course’s contents on a course page', () => {
    expect(topBarFor('/courses/12')).toEqual({ kind: 'search', scope: 'course', label: '本课程', placeholder: '搜索讲名或章节', inPlace: true });
  });
  it('searches the courses subscribed to on my courses', () => {
    expect(topBarFor('/mine')).toEqual({ kind: 'search', scope: 'mine', label: '我的课程', placeholder: '搜索已订阅的课程', inPlace: true });
  });
  it('goes to the library to search from any other page', () => {
    for (const p of ['/account', '/notes']) {
      expect(topBarFor(p)).toEqual({ kind: 'search', scope: 'library', label: '课程库', placeholder: '搜索课程名或讲师', inPlace: false });
    }
  });
  it('has no search on notifications and the admin pages: ← and the page’s name', () => {
    expect(topBarFor('/notifications')).toEqual({ kind: 'back', title: '通知', fallback: '/library' });
    expect(topBarFor('/admin')).toEqual({ kind: 'back', title: '管理', fallback: '/account' });
    expect(topBarFor('/admin/users')).toEqual({ kind: 'back', title: '管理', fallback: '/account' });
  });
});

describe('filtering a course’s contents', () => {
  it('keeps everything, untouched, with nothing typed', () => {
    expect(filterToc(toc, '  ')).toEqual({ chapters: toc, matches: 7 });
  });
  it('keeps only the lessons whose title matches, in chapters that have one', () => {
    const r = filterToc(toc, '规划');
    expect(titles(r)).toEqual([['规划与执行', ['静态规划', '动态规划', '工具调用']]]);
    const s = filterToc(toc, '意图');
    expect(titles(s)).toEqual([['意图识别', ['用户输入规范化', '三层意图识别', '槽位抽取与追问']]]);
    const t = filterToc(toc, '追问');
    expect(titles(t)).toEqual([['意图识别', ['槽位抽取与追问']]]);
    expect(t.matches).toBe(1);
  });
  it('keeps a whole chapter when its title matches', () => {
    expect(filterToc(toc, '开篇').matches).toBe(1);
    expect(filterToc(toc, '规划与执行').matches).toBe(3);
  });
  it('matches a lesson by its number through the course, and any case', () => {
    expect(titles(filterToc(toc, '06'))).toEqual([['规划与执行', ['动态规划']]]);
    expect(titles(filterToc(toc, 'agent'))).toEqual([['开篇', ['为什么 Agent 值得认真对待']]]);
  });
  it('keeps a chapter whose title matches even before it has a lesson', () => {
    const r = filterToc([...toc, chapter(4, '记忆', [])], '记忆');
    expect(titles(r)).toEqual([['记忆', []]]);
    expect(r.matches).toBe(0);
  });
  it('finds nothing where nothing matches', () => {
    expect(filterToc(toc, '向量数据库')).toEqual({ chapters: [], matches: 0 });
  });
});

describe('filtering my courses', () => {
  const course = (id: number, title: string, authorName: string) => ({ id, title, authorName } as CourseSummary);
  const mine = [course(1, 'Agent 系统设计 40 讲', '李明'), course(2, 'Kubernetes 实战', '王芳'), course(3, '分布式系统', '李华')];
  it('keeps the courses whose name or author matches', () => {
    expect(filterMine(mine, 'kubernetes').map((c) => c.id)).toEqual([2]);
    expect(filterMine(mine, '李').map((c) => c.id)).toEqual([1, 3]);
    expect(filterMine(mine, '').map((c) => c.id)).toEqual([1, 2, 3]);
    expect(filterMine(mine, '编译器')).toEqual([]);
  });
});

describe('marking what matched', () => {
  it('splits the text where the words typed are, any case', () => {
    expect(hitParts('Agent 与 agent', 'AGENT')).toEqual([{ text: 'Agent', hit: true }, { text: ' 与 ', hit: false }, { text: 'agent', hit: true }]);
    expect(hitParts('动态规划', '')).toEqual([{ text: '动态规划', hit: false }]);
  });
});

describe('where ← goes on notifications and the admin pages', () => {
  // nextEntered(what it was, the page left, the page come to)
  it('goes back a step to the page the reader just came in from', () => {
    let e = nextEntered(noEntry, '/mine', '/courses/3');
    e = nextEntered(e, '/courses/3', '/notifications');
    expect(backFrom('/notifications', e)).toEqual({ back: true });
  });
  it('goes to the page before the section, not one inside it, once the reader moved within', () => {
    let e = nextEntered(noEntry, '/account', '/admin');
    e = nextEntered(e, '/admin', '/admin/users');
    expect(backFrom('/admin/users', e)).toEqual({ to: '/account' });
  });
  it('goes to the page’s default, in place of the page, with no page before it', () => {
    expect(backFrom('/notifications', noEntry)).toEqual({ to: '/library', replace: true });
    expect(backFrom('/admin/users', noEntry)).toEqual({ to: '/account', replace: true });
    // a page from within the section does not count
    expect(backFrom('/admin', nextEntered(noEntry, '/admin/users', '/admin'))).toEqual({ to: '/account', replace: true });
  });
});

// The concept index of a course version (#158), as the admin page shows
// it: built only when an admin asks, charged to the site.

export type IndexStatus = 'none' | 'building' | 'built' | 'failed';

// IndexedCourse is a library course with its current version's index.
export interface IndexedCourse {
  id: number;
  title: string;
  updatedAt: string;
  lessons: number;
  status: IndexStatus;
  entries: number;
  error?: string;
  startedAt: string | null;
  finishedAt: string | null;
}

const STATES: Record<IndexStatus, { label: string; tone: '' | 'ok' | 'warn' | 'off' }> = {
  none: { label: '未建立', tone: '' },
  building: { label: '建立中', tone: 'warn' },
  built: { label: '已建立', tone: 'ok' },
  failed: { label: '失败', tone: 'off' },
};

// indexState is a status's name and the tone of its dot.
export const indexState = (s: IndexStatus) => STATES[s];

// indexAction is what the admin can do about a version's index: build it,
// or try again after it failed.
export function indexAction(s: IndexStatus): string | null {
  if (s === 'none') return '建立索引';
  if (s === 'failed') return '重试';
  return null;
}

// stillBuilding tells whether some index is being built, for the page to
// keep following it.
export const stillBuilding = (courses: { status: IndexStatus }[]) => courses.some((c) => c.status === 'building');

const ERRORS: Record<string, string> = {
  malformed_output: '模型的输出格式不对',
  output_truncated: '模型的输出被截断',
  timeout: '模型超时',
  rate_limited: '模型限流',
  interrupted: '服务重启, 建立被中断',
};

// indexError says why a build failed, by its run error code.
export const indexError = (code: string) => ERRORS[code] ?? '模型调用失败';

// The recall test (#222): what the linking step would be offered for a
// text, path by path and fused (ADR 0015).

export type HitSource = 'concept' | 'passage' | 'vector';

export interface HitLesson { id: number; key: string; title: string }
export interface HitSection { id: string; title: string }

// PathHit is a section one recall path found, with the path's own score.
export interface PathHit { lesson: HitLesson; section: HitSection | null; label: string; score: number }
export interface PathRanking { source: HitSource; hits: PathHit[] }

// Candidate is a fused candidate, as the linking step is offered it.
export interface Candidate { lesson: HitLesson; section: HitSection | null; label: string; sources: HitSource[]; score: number }
export interface RecallTrace { paths: PathRanking[]; candidates: Candidate[] }

const SOURCES: Record<HitSource, { name: string; note: string }> = {
  concept: { name: '概念', note: '按回答里首次提到的先后' },
  passage: { name: '段落 BM25', note: '中文二元组 · 英文按词' },
  vector: { name: '向量', note: '余弦相似度' },
};

// sourceName is a recall path's name; short is the tag a candidate wears.
export const sourceName = (s: HitSource, short = false) => (short && s === 'passage' ? '段落' : SOURCES[s].name);

// sourceNote says how a path ranks.
export const sourceNote = (s: HitSource) => SOURCES[s].note;

// pathScore shows a path's own score: where the concept path's concept is
// first named, the others' scores as numbers.
export function pathScore(s: HitSource, score: number): string {
  if (s === 'concept') return `第 ${score} 字`;
  return s === 'vector' ? score.toFixed(3) : score.toFixed(1);
}

// hitPlace is where a hit is: its lesson, and its section unless it is at
// the lesson's top or is headed like the lesson.
export const hitPlace = (h: { lesson: HitLesson; section: HitSection | null }) =>
  h.section && h.section.title !== h.lesson.title ? `${h.lesson.title} › ${h.section.title}` : h.lesson.title;

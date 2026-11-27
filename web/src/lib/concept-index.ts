// The concept index of a course version (#158), as the admin page shows
// it: built only when an admin asks, charged to the site.

export type IndexStatus = 'none' | 'building' | 'built' | 'partial' | 'failed';

// IndexedCourse is a library course with its current version's index.
export interface IndexedCourse {
  id: number;
  title: string;
  updatedAt: string;
  lessons: number;
  status: IndexStatus;
  entries: number;
  // failed is how many lessons failed to be extracted.
  failed: number;
  // stale: extracted by another prompt version or model than the current
  // ones; it links until rebuilt.
  stale: boolean;
  error?: string;
  startedAt: string | null;
  finishedAt: string | null;
}

const STATES: Record<IndexStatus, { label: string; tone: '' | 'ok' | 'warn' | 'off' }> = {
  none: { label: '未建立', tone: '' },
  building: { label: '建立中', tone: 'warn' },
  built: { label: '已建立', tone: 'ok' },
  partial: { label: '部分完成', tone: 'warn' },
  failed: { label: '失败', tone: 'off' },
};

// indexState is a status's name and the tone of its dot; a stale index
// says so and warns.
export function indexState(s: IndexStatus, stale = false): { label: string; tone: '' | 'ok' | 'warn' | 'off' } {
  return stale ? { label: `${STATES[s].label} · 过期`, tone: 'warn' } : STATES[s];
}

// indexAction is what the admin can do about a version's index from the
// list: build it, try again after it failed, rebuild it once stale.
export function indexAction(s: IndexStatus, stale = false): string | null {
  if (s === 'none') return '建立索引';
  if (s === 'failed') return '重试';
  if (stale) return '重建';
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
  internal: '保存抽取结果时出错',
};

// LessonStatus is where a lesson of a build stands.
export type LessonStatus = 'pending' | 'running' | 'done' | 'failed';

// IndexLesson is a lesson of an index's build (#224).
export interface IndexLesson { key: string; title: string; chapter: string; status: LessonStatus; error?: string; reused: boolean; entries: number; summary: string }

// IndexDetail is one course's index as its 概况 tab shows it.
export interface IndexDetail {
  status: IndexStatus;
  // stage is where a build is: extracting its lessons, then summarizing.
  stage: 'extract' | 'summarize';
  stale: boolean;
  promptVersion: string;
  modelName: string;
  startedAt: string | null;
  finishedAt: string | null;
  progress: { total: number; done: number; failed: number };
  lessons: IndexLesson[];
  // embedding is how far the version's passages are embedded, while
  // embedding is on (#227).
  embedding?: { done: number; total: number };
}

const LESSON_STATES: Record<LessonStatus, { label: string; tone: '' | 'ok' | 'warn' | 'off' }> = {
  pending: { label: '待处理', tone: '' },
  running: { label: '进行中', tone: 'warn' },
  done: { label: '成功', tone: 'ok' },
  failed: { label: '失败', tone: 'off' },
};

// lessonState names where a lesson stands; one kept from an earlier
// extraction is 复用.
export function lessonState(l: Pick<IndexLesson, 'status' | 'reused'>) {
  return l.status === 'done' && l.reused ? { label: '复用', tone: 'ok' as const } : LESSON_STATES[l.status];
}

// lessonNote is what the row says beside the status: why it failed, or
// how many concepts it has and where they came from.
export function lessonNote(l: IndexLesson): string {
  if (l.status === 'failed') return indexError(l.error ?? '');
  if (l.status !== 'done') return '';
  return l.reused ? `沿用之前的抽取  ·  ${l.entries} 个概念` : `${l.entries} 个概念`;
}

// indexError says why a build failed, by its run error code.
export const indexError = (code: string) => ERRORS[code] ?? '模型调用失败';

// The recall test (#222): what the linking step would be offered for a
// text, path by path and fused (ADR 0015).

export type HitSource = 'concept' | 'passage' | 'vector';

export interface HitLesson { id: number; key: string; title: string }
export interface HitSection { id: string; title: string }

// PathHit is a section one recall path found, with the path's own score.
export interface PathHit { lesson: HitLesson; section: HitSection | null; label: string; score: number; weight?: number }
export interface PathRanking { source: HitSource; hits: PathHit[] }

// Candidate is a fused candidate, as the linking step is offered it.
export interface Candidate { lesson: HitLesson; section: HitSection | null; label: string; sources: HitSource[]; score: number }
// RevisionEffect is how an admin's revision changed a recall (#226).
export interface RevisionEffect { id: number; kind: string; label: string; effect: string }
export interface RecallTrace { paths: PathRanking[]; candidates: Candidate[]; revisions: RevisionEffect[] }

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

// summaryStage says how far the course summary (#225) got: it waits for
// the lessons, runs once they are through, and is done when the build is.
export function summaryStage(x: Pick<IndexDetail, 'status' | 'stage'>): { label: string; tone: '' | 'ok' | 'warn' } {
  if (x.status !== 'building') return { label: '已完成', tone: 'ok' };
  return x.stage === 'summarize' ? { label: '进行中', tone: 'warn' } : { label: '等逐讲抽取完成', tone: '' };
}

// The course-level summary of an index (#225): concepts merged across
// lessons, and the summary tree.

export interface Occurrence { lesson: HitLesson; section: HitSection | null; role: 'define' | 'use'; summary: string }
export interface IndexConcept {
  id: number;
  name: string;
  aliases: string[];
  summary: string;
  generic: boolean;
  lessons: number;
  primary: Occurrence;
  occurrences: Occurrence[];
}

export type ConceptFilter = 'all' | 'specific' | 'generic' | 'revised' | 'disabled' | 'stale';

// fold is how concept names compare (NormalizeConcept on the server):
// width folded, lower case, white space out.
export const fold = (s: string) => s.normalize('NFKC').toLowerCase().replace(/\s+/g, '');

// Revision is an admin's revision of the index (#226): of a concept (by
// its name as extracted), a concept added, or a lesson or section excluded.
export interface Revision {
  id: number;
  kind: 'concept' | 'new' | 'exclude';
  concept?: string;
  name?: string;
  aliases?: string[];
  summary?: string;
  weight: number;
  disabled: boolean;
  generic?: boolean;
  lesson: HitLesson | null;
  section: HitSection | null;
  stale: boolean;
}

export interface LessonSections { lesson: HitLesson; sections: HitSection[] }

// The four weights a revision gives a concept.
export const WEIGHTS: [number, string][] = [[0.5, '降低 ×0.5'], [1, '默认 ×1'], [2, '提高 ×2'], [4, '优先 ×4']];

// ConceptRow is a concept as the 概念 tab lists it: extracted (concept),
// revised (revision), or added (revision alone), its names and flags as
// revised.
export interface ConceptRow {
  key: string;
  concept?: IndexConcept;
  revision?: Revision;
  name: string;
  aliases: string[];
  summary: string;
  generic: boolean;
  weight: number;
  disabled: boolean;
  stale: boolean;
}

// conceptRows lays the revisions over the concepts, the concepts added
// after them.
export function conceptRows(concepts: IndexConcept[], revisions: Revision[]): ConceptRow[] {
  const byKey = new Map(revisions.filter((r) => r.kind === 'concept').map((r) => [r.concept ?? '', r]));
  const rows: ConceptRow[] = concepts.map((c) => {
    const r = byKey.get(fold(c.name));
    return {
      key: `c${c.id}`, concept: c, revision: r,
      name: r?.name ?? c.name, aliases: r?.aliases ?? c.aliases, summary: r?.summary ?? c.summary,
      generic: r?.generic ?? c.generic, weight: r?.weight ?? 1, disabled: !!r?.disabled, stale: !!r?.stale,
    };
  });
  for (const r of revisions) {
    if (r.kind !== 'new') continue;
    rows.push({ key: `r${r.id}`, revision: r, name: r.name ?? '', aliases: r.aliases ?? [], summary: r.summary ?? '', generic: false, weight: r.weight, disabled: false, stale: r.stale });
  }
  return rows;
}

// filterConcepts is the rows whose name or an alias holds the query
// (width, case and spaces aside), kept as the filter says.
export function filterConcepts(rows: ConceptRow[], query: string, filter: ConceptFilter): ConceptRow[] {
  const q = fold(query);
  const keep: Record<ConceptFilter, (r: ConceptRow) => boolean> = {
    all: () => true,
    specific: (r) => !r.generic,
    generic: (r) => r.generic,
    revised: (r) => !!r.revision,
    disabled: (r) => r.disabled,
    stale: (r) => r.stale,
  };
  return rows.filter((r) => keep[filter](r) && (!q || [r.name, ...r.aliases].some((n) => fold(n).includes(q))));
}

// splitAliases reads the aliases field: comma, 、 or semicolon separated.
export const splitAliases = (s: string) => s.split(/[,，、;；]/).map((a) => a.trim()).filter(Boolean);

// roleName is what a lesson does with a concept.
export const roleName = (r: Occurrence['role']) => (r === 'use' ? '用到' : '定义');

export interface TreeLesson { key: string; title: string; status: LessonStatus; summary: string }
export interface TreeChapter { key: string; title: string; summary: string; error?: string; tooLong: boolean; lessons: TreeLesson[] }
export interface SummaryTree { summary: string; error?: string; chapters: TreeChapter[] }

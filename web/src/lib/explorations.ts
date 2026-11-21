import { api } from '@/lib/api';

// An exploration is an analysis the reader pinned (ADR 0007): a stretch of
// the lesson text, placed like an annotation by its quote and the text
// around it, with the head of its exploration document. Only the reader
// sees it.

export interface TextRange { start: number; end: number }

export interface Exploration {
  id: number;
  quote: string;
  prefix: string;
  suffix: string;
  state: 'attached' | 'pending' | 'excerpt';
  title: string;
  intro: string;
  // where the quote sits in the lesson text as it is now, counted in
  // non-whitespace characters like an annotation's
  range: TextRange | null;
  // how many nodes its dialogue has besides the main one (#137)
  nodes: number;
  createdAt: string;
}

const overlaps = (a: TextRange, b: TextRange) => a.start < b.end && b.start < a.end;

// overlapping is the exploration a selection shares text with, which
// keeps it from being analyzed (a stretch of text belongs to one
// exploration at most). It only greys 分析 out: the server decides.
export function overlapping(sel: TextRange | null, list: Exploration[]): Exploration | null {
  if (!sel) return null;
  return list.find((e) => e.range && overlaps(e.range, sel)) ?? null;
}

// what 分析 says when the selection overlaps an exploration
export const OVERLAP_TEXT = '这段文字已有探索';

const path = (courseId: number, lessonId: number) => `/api/courses/${courseId}/lessons/${lessonId}/explorations`;

// listExplorations is the reader's explorations shown in the lesson text.
export const listExplorations = (courseId: number, lessonId: number) =>
  api<{ explorations: Exploration[] }>('GET', path(courseId, lessonId)).then((r) => r.explorations);

// pinAnalysis keeps the reader's finished analysis, named by the ID its
// done event gave, as an exploration.
export const pinAnalysis = (courseId: number, lessonId: number, analysisId: string) =>
  api<Exploration>('POST', path(courseId, lessonId), { analysisId });

// ---- one exploration whole: its document and its dialogue (#134) ----

export type NodeKind = 'main' | 'child' | 'branch' | 'divergent';

export interface Attachment { kind: 'selection'; text: string }
export interface Question { text: string; attachments: Attachment[] }

// why a reply stops short; absent when it is whole
export type Interruption = 'credits_exhausted' | 'timeout' | 'truncated' | 'failed';

export interface Turn {
  id: number;
  parentId: number | null;
  question: Question;
  reply: string;
  meta: { source: 'site' | 'own'; model: string; degraded?: string[]; sources: unknown[]; interrupted?: Interruption };
  createdAt: string;
}

export interface DialogueNode {
  id: number;
  kind: NodeKind;
  name: string;
  parentId: number | null;
  forkTurnId: number | null;
  turns: Turn[];
  createdAt: string;
}

export interface DocItem { id: number; text: string }

export interface ExplorationDetail {
  id: number;
  courseId: number;
  quote: string;
  state: Exploration['state'];
  lessonTitle: string;
  title: string;
  intro: string;
  meaning: string;
  docVersion: number;
  footnotes: DocItem[];
  extensions: DocItem[];
  expansions: DocItem[];
  references: DocItem[];
  nodes: DialogueNode[];
  createdAt: string;
}

export const getExploration = (id: number) => api<ExplorationDetail>('GET', `/api/explorations/${id}`);

export const deleteExploration = (id: number) => api<void>('DELETE', `/api/explorations/${id}`);

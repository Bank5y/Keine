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

// Course drafts (ADR-0006): the course editor's changes are kept on the
// server as an ordered list of operations; the server applies them to the
// course's latest version and sends back the course they make.
import { api, sendFile } from '@/lib/api';
import type { Completion, CourseType, LessonVideo } from '@/lib/courses';

// LessonContent is a lesson's content from an upload of the draft: its
// text and video are found in the upload, or the video is a link.
export interface LessonContent { upload?: string; videoUrl?: string }

export interface InfoChange { title?: string; completion?: Completion; intro?: string; authorName?: string; authorIntro?: string }

// Op is one draft operation. New chapters and lessons get their keys from
// the server. `after` names the chapter (or, for a lesson, the lesson of the
// target chapter) to follow; "" puts it first.
export type Op =
  | { type: 'addChapter'; title: string }
  | { type: 'renameChapter'; chapter: string; title: string }
  | { type: 'deleteChapter'; chapter: string }
  | { type: 'moveChapter'; chapter: string; after: string }
  | { type: 'addUpcomingLesson'; chapter: string; title: string }
  | { type: 'renameLesson'; lesson: string; title: string }
  | { type: 'deleteLesson'; lesson: string }
  | { type: 'moveLesson'; lesson: string; chapter: string; after: string }
  | { type: 'setInfo'; info: InfoChange }
  | { type: 'addLesson'; chapter: string; title: string; content: LessonContent }
  | { type: 'replaceLesson'; lesson: string; title?: string; content: LessonContent }
  | { type: 'uploadLessonContent'; lesson: string; content: LessonContent };

export interface DraftLesson { key: string; title: string; video: '' | 'file' | 'url'; hasText: boolean; upcoming?: boolean; added?: boolean; changed?: boolean }
export interface DraftChapter { key: string; title: string; added?: boolean; lessons: DraftLesson[] }
export interface DraftCourse {
  title: string; type: CourseType; completion: Completion; intro: string; authorName: string; authorIntro: string;
  chapters: DraftChapter[];
}
// mode: how committing takes effect — at once ("direct"), or as an update
// request for an admin to review ("request").
export interface Draft { ops: unknown[]; mode: 'direct' | 'request'; course: DraftCourse }

const base = (id: number) => `/api/courses/${id}/draft`;
export const getDraft = (id: number) => api<Draft>('GET', base(id));
export const addOp = (id: number, op: Op) => api<Draft>('POST', `${base(id)}/ops`, op);
export const undoOp = (id: number) => api<Draft>('POST', `${base(id)}/undo`);
export const discardDraft = (id: number) => api('DELETE', base(id));
export const commitDraft = (id: number) => api<{ outcome: 'updated' | 'requested'; requestId?: number }>('POST', `${base(id)}/commit`);

// Lesson files go to an upload of the draft one request per file, so a
// failed or cancelled file is sent again on its own. A .zip is a single
// lesson's zip, unpacked by the server.
export const startUpload = (id: number) => api<{ upload: string }>('POST', `${base(id)}/uploads`).then((r) => r.upload);
export const putUploadFile = (id: number, upload: string, file: File, onProgress: (p: number) => void, signal?: AbortSignal) =>
  sendFile(`${base(id)}/uploads/${upload}/files/${encodeURIComponent(file.name)}`, file, onProgress, signal);

// LessonPreview is a lesson of the draft as the reader would show it; its
// images and uploaded video are served from the draft.
export interface LessonPreview { key: string; title: string; markdown: string; base: string; video: LessonVideo | null }
export const previewLesson = (id: number, key: string) => api<LessonPreview>('GET', `${base(id)}/lessons/${encodeURIComponent(key)}`);

// The moves below are what the editor's buttons and drags send; a drag
// gives the same operation as the buttons for the same move.

// lessonUp moves a lesson one place up; the first lesson of a chapter goes
// to the end of the chapter before.
export function lessonUp(chapters: DraftChapter[], ci: number, li: number): Op | null {
  const ch = chapters[ci];
  const l = ch.lessons[li];
  if (li > 0) return { type: 'moveLesson', lesson: l.key, chapter: ch.key, after: ch.lessons[li - 2]?.key ?? '' };
  const prev = chapters[ci - 1];
  if (!prev) return null;
  return { type: 'moveLesson', lesson: l.key, chapter: prev.key, after: prev.lessons[prev.lessons.length - 1]?.key ?? '' };
}

// lessonDown moves a lesson one place down; the last lesson of a chapter
// goes to the start of the chapter after.
export function lessonDown(chapters: DraftChapter[], ci: number, li: number): Op | null {
  const ch = chapters[ci];
  const l = ch.lessons[li];
  if (li < ch.lessons.length - 1) return { type: 'moveLesson', lesson: l.key, chapter: ch.key, after: ch.lessons[li + 1].key };
  const next = chapters[ci + 1];
  if (!next) return null;
  return { type: 'moveLesson', lesson: l.key, chapter: next.key, after: '' };
}

// lessonTo moves a lesson to the end of another chapter.
export function lessonTo(chapters: DraftChapter[], lesson: string, chapter: string): Op {
  const to = chapters.find((c) => c.key === chapter)!;
  return { type: 'moveLesson', lesson, chapter, after: to.lessons[to.lessons.length - 1]?.key ?? '' };
}

export function chapterUp(chapters: DraftChapter[], ci: number): Op | null {
  if (ci === 0) return null;
  return { type: 'moveChapter', chapter: chapters[ci].key, after: chapters[ci - 2]?.key ?? '' };
}

export function chapterDown(chapters: DraftChapter[], ci: number): Op | null {
  if (ci === chapters.length - 1) return null;
  return { type: 'moveChapter', chapter: chapters[ci].key, after: chapters[ci + 1].key };
}

// lessonDrop moves a lesson to place `at` of a chapter: the gap before the
// at-th lesson as shown, the lesson itself included. Dropping a lesson where
// it already is gives null.
export function lessonDrop(chapters: DraftChapter[], lesson: string, chapter: string, at: number): Op | null {
  const to = chapters.find((c) => c.key === chapter);
  if (!to) return null;
  const from = to.lessons.findIndex((l) => l.key === lesson);
  if (from >= 0 && (at === from || at === from + 1)) return null;
  const before = to.lessons.slice(0, at).filter((l) => l.key !== lesson);
  return { type: 'moveLesson', lesson, chapter, after: before[before.length - 1]?.key ?? '' };
}

// chapterDrop moves a chapter to place `at`, counted the same way.
export function chapterDrop(chapters: DraftChapter[], chapter: string, at: number): Op | null {
  const from = chapters.findIndex((c) => c.key === chapter);
  if (from < 0 || at === from || at === from + 1) return null;
  const before = chapters.slice(0, at).filter((c) => c.key !== chapter);
  return { type: 'moveChapter', chapter, after: before[before.length - 1]?.key ?? '' };
}

export const hasUpcoming = (c: DraftCourse) => c.chapters.some((ch) => ch.lessons.some((l) => l.upcoming));

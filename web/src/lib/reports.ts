// The exploration report (ADR 0012, #160): a write-up of the whole
// dialogue tree, kept under the exploration. Each generation is a new
// version; the newest shows by default and the older ones stay. It is read
// only, and downloads as Markdown.

import { postStream, type StreamEvent } from '@/lib/analysis';
import { api } from '@/lib/api';

export interface ReportVersion { id: number; version: number; title: string; createdAt: string }
export interface Reference { title: string; url: string }
export interface Report extends ReportVersion {
  summary: string;
  body: string;
  points: string;
  references: Reference[];
  turnIds: number[];
}

export type ReportPart = 'title' | 'summary' | 'body' | 'points';
const PARTS: ReportPart[] = ['title', 'summary', 'body', 'points'];

// Writing is a report as it streams: its parts so far, and whether the
// model is thinking.
export interface Writing { parts: Record<ReportPart, string>; thinking: boolean }

export const blankWriting: Writing = { parts: { title: '', summary: '', body: '', points: '' }, thinking: false };

export function stepWriting(w: Writing, e: StreamEvent): Writing {
  if (e.type === 'activity' && e.kind === 'thinking') return { ...w, thinking: e.status === 'running' };
  if (e.type !== 'delta') return w;
  if (e.reset) return { ...blankWriting };
  const part = e.section as ReportPart;
  if (!PARTS.includes(part)) return w;
  return { parts: { ...w.parts, [part]: w.parts[part] + (e.text ?? '') }, thinking: false };
}

// shownVersion is the version on screen: the one the reader chose while
// it exists, else the newest (the list comes newest first).
export function shownVersion(list: ReportVersion[], chosen: number | null): number | null {
  if (chosen != null && list.some((v) => v.version === chosen)) return chosen;
  return list[0]?.version ?? null;
}

// refusalTitle heads what stopped a report.
export function refusalTitle(code: string): string {
  switch (code) {
    case 'report_too_long': return '对话过长，暂时无法生成';
    case 'credits_insufficient': return '积分不足';
    case 'run_in_progress': return '还有一个回答在进行';
    default: return '报告没有生成完';
  }
}

const base = (exp: number) => `/api/explorations/${exp}/reports`;

export const markdownUrl = (exp: number, version: number) => `${base(exp)}/${version}/markdown`;

export const listReports = (exp: number) => api<{ reports: ReportVersion[] }>('GET', base(exp)).then((r) => r.reports);

export const getReport = (exp: number, version: number) => api<Report>('GET', `${base(exp)}/${version}`);

export type Generated = { ok: true; report: Report } | { ok: false; code: string; message: string };

// generateReport writes a new version, handing on each event as it
// streams, and says how it ended: done carries the version kept.
export async function generateReport(exp: number, on: (e: StreamEvent) => void, signal: AbortSignal): Promise<Generated> {
  let end: Generated = { ok: false, code: 'network', message: '连接中断了，请重试。' };
  await postStream(base(exp), {}, (e) => {
    if (e.type === 'done') {
      const { type: _, ...report } = e;
      end = { ok: true, report: report as Report };
    } else if (e.type === 'error') end = { ok: false, code: e.code, message: e.message || '报告没有生成完，请重试。' };
    else on(e);
  }, signal);
  return end;
}

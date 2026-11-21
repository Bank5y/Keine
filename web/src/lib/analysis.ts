// An analysis as the popover shows it (ADR 0008): the server streams the
// three sections as Server-Sent Events (delta, then done or error), with an
// activity for each lookup, and step folds them into what the popover shows.

import { track, type ToolActivity } from '@/lib/progress';

// the three parts of an analysis: title, intro and meaning ("body")
export type Part = 'title' | 'intro' | 'body';
export type Parts = Record<Part, string>;

export type AnalysisEvent =
  // reset (with no text) throws away the draft streamed so far
  | { type: 'delta'; section: Part | ''; text: string; reset?: boolean }
  // a lookup of the run, or thinking while the model reasons
  | { type: 'activity'; id: string; kind: string; target?: string; status: 'running' | 'done' | 'failed'; reason?: string; ownKey?: boolean }
  // degraded: the web capabilities that went off during the run; sources
  // are the turn's metadata, which the popover does not show, only whether
  // there are any; analysisId names the analysis to pin it by
  | { type: 'done'; analysisId?: string; title: string; intro: string; body: string; degraded?: string[]; sources?: unknown[] }
  // message is the server's, in the reader's words; length comes with
  // length_choice_required
  | { type: 'error'; code: string; message?: string; kept: boolean; length?: LessonLength }
  // the stream closed
  | { type: 'end' }
  // a new run begins (a retry)
  | { type: 'start' };

// LessonLength is a lesson too long to send whole without asking (#131):
// the limits and the lesson in tokens, its length in characters, and
// whether the whole lesson still fits the model.
export interface LessonLength { soft: number; hard: number; lesson: number; chars: number; full: boolean }

// Length is the reader's choice of how much of a long lesson to send.
export type Length = 'related' | 'full';

// Refusal is a run turned away before it started: what to call it, and
// whether trying again can help.
export interface Refusal { title: string; retry: boolean }

const refusals: Record<string, Refusal> = {
  busy: { title: '分析没能开始', retry: true },
  run_in_progress: { title: '分析没能开始', retry: true },
  context_too_long: { title: '分析没能开始', retry: false },
  credits_insufficient: { title: '今日积分不够这次分析', retry: false },
};

// activities are the run's lookups, kept through the answer
export type AnalysisState =
  // choose: the lesson is long, and the reader chooses how much to send
  | { status: 'choose'; length: LessonLength }
  // thinking: the model is reasoning before it answers
  | { status: 'loading'; thinking?: boolean; activities: ToolActivity[] }
  | { status: 'streaming'; parts: Parts; activities: ToolActivity[] }
  // noWeb: the run got no web results, which the popover says apart from
  // the sections (it never goes into the document); analysisId is what
  // pinning names it by
  | { status: 'done'; parts: Parts; activities: ToolActivity[]; noWeb?: true; analysisId?: string }
  | { status: 'error'; message: string; parts: Parts; activities: ToolActivity[]; refusal?: Refusal };

export const initial: AnalysisState = { status: 'loading', activities: [] };
const empty: Parts = { title: '', intro: '', body: '' };

const partsOf = (s: AnalysisState): Parts => ('parts' in s ? s.parts : empty);

export function step(s: AnalysisState, e: AnalysisEvent): AnalysisState {
  if (e.type === 'start') return initial;
  if (s.status === 'done' || s.status === 'error' || s.status === 'choose') return s;
  const { activities } = s;
  switch (e.type) {
    case 'activity':
      if (e.kind !== 'thinking') {
        const { type: _, ...a } = e;
        return { ...s, activities: track(activities, a) };
      }
      if (s.status !== 'loading') return s;
      return { status: 'loading', thinking: e.status === 'running', activities };
    case 'delta': {
      if (e.reset) return { status: 'loading', activities };
      if (!e.section) return s;
      const parts = partsOf(s);
      return { status: 'streaming', parts: { ...parts, [e.section]: parts[e.section] + e.text }, activities };
    }
    case 'done': {
      const done: AnalysisState = { status: 'done', parts: { title: e.title, intro: e.intro, body: e.body }, activities, ...(e.analysisId ? { analysisId: e.analysisId } : {}) };
      // a capability went off and the run found nothing on the web
      return e.degraded?.length && !e.sources?.length ? { ...done, noWeb: true } : done;
    }
    case 'error': {
      if (e.code === 'length_choice_required' && e.length) return { status: 'choose', length: e.length };
      const out: AnalysisState = { status: 'error', message: e.message || FALLBACK_ERROR, parts: e.kept ? partsOf(s) : empty, activities };
      return refusals[e.code] ? { ...out, refusal: refusals[e.code] } : out;
    }
    case 'end':
      return { status: 'error', message: BROKEN, parts: partsOf(s), activities };
  }
  return s;
}

// pinRefusals are why an analysis could not be pinned, by the server's
// code (the server's message says the rest); its content stays in the
// popover either way. An expired one is analyzed again with 重试; one
// overlapping an exploration made meanwhile cannot be pinned at all.
export const pinRefusals: Record<string, Refusal> = {
  analysis_expired: { title: '分析结果已过期', retry: true },
  overlap: { title: '不能固定这次分析', retry: false },
};

// hasContent tells whether the popover shows some of an answer, which
// closing it would throw away.
export const hasContent = (s: AnalysisState) => {
  const p = partsOf(s);
  return !!(p.title || p.intro || p.body);
};

// what went wrong is the server's to say; these cover the server saying
// nothing, and the connection breaking before it could
export const FALLBACK_ERROR = '分析没能完成，请重试。';
const BROKEN = '连接中断了，请重试。';

// readEvents takes the events complete in buf; rest is the start of the
// next one, to be read with the following chunk.
export function readEvents(buf: string): { events: AnalysisEvent[]; rest: string } {
  const blocks = buf.split('\n\n');
  const rest = blocks.pop() ?? '';
  const events: AnalysisEvent[] = [];
  for (const block of blocks) {
    let name = '';
    let data = '';
    for (const line of block.split('\n')) {
      if (line.startsWith('event: ')) name = line.slice(7);
      else if (line.startsWith('data: ')) data += line.slice(6);
    }
    if (name !== 'delta' && name !== 'activity' && name !== 'done' && name !== 'error') continue;
    try { events.push({ type: name, ...JSON.parse(data) }); } catch { /* a broken event is skipped */ }
  }
  return { events, rest };
}

// analyze asks for an analysis of a selection and hands on each event,
// ending with 'end' when the stream closes. A refusal before the run
// (a JSON error) comes as an error event; aborting the signal stops it
// without one.
export async function analyze(courseId: number, lessonId: number, quote: { quote: string; prefix: string; suffix: string }, length: Length | undefined, on: (e: AnalysisEvent) => void, signal: AbortSignal) {
  let res: Response;
  try {
    res = await fetch(`/api/courses/${courseId}/lessons/${lessonId}/analysis`, {
      method: 'POST', credentials: 'same-origin', signal,
      headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ...quote, length }),
    });
  } catch {
    if (!signal.aborted) on({ type: 'error', code: 'network', message: BROKEN, kept: false });
    return;
  }
  if (!res.ok || !res.body) {
    const body = await res.json().catch(() => null);
    on({ type: 'error', code: body?.error?.code ?? 'network', message: body?.error?.message, kept: false, length: body?.error?.length });
    return;
  }
  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
  let buf = '';
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      const r = readEvents(buf + value);
      buf = r.rest;
      r.events.forEach(on);
    }
  } catch {
    if (signal.aborted) return;
  }
  on({ type: 'end' });
}

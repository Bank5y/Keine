// An analysis as the popover shows it (ADR 0008): the server streams the
// three sections as Server-Sent Events (delta, then done or error), and
// step folds them into what the popover shows.

// the three parts of an analysis: title, intro and meaning ("body")
export type Part = 'title' | 'intro' | 'body';
export type Parts = Record<Part, string>;

export type AnalysisEvent =
  | { type: 'delta'; section: Part; text: string }
  // a step of the run in progress; the popover shows only thinking so far
  | { type: 'activity'; id: string; kind: string; target?: string; status: 'running' | 'done' | 'failed' }
  | { type: 'done'; title: string; intro: string; body: string }
  // message is the server's, in the reader's words
  | { type: 'error'; code: string; message?: string; kept: boolean }
  // the stream closed
  | { type: 'end' }
  // a new run begins (a retry)
  | { type: 'start' };

export type AnalysisState =
  // thinking: the model is reasoning before it answers
  | { status: 'loading'; thinking?: boolean }
  | { status: 'streaming'; parts: Parts }
  | { status: 'done'; parts: Parts }
  | { status: 'error'; message: string; parts: Parts };

export const initial: AnalysisState = { status: 'loading' };
const empty: Parts = { title: '', intro: '', body: '' };

const partsOf = (s: AnalysisState): Parts => (s.status === 'loading' ? empty : s.parts);

export function step(s: AnalysisState, e: AnalysisEvent): AnalysisState {
  if (e.type === 'start') return initial;
  if (s.status === 'done' || s.status === 'error') return s;
  switch (e.type) {
    case 'activity':
      if (e.kind !== 'thinking' || s.status !== 'loading') return s;
      return { status: 'loading', thinking: e.status === 'running' };
    case 'delta': {
      const parts = partsOf(s);
      return { status: 'streaming', parts: { ...parts, [e.section]: parts[e.section] + e.text } };
    }
    case 'done':
      return { status: 'done', parts: { title: e.title, intro: e.intro, body: e.body } };
    case 'error':
      return { status: 'error', message: e.message || FALLBACK_ERROR, parts: e.kept ? partsOf(s) : empty };
    case 'end':
      return { status: 'error', message: BROKEN, parts: partsOf(s) };
  }
  return s;
}

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
export async function analyze(courseId: number, lessonId: number, quote: { quote: string; prefix: string; suffix: string }, on: (e: AnalysisEvent) => void, signal: AbortSignal) {
  let res: Response;
  try {
    res = await fetch(`/api/courses/${courseId}/lessons/${lessonId}/analysis`, {
      method: 'POST', credentials: 'same-origin', signal,
      headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(quote),
    });
  } catch {
    if (!signal.aborted) on({ type: 'error', code: 'network', message: BROKEN, kept: false });
    return;
  }
  if (!res.ok || !res.body) {
    const body = await res.json().catch(() => null);
    on({ type: 'error', code: body?.error?.code ?? 'network', message: body?.error?.message, kept: false });
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

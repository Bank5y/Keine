import { describe, expect, it } from 'vitest';
import { FALLBACK_ERROR, hasContent, initial, readEvents, step, type AnalysisState } from '@/lib/analysis';

const run = (...events: Parameters<typeof step>[1][]) => events.reduce<AnalysisState>(step, initial);

describe('step', () => {
  it('starts loading and streams each section as deltas arrive', () => {
    expect(initial).toEqual({ status: 'loading', activities: [] });
    const s = run(
      { type: 'delta', section: 'title', text: 'Agent' },
      { type: 'delta', section: 'title', text: '（智能体）' },
      { type: 'delta', section: 'concept', text: '能自主规划。' },
      { type: 'delta', section: 'explanation', text: '第一段' },
    );
    expect(s).toEqual({ status: 'streaming', activities: [], parts: { title: 'Agent（智能体）', concept: '能自主规划。', explanation: '第一段' } });
  });

  it('shows thinking while the model reasons, until the answer starts', () => {
    const thinking = run({ type: 'activity', id: 'thinking-1', kind: 'thinking', status: 'running' });
    expect(thinking).toEqual({ status: 'loading', activities: [], thinking: true });
    expect(step(thinking, { type: 'activity', id: 'thinking-1', kind: 'thinking', status: 'done' })).toEqual({ status: 'loading', activities: [], thinking: false });
    expect(step(thinking, { type: 'delta', section: 'title', text: 'A' })).toEqual({ status: 'streaming', activities: [], parts: { title: 'A', concept: '', explanation: '' } });
  });

  it('keeps each lookup through the answer, and thinking out of them', () => {
    const looking = run(
      { type: 'activity', id: 'thinking-1', kind: 'thinking', status: 'running' },
      { type: 'activity', id: 'tool-1', kind: 'search_course', target: '任期', status: 'running' },
    );
    expect(looking).toMatchObject({ status: 'loading', thinking: true, activities: [{ id: 'tool-1', kind: 'search_course', target: '任期', status: 'running' }] });
    const answered = [
      { type: 'activity', id: 'tool-1', kind: 'search_course', target: '任期', status: 'done' },
      { type: 'delta', section: 'title', text: '任期' },
      { type: 'done', title: '任期', concept: 'i', explanation: 'b' },
    ].reduce<AnalysisState>((s, e) => step(s, e as Parameters<typeof step>[1]), looking);
    expect(answered).toMatchObject({ status: 'done', parts: { title: '任期', concept: 'i', explanation: 'b' }, activities: [{ id: 'tool-1', kind: 'search_course', target: '任期', status: 'done' }] });
  });

  it('throws the draft away on a reset and shows the progress again', () => {
    const s = run(
      { type: 'activity', id: 'tool-1', kind: 'read_lesson', target: '02', status: 'done' },
      { type: 'delta', section: 'title', text: '草稿' },
      { type: 'delta', section: '', text: '', reset: true },
    );
    expect(s).toMatchObject({ status: 'loading', activities: [{ id: 'tool-1', kind: 'read_lesson', target: '02', status: 'done' }] });
    expect(hasContent(s)).toBe(false);
    expect(step(s, { type: 'delta', section: 'title', text: 'Agent' })).toMatchObject({ status: 'streaming', parts: { title: 'Agent', concept: '', explanation: '' } });
  });

  it('ends done with the sections the server checked', () => {
    const s = run(
      { type: 'delta', section: 'title', text: 'Agent' },
      { type: 'done', title: 'Agent', concept: '简介。', explanation: '含义。' },
    );
    expect(s).toEqual({ status: 'done', activities: [], parts: { title: 'Agent', concept: '简介。', explanation: '含义。' } });
  });

  it('keeps the ID to pin the analysis by', () => {
    const s = run({ type: 'done', analysisId: 'a1', title: 'Agent', concept: '简介。', explanation: '含义。' });
    expect(s).toMatchObject({ status: 'done', analysisId: 'a1' });
  });

  it('notes a run that got no web results, apart from the sections', () => {
    const s = run({ type: 'done', title: 'Agent', concept: '简介。', explanation: '含义。', degraded: ['web_search'], sources: [] });
    expect(s).toEqual({ status: 'done', activities: [], parts: { title: 'Agent', concept: '简介。', explanation: '含义。' }, noWeb: true });
    expect(run({ type: 'done', title: 'A', concept: 'B', explanation: 'C', degraded: [] })).not.toHaveProperty('noWeb');
  });

  it('says nothing of the web when the run found sources before search went off', () => {
    const found = [{ title: 'Raft', url: 'https://raft.github.io/', kind: 'search' }];
    expect(run({ type: 'done', title: 'A', concept: 'B', explanation: 'C', degraded: ['web_search'], sources: found })).not.toHaveProperty('noWeb');
  });

  it('asks the reader how much of a long lesson to send', () => {
    const length = { soft: 12952, hard: 25904, lesson: 20100, chars: 20100, full: true };
    const s = run({ type: 'error', code: 'length_choice_required', message: '这一讲比较长。', kept: false, length });
    expect(s).toEqual({ status: 'choose', length });
    expect(step(s, { type: 'start' })).toEqual(initial);
  });

  it('says a run that was turned away did not start, and whether trying again can help', () => {
    expect(run({ type: 'error', code: 'busy', message: '人多。', kept: false })).toMatchObject({ status: 'error', message: '人多。', refusal: { title: '分析没能开始', retry: true } });
    expect(run({ type: 'error', code: 'run_in_progress', kept: false })).toMatchObject({ refusal: { title: '分析没能开始', retry: true } });
    expect(run({ type: 'error', code: 'context_too_long', kept: false })).toMatchObject({ refusal: { title: '分析没能开始', retry: false } });
    expect(run({ type: 'error', code: 'credits_insufficient', kept: false })).toMatchObject({ refusal: { title: '今日积分不够这次分析', retry: false } });
    expect(run({ type: 'error', code: 'timeout', kept: false })).not.toHaveProperty('refusal');
  });

  it('keeps what streamed when an error says so, and drops it otherwise', () => {
    const kept = run({ type: 'delta', section: 'title', text: 'Agent' }, { type: 'error', code: 'provider_error', message: '连不上。', kept: true });
    expect(kept).toEqual({ status: 'error', activities: [], message: '连不上。', parts: { title: 'Agent', concept: '', explanation: '' } });
    const dropped = run({ type: 'error', code: 'malformed_output', message: '格式不对。', kept: false });
    expect(dropped).toEqual({ status: 'error', activities: [], message: '格式不对。', parts: { title: '', concept: '', explanation: '' } });
  });

  it('falls back to a general message when the server gives none', () => {
    expect(run({ type: 'error', code: 'something_new', kept: false })).toMatchObject({ status: 'error', activities: [], message: FALLBACK_ERROR });
  });

  it('ends an unfinished stream as a broken connection', () => {
    expect(run({ type: 'delta', section: 'explanation', text: '半句' }, { type: 'end' })).toEqual({ status: 'error', activities: [], message: '连接中断了，请重试。', parts: { title: '', concept: '', explanation: '半句' } });
    expect(run({ type: 'done', title: 'A', concept: 'B', explanation: 'C' }, { type: 'end' }).status).toBe('done');
  });

  it('starts over on a retry, whatever came before', () => {
    expect(step(run({ type: 'error', code: 'timeout', kept: false }), { type: 'start' })).toEqual(initial);
    expect(step(run({ type: 'done', title: 'A', concept: 'B', explanation: 'C' }), { type: 'start' })).toEqual(initial);
  });

  it('ignores anything after the run ended', () => {
    const s = run({ type: 'error', code: 'timeout', message: '超时。', kept: false }, { type: 'delta', section: 'title', text: '迟到' });
    expect(s).toEqual({ status: 'error', activities: [], message: '超时。', parts: { title: '', concept: '', explanation: '' } });
  });
});

describe('hasContent', () => {
  it('tells whether closing would throw away something the reader saw', () => {
    expect(hasContent(initial)).toBe(false);
    expect(hasContent(run({ type: 'delta', section: 'title', text: 'A' }))).toBe(true);
    expect(hasContent(run({ type: 'error', code: 'timeout', kept: false }))).toBe(false);
  });
});

describe('readEvents', () => {
  it('reads complete events and keeps a cut-off one for the next chunk', () => {
    const first = readEvents('event: delta\ndata: {"section":"title","text":"Ag"}\n\nevent: delta\ndata: {"sec');
    expect(first.events).toEqual([{ type: 'delta', section: 'title', text: 'Ag' }]);
    const second = readEvents(first.rest + 'tion":"title","text":"ent"}\n\nevent: done\ndata: {"title":"Agent","concept":"i","explanation":"b"}\n\n');
    expect(second.events).toEqual([
      { type: 'delta', section: 'title', text: 'ent' },
      { type: 'done', title: 'Agent', concept: 'i', explanation: 'b' },
    ]);
    expect(second.rest).toBe('');
  });

  it('reads activity events', () => {
    expect(readEvents('event: activity\ndata: {"id":"thinking-1","kind":"thinking","status":"running"}\n\n').events)
      .toEqual([{ type: 'activity', id: 'thinking-1', kind: 'thinking', status: 'running' }]);
  });

  it('reads error events and skips unknown ones', () => {
    const r = readEvents('event: ping\ndata: {}\n\nevent: error\ndata: {"code":"timeout","message":"超时。","kept":true}\n\n');
    expect(r.events).toEqual([{ type: 'error', code: 'timeout', message: '超时。', kept: true }]);
  });
});

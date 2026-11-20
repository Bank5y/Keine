import { describe, expect, it } from 'vitest';
import { errorText, hasContent, initial, readEvents, step, type AnalysisState } from '@/lib/analysis';

const run = (...events: Parameters<typeof step>[1][]) => events.reduce<AnalysisState>(step, initial);

describe('step', () => {
  it('starts loading and streams each section as deltas arrive', () => {
    expect(initial).toEqual({ status: 'loading' });
    const s = run(
      { type: 'delta', section: 'title', text: 'Agent' },
      { type: 'delta', section: 'title', text: '（智能体）' },
      { type: 'delta', section: 'intro', text: '能自主规划。' },
      { type: 'delta', section: 'body', text: '第一段' },
    );
    expect(s).toEqual({ status: 'streaming', parts: { title: 'Agent（智能体）', intro: '能自主规划。', body: '第一段' } });
  });

  it('ends done with the sections the server checked', () => {
    const s = run(
      { type: 'delta', section: 'title', text: 'Agent' },
      { type: 'done', title: 'Agent', intro: '简介。', body: '含义。' },
    );
    expect(s).toEqual({ status: 'done', parts: { title: 'Agent', intro: '简介。', body: '含义。' } });
  });

  it('keeps what streamed when an error says so, and drops it otherwise', () => {
    const kept = run({ type: 'delta', section: 'title', text: 'Agent' }, { type: 'error', code: 'provider_error', kept: true });
    expect(kept).toEqual({ status: 'error', code: 'provider_error', parts: { title: 'Agent', intro: '', body: '' } });
    const dropped = run({ type: 'error', code: 'malformed_output', kept: false });
    expect(dropped).toEqual({ status: 'error', code: 'malformed_output', parts: { title: '', intro: '', body: '' } });
  });

  it('ends an unfinished stream as a broken connection', () => {
    expect(run({ type: 'delta', section: 'body', text: '半句' }, { type: 'end' })).toEqual({ status: 'error', code: 'network', parts: { title: '', intro: '', body: '半句' } });
    expect(run({ type: 'done', title: 'A', intro: 'B', body: 'C' }, { type: 'end' }).status).toBe('done');
  });

  it('starts over on a retry, whatever came before', () => {
    expect(step(run({ type: 'error', code: 'timeout', kept: false }), { type: 'start' })).toEqual(initial);
    expect(step(run({ type: 'done', title: 'A', intro: 'B', body: 'C' }), { type: 'start' })).toEqual(initial);
  });

  it('ignores anything after the run ended', () => {
    const s = run({ type: 'error', code: 'timeout', kept: false }, { type: 'delta', section: 'title', text: '迟到' });
    expect(s).toEqual({ status: 'error', code: 'timeout', parts: { title: '', intro: '', body: '' } });
  });
});

describe('hasContent', () => {
  it('tells whether closing would throw away something the reader saw', () => {
    expect(hasContent(initial)).toBe(false);
    expect(hasContent(run({ type: 'delta', section: 'title', text: 'A' }))).toBe(true);
    expect(hasContent(run({ type: 'error', code: 'timeout', kept: false }))).toBe(false);
  });
});

describe('errorText', () => {
  it('says what went wrong in the reader\'s words, with a fallback', () => {
    expect(errorText('provider_error')).toBe('模型服务暂时无法连接，请稍后再试。');
    expect(errorText('rate_limited')).toContain('繁忙');
    expect(errorText('something_new')).toBe('分析没能完成，请重试。');
  });
});

describe('readEvents', () => {
  it('reads complete events and keeps a cut-off one for the next chunk', () => {
    const first = readEvents('event: delta\ndata: {"section":"title","text":"Ag"}\n\nevent: delta\ndata: {"sec');
    expect(first.events).toEqual([{ type: 'delta', section: 'title', text: 'Ag' }]);
    const second = readEvents(first.rest + 'tion":"title","text":"ent"}\n\nevent: done\ndata: {"title":"Agent","intro":"i","body":"b"}\n\n');
    expect(second.events).toEqual([
      { type: 'delta', section: 'title', text: 'ent' },
      { type: 'done', title: 'Agent', intro: 'i', body: 'b' },
    ]);
    expect(second.rest).toBe('');
  });

  it('reads error events and skips unknown ones', () => {
    const r = readEvents('event: ping\ndata: {}\n\nevent: error\ndata: {"code":"timeout","kept":true}\n\n');
    expect(r.events).toEqual([{ type: 'error', code: 'timeout', kept: true }]);
  });
});

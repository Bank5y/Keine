import { describe, expect, it } from 'vitest';
import { blankWriting, markdownUrl, refusalTitle, shownVersion, stepWriting, type ReportVersion } from './reports';

const v = (version: number): ReportVersion => ({ id: version * 10, version, title: `第 ${version} 版`, createdAt: '' });

describe('stepWriting', () => {
  it('puts each delta into its part as the report streams', () => {
    let w = blankWriting;
    for (const e of [
      { type: 'delta', section: 'title', text: 'Agent' },
      { type: 'delta', section: 'title', text: ' 的分工' },
      { type: 'delta', section: 'summary', text: '摘要。' },
      { type: 'delta', section: 'body', text: '## 原理' },
      { type: 'delta', section: 'points', text: '- 一' },
    ]) w = stepWriting(w, e);
    expect(w.parts).toEqual({ title: 'Agent 的分工', summary: '摘要。', body: '## 原理', points: '- 一' });
  });
  it('throws the parts away on a reset', () => {
    const w = stepWriting(stepWriting(blankWriting, { type: 'delta', section: 'body', text: '草稿' }), { type: 'delta', reset: true });
    expect(w.parts.body).toBe('');
  });
  it('tells while the model thinks, until it writes', () => {
    const thinking = stepWriting(blankWriting, { type: 'activity', kind: 'thinking', status: 'running' });
    expect(thinking.thinking).toBe(true);
    expect(stepWriting(thinking, { type: 'delta', section: 'title', text: 'A' }).thinking).toBe(false);
  });
  it('ignores a part it does not know', () => {
    expect(stepWriting(blankWriting, { type: 'delta', section: 'intro', text: 'x' })).toEqual(blankWriting);
  });
});

describe('shownVersion', () => {
  const list = [v(3), v(2), v(1)];
  it('is the newest by default', () => {
    expect(shownVersion(list, null)).toBe(3);
  });
  it('is the one chosen while it exists', () => {
    expect(shownVersion(list, 1)).toBe(1);
    expect(shownVersion(list, 7)).toBe(3);
  });
  it('is none before the first report', () => {
    expect(shownVersion([], null)).toBeNull();
  });
});

describe('refusalTitle', () => {
  it('says a dialogue too long cannot be written up for now', () => {
    expect(refusalTitle('report_too_long')).toBe('对话过长，暂时无法生成');
    expect(refusalTitle('credits_insufficient')).toBe('积分不足');
    expect(refusalTitle('timeout')).toBe('报告没有生成完');
  });
});

it('downloads a version as Markdown from the server', () => {
  expect(markdownUrl(5, 2)).toBe('/api/explorations/5/reports/2/markdown');
});

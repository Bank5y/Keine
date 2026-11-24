import { describe, expect, it } from 'vitest';
import { currentLine, progressLines, summary, track, webDown, type ToolActivity } from '@/lib/progress';

const act = (id: string, kind: string, target: string, status: ToolActivity['status'], reason?: string): ToolActivity =>
  ({ id, kind, target, status, ...(reason ? { reason } : {}) });
const all = (...acts: ToolActivity[]) => acts.reduce<ToolActivity[]>(track, []);

describe('track', () => {
  it('keeps one entry per call, in the order they started, with the latest status', () => {
    const list = all(
      act('tool-1', 'read_lesson', '02｜三层意图识别', 'running'),
      act('tool-2', 'search_course', 'Agent', 'running'),
      act('tool-1', 'read_lesson', '02｜三层意图识别', 'done'),
    );
    expect(list.map((a) => [a.id, a.status])).toEqual([['tool-1', 'done'], ['tool-2', 'running']]);
  });
});

describe('progressLines', () => {
  it('names what was read and searched, and what failed', () => {
    const lines = progressLines(all(
      act('tool-1', 'read_lesson', '02｜三层意图识别 › 意图的三层', 'done'),
      act('tool-2', 'list_sections', '03｜工具', 'done'),
      act('tool-3', 'search_course', '任期', 'failed', 'not_found'),
    ));
    expect(lines).toEqual([
      { text: '查阅了「02｜三层意图识别 › 意图的三层」', failed: false, running: false },
      { text: '查看了「03｜工具」的目录', failed: false, running: false },
      { text: '没能在课程里查找「任期」', failed: true, running: false, note: '没找到' },
    ]);
  });

  it('shows one call running by what it is about', () => {
    expect(progressLines(all(act('tool-1', 'search_course', '任期', 'running')))).toEqual([
      { text: '正在课程里查找「任期」', failed: false, running: true },
    ]);
  });

  it('merges calls of a kind running at once into one line', () => {
    const lines = progressLines(all(
      act('tool-1', 'search_course', 'a', 'running'),
      act('tool-2', 'search_course', 'b', 'running'),
      act('tool-3', 'search_course', 'c', 'running'),
      act('tool-4', 'read_lesson', '01', 'running'),
    ));
    expect(lines).toEqual([
      { text: '正在课程里查找 3 项', failed: false, running: true },
      { text: '正在查阅「01」', failed: false, running: true },
    ]);
  });

  it('says a lookup of a kind it does not know about in general words', () => {
    expect(progressLines(all(act('tool-1', 'fetch_more', 'example.com', 'done')))).toEqual([
      { text: '查阅了「example.com」', failed: false, running: false },
    ]);
  });

  it('names web searches and the sites read', () => {
    expect(progressLines(all(
      act('tool-1', 'web_search', 'Raft 任期', 'done'),
      act('tool-2', 'web_fetch', 'raft.github.io', 'running'),
    ))).toEqual([
      { text: '搜索了「Raft 任期」', failed: false, running: false },
      { text: '正在读「raft.github.io」', failed: false, running: true },
    ]);
  });

  it('says only that web search is unavailable when the site key failed', () => {
    const [l] = progressLines(all(act('tool-1', 'web_search', 'Raft', 'failed', 'quota_exhausted')));
    expect(l).toEqual({ text: '没能搜索「Raft」', failed: true, running: false, note: '联网搜索暂不可用' });
  });

  it('tells the reader why their own key failed', () => {
    const own = (reason: string) => progressLines([{ ...act('tool-1', 'web_search', 'Raft', 'failed', reason), ownKey: true }])[0].note;
    expect(own('quota_exhausted')).toBe('你的搜索 key 余额不足');
    expect(own('auth')).toBe('你的搜索 key 无效');
    expect(own('unavailable')).toBe('联网搜索暂不可用');
  });

  it('says why a page could not be read', () => {
    const [l] = progressLines(all(act('tool-1', 'web_fetch', 'example.com', 'failed', 'not_found')));
    expect(l).toEqual({ text: '没能读「example.com」', failed: true, running: false, note: '找不到网页' });
    expect(progressLines(all(act('tool-1', 'web_fetch', 'x.pdf', 'failed', 'unsupported_content')))[0].note).toBe('内容读不出来');
  });

  it('says a lesson or section could not be found', () => {
    expect(progressLines(all(act('tool-1', 'read_lesson', 'l99', 'failed', 'not_found')))[0].note).toBe('没找到');
  });

  it('marks a failure it has no words for only as failed', () => {
    const [l] = progressLines(all(act('tool-1', 'read_lesson', 'l99', 'failed', 'something_new')));
    expect(l).toEqual({ text: '没能查阅「l99」', failed: true, running: false });
  });
});

describe('summary', () => {
  it('counts the lessons read and the searches made', () => {
    expect(summary(all(
      act('tool-1', 'read_lesson', '02｜三层意图识别 › 意图的三层', 'done'),
      act('tool-2', 'list_sections', '02｜三层意图识别', 'done'),
      act('tool-3', 'read_lesson', '03｜工具', 'done'),
      act('tool-4', 'search_course', 'a', 'done'),
      act('tool-5', 'search_course', 'b', 'done'),
      act('tool-6', 'search_course', 'c', 'done'),
    ))).toBe('查阅了 2 讲 · 搜索 3 次');
  });

  it('names failures, and leaves out what did not happen', () => {
    expect(summary(all(
      act('tool-1', 'search_course', 'a', 'done'),
      act('tool-2', 'read_lesson', 'l99', 'failed', 'not_found'),
    ))).toBe('搜索 1 次 · 1 项失败');
  });

  it('counts web searches with the searches, and the pages read', () => {
    expect(summary(all(
      act('tool-1', 'web_search', 'a', 'done'),
      act('tool-2', 'search_course', 'b', 'done'),
      act('tool-3', 'web_fetch', 'raft.github.io', 'done'),
    ))).toBe('搜索 2 次 · 读了 1 个网页');
  });

  it('is empty when nothing was looked up', () => {
    expect(summary([])).toBe('');
  });
});

describe('currentLine', () => {
  it('shows the call that started last', () => {
    expect(currentLine(all(
      act('tool-1', 'read_lesson', '01', 'running'),
      act('tool-2', 'web_search', 'Raft', 'running'),
    ))).toEqual({ text: '正在搜索「Raft」', failed: false, running: true });
  });

  it('merges calls of the kind running at once', () => {
    expect(currentLine(all(
      act('tool-1', 'web_search', 'a', 'running'),
      act('tool-2', 'web_search', 'b', 'running'),
      act('tool-3', 'web_search', 'c', 'running'),
    ))?.text).toBe('正在搜索 3 项');
  });

  it('flashes a failure until the next change, then shows what still runs', () => {
    const failed = all(
      act('tool-1', 'web_search', 'a', 'running'),
      act('tool-2', 'web_fetch', 'example.com', 'running'),
      act('tool-2', 'web_fetch', 'example.com', 'failed', 'not_found'),
    );
    expect(currentLine(failed)).toEqual({ text: '没能读「example.com」', failed: true, running: false, note: '找不到网页' });
    expect(currentLine(track(failed, act('tool-3', 'read_lesson', '01', 'running')))?.text).toBe('正在查阅「01」');
  });

  it('goes back to a call still running when the last one finished', () => {
    expect(currentLine(all(
      act('tool-1', 'web_search', 'a', 'running'),
      act('tool-2', 'read_lesson', '01', 'running'),
      act('tool-2', 'read_lesson', '01', 'done'),
    ))?.text).toBe('正在搜索「a」');
  });

  it('shows what was done last once nothing runs', () => {
    expect(currentLine(all(
      act('tool-1', 'web_search', 'a', 'running'),
      act('tool-1', 'web_search', 'a', 'done'),
    ))?.text).toBe('搜索了「a」');
  });

  it('is nothing before any call', () => {
    expect(currentLine([])).toBeNull();
  });

  it('once a failure has flashed, goes back to what still runs', () => {
    const failed = all(
      act('tool-1', 'web_search', 'a', 'running'),
      act('tool-2', 'web_fetch', 'example.com', 'running'),
      act('tool-2', 'web_fetch', 'example.com', 'failed', 'not_found'),
    );
    expect(currentLine(failed, true)?.text).toBe('正在搜索「a」');
  });

  it('once a failure has flashed with nothing running, shows what was done last', () => {
    const failed = all(
      act('tool-1', 'web_search', 'a', 'running'),
      act('tool-1', 'web_search', 'a', 'done'),
      act('tool-2', 'web_fetch', 'example.com', 'running'),
      act('tool-2', 'web_fetch', 'example.com', 'failed', 'not_found'),
    );
    expect(currentLine(failed, true)?.text).toBe('搜索了「a」');
  });
});

describe('webDown', () => {
  it('says web search is unavailable once the service failed on the site key', () => {
    expect(webDown(all(
      act('tool-1', 'web_search', 'a', 'failed', 'quota_exhausted'),
      act('tool-2', 'read_lesson', '01', 'done'),
    ))).toBe('联网搜索暂不可用');
  });

  it("says why the reader's own key failed", () => {
    expect(webDown([{ ...act('tool-1', 'web_search', 'a', 'failed', 'auth'), ownKey: true }])).toBe('你的搜索 key 无效');
  });

  it('is nothing once a search went through after the service failed', () => {
    expect(webDown(all(
      act('tool-1', 'web_search', 'a', 'failed', 'timeout'),
      act('tool-2', 'web_search', 'b', 'running'),
      act('tool-2', 'web_search', 'b', 'done'),
    ))).toBeUndefined();
  });

  it('is nothing when only a page failed, or nothing failed', () => {
    expect(webDown(all(act('tool-1', 'web_fetch', 'example.com', 'failed', 'not_found')))).toBeUndefined();
    expect(webDown(all(act('tool-1', 'web_search', 'a', 'done')))).toBeUndefined();
  });
});

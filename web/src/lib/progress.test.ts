import { describe, expect, it } from 'vitest';
import { progressLines, summary, track, type ToolActivity } from '@/lib/progress';

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
      { text: '没能在课程里查找「任期」', failed: true, running: false },
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
    expect(progressLines(all(act('tool-1', 'web_fetch', 'example.com', 'done')))).toEqual([
      { text: '查阅了「example.com」', failed: false, running: false },
    ]);
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

  it('is empty when nothing was looked up', () => {
    expect(summary([])).toBe('');
  });
});

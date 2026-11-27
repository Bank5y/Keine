import { describe, expect, it } from 'vitest';
import { conceptRows, filterConcepts, splitAliases, summaryStage, type IndexConcept, type Revision, hitPlace, indexAction, indexError, indexState, lessonNote, lessonState, pathScore, sourceName, stillBuilding, type IndexLesson } from '@/lib/concept-index';

describe('indexState', () => {
  it('names each status', () => {
    expect(indexState('none')).toEqual({ label: '未建立', tone: '' });
    expect(indexState('building')).toEqual({ label: '建立中', tone: 'warn' });
    expect(indexState('built')).toEqual({ label: '已建立', tone: 'ok' });
    expect(indexState('failed')).toEqual({ label: '失败', tone: 'off' });
    expect(indexState('partial')).toEqual({ label: '部分完成', tone: 'warn' });
  });

  it('warns of a stale index', () => {
    expect(indexState('built', true)).toEqual({ label: '已建立 · 过期', tone: 'warn' });
  });
});

describe('indexAction', () => {
  it('builds a version without an index, and tries a failed one again', () => {
    expect(indexAction('none')).toBe('建立索引');
    expect(indexAction('failed')).toBe('重试');
  });

  it('offers nothing while building or once built', () => {
    expect(indexAction('building')).toBeNull();
    expect(indexAction('built')).toBeNull();
    expect(indexAction('partial')).toBeNull();
  });

  it('rebuilds a stale index', () => {
    expect(indexAction('built', true)).toBe('重建');
  });
});

describe('stillBuilding', () => {
  it('tells whether the page has a build to follow', () => {
    expect(stillBuilding([{ status: 'built' }, { status: 'none' }])).toBe(false);
    expect(stillBuilding([{ status: 'built' }, { status: 'building' }])).toBe(true);
  });
});

describe('indexError', () => {
  it('says why a build failed in words', () => {
    expect(indexError('malformed_output')).toBe('模型的输出格式不对');
    expect(indexError('interrupted')).toBe('服务重启, 建立被中断');
    expect(indexError('something_new')).toBe('模型调用失败');
  });
});

describe('pathScore', () => {
  it('shows where the concept path found the concept', () => {
    expect(pathScore('concept', 12)).toBe('第 12 字');
  });

  it('shows the other paths their scores', () => {
    expect(pathScore('passage', 14.237)).toBe('14.2');
    expect(pathScore('vector', 0.83127)).toBe('0.831');
  });
});

describe('hitPlace', () => {
  const lesson = { id: 1, key: 'l08', title: '领导者选举' };
  it('names the lesson and the section', () => {
    expect(hitPlace({ lesson, section: { id: 'h-1', title: '选举超时' } })).toBe('领导者选举 › 选举超时');
  });
  it('names the lesson alone at its top', () => {
    expect(hitPlace({ lesson, section: null })).toBe('领导者选举');
  });
  it('names the lesson alone when its section is headed like it', () => {
    expect(hitPlace({ lesson, section: { id: 'h-0', title: '领导者选举' } })).toBe('领导者选举');
  });
});

describe('sourceName', () => {
  it('names the passage path shorter on a candidate', () => {
    expect(sourceName('passage')).toBe('段落 BM25');
    expect(sourceName('passage', true)).toBe('段落');
    expect(sourceName('concept', true)).toBe('概念');
  });
});

describe('lessonState and lessonNote', () => {
  const lesson: IndexLesson = { key: 'l01', title: '01', chapter: '基础', status: 'done', reused: false, entries: 12, summary: '' };
  it('counts the concepts of a lesson extracted', () => {
    expect(lessonState(lesson)).toEqual({ label: '成功', tone: 'ok' });
    expect(lessonNote(lesson)).toBe('12 个概念');
  });
  it('tells a lesson kept from an earlier extraction', () => {
    expect(lessonState({ ...lesson, reused: true }).label).toBe('复用');
    expect(lessonNote({ ...lesson, reused: true })).toBe('沿用之前的抽取  ·  12 个概念');
  });
  it('says why a lesson failed', () => {
    expect(lessonState({ ...lesson, status: 'failed' })).toEqual({ label: '失败', tone: 'off' });
    expect(lessonNote({ ...lesson, status: 'failed', error: 'timeout' })).toBe('模型超时');
  });
  it('says nothing of a lesson not through yet', () => {
    expect(lessonNote({ ...lesson, status: 'running' })).toBe('');
  });
});

describe('summaryStage', () => {
  it('waits for the lessons, runs, then is done', () => {
    expect(summaryStage({ status: 'building', stage: 'extract' }).label).toBe('等逐讲抽取完成');
    expect(summaryStage({ status: 'building', stage: 'summarize' })).toEqual({ label: '进行中', tone: 'warn' });
    expect(summaryStage({ status: 'partial', stage: 'summarize' })).toEqual({ label: '已完成', tone: 'ok' });
  });
});

describe('conceptRows and filterConcepts', () => {
  const at = { lesson: { id: 1, key: 'l01', title: '01' }, section: null, role: 'define' as const, summary: '' };
  const c = (id: number, name: string, aliases: string[], generic: boolean): IndexConcept => ({ id, name, aliases, summary: '', generic, lessons: 1, primary: at, occurrences: [at] });
  const all = [c(1, '任期', ['term'], false), c(2, '领导者', ['Leader'], true), c(3, '日志复制', ['log replication'], false)];
  const rev = (r: Partial<Revision>): Revision => ({ id: 9, kind: 'concept', weight: 1, disabled: false, lesson: null, section: null, stale: false, ...r });
  const names = (rows: ReturnType<typeof conceptRows>) => rows.map((x) => x.name);

  it('finds a concept by name or alias, case, width and spaces aside', () => {
    const rows = conceptRows(all, []);
    expect(names(filterConcepts(rows, 'ＴＥＲＭ', 'all'))).toEqual(['任期']);
    expect(names(filterConcepts(rows, 'logreplication', 'all'))).toEqual(['日志复制']);
    expect(names(filterConcepts(rows, '领导', 'all'))).toEqual(['领导者']);
  });

  it('keeps the generic ones or the others', () => {
    const rows = conceptRows(all, []);
    expect(names(filterConcepts(rows, '', 'generic'))).toEqual(['领导者']);
    expect(names(filterConcepts(rows, '', 'specific'))).toEqual(['任期', '日志复制']);
  });

  it('lays a revision over the concept extracted under its name', () => {
    const rows = conceptRows(all, [rev({ concept: '领导者', name: '主节点', generic: false, weight: 2, disabled: true, stale: true })]);
    const r = rows[1];
    expect([r.name, r.generic, r.weight, r.disabled, r.stale]).toEqual(['主节点', false, 2, true, true]);
    expect(names(filterConcepts(rows, '', 'revised'))).toEqual(['主节点']);
    expect(names(filterConcepts(rows, '', 'disabled'))).toEqual(['主节点']);
    expect(names(filterConcepts(rows, '', 'stale'))).toEqual(['主节点']);
  });

  it('lists the concepts added after the extracted ones', () => {
    const rows = conceptRows(all, [rev({ id: 5, kind: 'new', name: '脑裂', aliases: ['split brain'] })]);
    expect(names(rows)).toEqual(['任期', '领导者', '日志复制', '脑裂']);
    expect(rows[3].concept).toBeUndefined();
  });
});

describe('splitAliases', () => {
  it('splits on commas and the like', () => {
    expect(splitAliases('log replication, 复制日志、AppendEntries；x ,')).toEqual(['log replication', '复制日志', 'AppendEntries', 'x']);
  });
});

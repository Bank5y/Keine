import { describe, expect, it } from 'vitest';
import { filterConcepts, summaryStage, type IndexConcept, hitPlace, indexAction, indexError, indexState, lessonNote, lessonState, pathScore, sourceName, stillBuilding, type IndexLesson } from '@/lib/concept-index';

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

describe('filterConcepts', () => {
  const at = { lesson: { id: 1, key: 'l01', title: '01' }, section: null, role: 'define' as const, summary: '' };
  const c = (name: string, aliases: string[], generic: boolean): IndexConcept => ({ id: 1, name, aliases, summary: '', generic, lessons: 1, primary: at, occurrences: [at] });
  const all = [c('任期', ['term'], false), c('领导者', ['Leader'], true), c('日志复制', ['log replication'], false)];
  const names = (cs: IndexConcept[]) => cs.map((x) => x.name);

  it('finds a concept by name or alias, case, width and spaces aside', () => {
    expect(names(filterConcepts(all, 'ＴＥＲＭ', 'all'))).toEqual(['任期']);
    expect(names(filterConcepts(all, 'logreplication', 'all'))).toEqual(['日志复制']);
    expect(names(filterConcepts(all, '领导', 'all'))).toEqual(['领导者']);
  });

  it('keeps the generic ones or the others', () => {
    expect(names(filterConcepts(all, '', 'generic'))).toEqual(['领导者']);
    expect(names(filterConcepts(all, '', 'specific'))).toEqual(['任期', '日志复制']);
    expect(filterConcepts(all, '', 'all')).toHaveLength(3);
  });
});

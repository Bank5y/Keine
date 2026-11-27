import { describe, expect, it } from 'vitest';
import { hitPlace, indexAction, indexError, indexState, pathScore, sourceName, stillBuilding } from '@/lib/concept-index';

describe('indexState', () => {
  it('names each status', () => {
    expect(indexState('none')).toEqual({ label: '未建立', tone: '' });
    expect(indexState('building')).toEqual({ label: '建立中', tone: 'warn' });
    expect(indexState('built')).toEqual({ label: '已建立', tone: 'ok' });
    expect(indexState('failed')).toEqual({ label: '失败', tone: 'off' });
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

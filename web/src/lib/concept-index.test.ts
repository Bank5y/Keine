import { describe, expect, it } from 'vitest';
import { indexAction, indexError, indexState, stillBuilding } from '@/lib/concept-index';

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

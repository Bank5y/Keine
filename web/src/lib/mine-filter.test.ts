import { describe, expect, it } from 'vitest';
import { mineParams, readMineFilter } from '@/lib/mine-filter';

describe('readMineFilter', () => {
  it('reads 学习, type and completion from the address', () => {
    expect(readMineFilter('?state=unfinished&type=video&completion=finished')).toEqual({ state: 'unfinished', type: 'video', completion: 'finished' });
  });

  it('reads a missing or unknown value as 全部', () => {
    expect(readMineFilter('')).toEqual({ state: '', type: '', completion: '' });
    expect(readMineFilter('?state=half&type=novel&completion=done')).toEqual({ state: '', type: '', completion: '' });
  });
});

describe('mineParams', () => {
  it('writes the chosen dimensions for the my-courses API, leaving 全部 out', () => {
    expect(mineParams({ state: 'done', type: '', completion: 'ongoing' }).toString()).toBe('state=done&completion=ongoing');
    expect(mineParams({ state: '', type: '', completion: '' }).toString()).toBe('');
  });
});

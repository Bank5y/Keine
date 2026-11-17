import { describe, expect, it } from 'vitest';
import { COMPLETIONS, filterParams, oneOf } from '@/lib/course-filter';

describe('oneOf', () => {
  it('keeps an allowed value and reads a missing or unknown one as 全部', () => {
    expect(oneOf('ongoing', COMPLETIONS)).toBe('ongoing');
    expect(oneOf(null, COMPLETIONS)).toBe('');
    expect(oneOf('done', COMPLETIONS)).toBe('');
  });
});

describe('filterParams', () => {
  it('writes the chosen dimensions in order, leaving 全部 and blanks out', () => {
    expect(filterParams({ state: 'done', type: '', completion: 'ongoing', q: '   ' }).toString()).toBe('state=done&completion=ongoing');
    expect(filterParams({ type: undefined }).toString()).toBe('');
  });

  it('trims what it writes', () => {
    expect(filterParams({ q: '  go ' }).toString()).toBe('q=go');
  });
});

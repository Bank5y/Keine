import { describe, expect, it } from 'vitest';
import { libraryHref, readLibraryFilter } from '@/lib/library-filter';

describe('readLibraryFilter', () => {
  it('reads type, completion and keywords from the address', () => {
    expect(readLibraryFilter('?type=column&completion=ongoing&q=agent')).toEqual({ type: 'column', completion: 'ongoing', q: 'agent' });
  });

  it('reads a missing or unknown value as 全部', () => {
    expect(readLibraryFilter('')).toEqual({ type: '', completion: '', q: '' });
    expect(readLibraryFilter('?type=novel&completion=done')).toEqual({ type: '', completion: '', q: '' });
  });
});

describe('libraryHref', () => {
  it('writes each chosen dimension into the address', () => {
    expect(libraryHref({ type: 'column', completion: 'ongoing', q: 'agent' })).toBe('/library?type=column&completion=ongoing&q=agent');
  });

  it('leaves out a dimension set to 全部', () => {
    expect(libraryHref({ type: '', completion: 'ongoing', q: '' })).toBe('/library?completion=ongoing');
    expect(libraryHref({ type: '', completion: '', q: '' })).toBe('/library');
  });

  it('takes a search typed on another page to the library with its trimmed keywords', () => {
    expect(libraryHref({ q: '  分布式 系统 ' })).toBe('/library?q=%E5%88%86%E5%B8%83%E5%BC%8F+%E7%B3%BB%E7%BB%9F');
    expect(libraryHref({ q: '   ' })).toBe('/library');
  });

  it('reads back the filter it wrote', () => {
    const f = { type: 'video', completion: 'incomplete', q: 'go & rust' } as const;
    expect(readLibraryFilter(libraryHref(f).split('?')[1])).toEqual(f);
  });
});

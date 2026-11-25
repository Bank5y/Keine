import { describe, expect, it } from 'vitest';
import { activeItem, navItems } from '@/lib/nav';

const user = { role: 'user' as const };
const admin = { role: 'admin' as const };
const labels = (items: { label: string }[]) => items.map((i) => i.label);

describe('navItems', () => {
  it('gives everyone the same four items in the bottom bar', () => {
    expect(labels(navItems('bottom', user))).toEqual(['课程库', '我的课程', '笔记', '我的']);
    expect(labels(navItems('bottom', admin))).toEqual(['课程库', '我的课程', '笔记', '我的']);
  });

  it('gives everyone the same three items in the desktop masthead (admin is the gear)', () => {
    expect(labels(navItems('top', user))).toEqual(['课程库', '我的课程', '笔记']);
    expect(labels(navItems('top', admin))).toEqual(['课程库', '我的课程', '笔记']);
  });
});

describe('activeItem', () => {
  const current = (surface: 'top' | 'bottom', path: string) => activeItem(navItems(surface, admin), path)?.label ?? null;

  it('highlights the item a main page belongs to', () => {
    expect(current('bottom', '/library')).toBe('课程库');
    expect(current('bottom', '/mine')).toBe('我的课程');
    expect(current('bottom', '/notes')).toBe('笔记');
    expect(current('bottom', '/account')).toBe('我的');
  });

  it('puts the admin pages under 我的 on a phone, under no item on the desktop', () => {
    for (const path of ['/admin', '/admin/users']) {
      expect(current('bottom', path)).toBe('我的');
      expect(current('top', path)).toBeNull();
    }
  });

  it('highlights nothing on pages outside every item', () => {
    for (const path of ['/notifications', '/courses/3', '/courses/3/edit', '/courses/3/lessons/12', '/courses/3/lessons/12/migrate']) {
      expect(current('bottom', path)).toBeNull();
      expect(current('top', path)).toBeNull();
    }
  });

  it('matches whole path segments only', () => {
    expect(current('bottom', '/library-old')).toBeNull();
  });
});

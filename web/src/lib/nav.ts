import { BookMarked, Library, NotebookPen, UserRound, type LucideIcon } from 'lucide-react';
import type { User } from '@/lib/session';

// The main navigation, declared once: the desktop masthead and the narrow
// bottom bar both render from it. A new item is one more entry here.
//
// surface: where the item shows, the masthead ('top') or the bottom bar.
// covers: the addresses that count as being under the item.
// The admin pages are not an item: the desktop reaches them from the gear
// at the masthead's right, a phone from 我的.
export type NavSurface = 'top' | 'bottom';

export interface NavItem {
  label: string;
  icon: LucideIcon;
  href: string;
  covers: string[];
  surfaces: NavSurface[];
  adminOnly?: boolean;
}

const ITEMS: NavItem[] = [
  { label: '课程库', icon: Library, href: '/library', covers: ['/library'], surfaces: ['top', 'bottom'] },
  { label: '我的课程', icon: BookMarked, href: '/mine', covers: ['/mine'], surfaces: ['top', 'bottom'] },
  { label: '笔记', icon: NotebookPen, href: '/notes', covers: ['/notes'], surfaces: ['top', 'bottom'] },
  // the admin pages are reached from 我的 on a phone, so they count under it
  { label: '我的', icon: UserRound, href: '/account', covers: ['/account', '/admin'], surfaces: ['bottom'] },
];

export function navItems(surface: NavSurface, user: Pick<User, 'role'>): NavItem[] {
  return ITEMS.filter((i) => i.surfaces.includes(surface) && (!i.adminOnly || user.role === 'admin'));
}

// activeItem is the item the page at path counts under, or null for a page
// outside every item (a course, a notification, a lesson).
export function activeItem(items: NavItem[], path: string): NavItem | null {
  return items.find((i) => i.covers.some((p) => path === p || path.startsWith(p + '/'))) ?? null;
}

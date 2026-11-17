import { activeItem, navItems, type NavItem } from '@/lib/nav';
import { Link } from '@/lib/router';
import type { User } from '@/lib/session';
import { cn } from '@/lib/utils';

// NavLink is one main-navigation item, in the masthead (label) or the
// bottom bar (icon over label).
export function NavLink({ item, current, icon = false }: { item: NavItem; current: boolean; icon?: boolean }) {
  return (
    <Link href={item.href} className={cn(current && 'active')} aria-current={current ? 'page' : undefined}>
      {icon ? <><item.icon aria-hidden="true" /><span>{item.label}</span></> : item.label}
    </Link>
  );
}

// BottomNav is the narrow bar of main pages, reached with a thumb.
export function BottomNav({ user, path }: { user: User; path: string }) {
  const items = navItems('bottom', user);
  const current = activeItem(items, path);
  return (
    <nav className="tabbar" aria-label="主导航">
      {items.map((item) => <NavLink key={item.href} item={item} current={item === current} icon />)}
    </nav>
  );
}

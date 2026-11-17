import { useState, type FormEvent } from 'react';
import { LogOut, Moon, Search, Sun } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { NotificationBell } from '@/components/notification-bell';
import { NavLink } from '@/components/bottom-nav';
import { activeItem, navItems } from '@/lib/nav';
import { Link, navigate, setParam, useParam } from '@/lib/router';
import { logout, type User } from '@/lib/session';
import { toggleTheme } from '@/lib/theme';

function ThemeToggle() {
  return (
    <Button variant="round" size="round" title="切换深色 / 浅色" aria-label="切换主题" onClick={toggleTheme}>
      <Sun className="i-sun" />
      <Moon className="i-moon" />
    </Button>
  );
}

// Masthead is the desktop top bar, and the one shown before signing in.
export function Masthead({ user, path }: { user: User | null | undefined; path: string }) {
  const nav = user ? navItems('top', user) : [];
  const current = activeItem(nav, path);
  return (
    <header className="masthead">
      <div className="mast-inner">
        <Link className="wordmark" href={user ? '/library' : '/login'} aria-label="Keine">
          <span className="seal" aria-hidden="true">慧</span>
          <span className="wm-text">Keine<span className="wm-sub">学习空间</span></span>
        </Link>
        <nav className="nav" aria-label="主导航">
          {nav.map((item) => <NavLink key={item.href} item={item} current={item === current} />)}
        </nav>
        <div className="mast-tools">
          {user ? <NotificationBell path={path} /> : null}
          {user ? <Link className="mast-user" href="/account" title="我的">{user.username}</Link> : null}
          <ThemeToggle />
          {user ? (
            <Button variant="round" size="round" title="退出" aria-label="退出" onClick={() => void logout()}>
              <LogOut />
            </Button>
          ) : null}
        </div>
      </div>
    </header>
  );
}

// TopBar is the narrow top bar: search, notifications, theme. The search
// always searches the library: on the library it filters as you type, on
// any other page Enter opens the library on the results.
export function TopBar({ path }: { path: string }) {
  return (
    <header className="masthead topbar">
      <div className="mast-inner">
        <TopSearch onLibrary={path === '/library'} />
        <div className="mast-tools">
          <NotificationBell path={path} />
          <ThemeToggle />
        </div>
      </div>
    </header>
  );
}

function TopSearch({ onLibrary }: { onLibrary: boolean }) {
  const libraryQuery = useParam('q');
  const [draft, setDraft] = useState('');
  const value = onLibrary ? libraryQuery : draft;

  function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    e.currentTarget.querySelector('input')?.blur();
    if (onLibrary) return;
    const q = draft.trim();
    setDraft('');
    navigate(q ? `/library?q=${encodeURIComponent(q)}` : '/library');
  }

  return (
    <form className="top-search" role="search" onSubmit={submit}>
      <Search aria-hidden="true" />
      <input
        type="search" enterKeyHint="search" placeholder="搜索课程名或讲师" aria-label="搜索课程"
        value={value} onChange={(e) => (onLibrary ? setParam('q', e.target.value) : setDraft(e.target.value))}
      />
    </form>
  );
}

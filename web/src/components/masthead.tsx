import { useEffect, useRef, type FormEvent } from 'react';
import { ArrowLeft, LogOut, Moon, Search, Sun, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { NotificationBell } from '@/components/notification-bell';
import { NavLink } from '@/components/bottom-nav';
import { libraryHref } from '@/lib/library-filter';
import { activeItem, navItems } from '@/lib/nav';
import { Link, navigate, setParam, useParam } from '@/lib/router';
import { backTarget, topBarFor, type TopBar } from '@/lib/page-search';
import { logout, type User } from '@/lib/session';
import { askLibraryFocus, setTopSearch, takeLibraryFocus, useTopSearch } from '@/lib/top-search';
import { toggleTheme } from '@/lib/theme';
import { cn } from '@/lib/utils';

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
          <span className="wm-text">Keine</span>
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

// TopBar is the narrow top bar. Most pages have a search scoped to the
// page (see page-search.ts), with notifications and the theme beside it:
// tapped, it searches in place (the field widens, the others make way for
// 清空 and 取消) or, on a page with nothing to search, opens the library's.
// Notifications and the admin pages have ← and the page's name instead; ←
// goes back to the page come in from (from), else to the page's default.
export function TopBar({ path, from }: { path: string; from: string | null }) {
  const bar = topBarFor(path);
  const query = useTopSearch();
  const libraryQuery = useParam('q');
  // a search belongs to its page: another page starts with none
  useEffect(() => () => setTopSearch(null), [path]);
  if (bar.kind === 'back') {
    return (
      <header className="masthead topbar">
        <div className="mast-inner">
          <button type="button" className="topbar-back" aria-label="返回" onClick={() => navigate(backTarget(path, from))}><ArrowLeft /></button>
          <b className="topbar-title">{bar.title}</b>
          <div className="mast-tools">
            <NotificationBell path={path} />
            <ThemeToggle />
          </div>
        </div>
      </header>
    );
  }
  // the library searches by its address: words there are a search under way
  const searching = query != null || (bar.scope === 'library' && bar.inPlace && libraryQuery !== '');
  return (
    <header className={cn('masthead topbar', searching && 'searching')}>
      <div className="mast-inner">
        <TopSearch bar={bar} searching={searching} />
        {searching ? (
          <button type="button" className="topbar-cancel" onClick={() => cancel(bar.scope)}>取消</button>
        ) : (
          <div className="mast-tools">
            <NotificationBell path={path} />
            <ThemeToggle />
          </div>
        )}
      </div>
    </header>
  );
}

// cancel ends a search, the page back as it was
function cancel(scope: string) {
  if (scope === 'library') setParam('q', '');
  setTopSearch(null);
}

function TopSearch({ bar, searching }: { bar: Extract<TopBar, { kind: 'search' }>; searching: boolean }) {
  const libraryQuery = useParam('q');
  const query = useTopSearch();
  const library = bar.scope === 'library';
  const value = library ? libraryQuery : query ?? '';
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => { if (library && bar.inPlace && takeLibraryFocus()) { setTopSearch(''); input.current?.focus(); } }, [library, bar.inPlace]);

  const write = (v: string) => {
    if (library) setParam('q', v);
    setTopSearch(library ? '' : v);
  };
  const start = () => {
    if (!bar.inPlace) { askLibraryFocus(); navigate(libraryHref({})); return; }
    if (!searching) setTopSearch('');
  };
  function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    input.current?.blur();
  }

  return (
    <form className={cn('top-search', searching && 'on')} role="search" onSubmit={submit}>
      <Search aria-hidden="true" />
      <b className="top-search-scope">{bar.label}</b>
      <i className="top-search-rule" aria-hidden="true" />
      <input
        ref={input} type="search" enterKeyHint="search" placeholder={bar.placeholder} aria-label={`搜索${bar.label}`}
        value={value} readOnly={!bar.inPlace}
        onFocus={start} onChange={(e) => write(e.target.value)}
        onKeyDown={(e) => { if (e.key === 'Escape') { e.preventDefault(); cancel(bar.scope); input.current?.blur(); } }}
      />
      {searching && value ? (
        <button type="button" className="top-search-clear" aria-label="清空" onMouseDown={(e) => e.preventDefault()} onClick={() => { write(''); input.current?.focus(); }}><X /></button>
      ) : null}
    </form>
  );
}

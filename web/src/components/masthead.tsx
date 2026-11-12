import { LogOut, Moon, Sun } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Link } from '@/lib/router';
import { logout, type User } from '@/lib/session';
import { toggleTheme } from '@/lib/theme';
import { cn } from '@/lib/utils';

export function Masthead({ user, path }: { user: User | null | undefined; path: string }) {
  const nav: [string, string][] = user ? [['/library', '课程库'], ['/mine', '我的课程']] : [];
  if (user?.role === 'admin') nav.push(['/admin', '管理']);
  return (
    <header className="masthead">
      <div className="mast-inner">
        <Link className="wordmark" href={user ? '/library' : '/login'} aria-label="Keine">
          <span className="seal" aria-hidden="true">慧</span>
          <span className="wm-text">Keine<span className="wm-sub">学习空间</span></span>
        </Link>
        <nav className="nav" aria-label="主导航">
          {nav.map(([href, label]) => (
            <Link key={href} href={href} className={cn(path.startsWith(href) && 'active')}>{label}</Link>
          ))}
        </nav>
        <div className="mast-tools">
          {user ? <Link className="mast-user" href="/account" title="账号设置">{user.username}</Link> : null}
          <Button variant="round" size="round" title="切换深色 / 浅色" aria-label="切换主题" onClick={toggleTheme}>
            <Sun className="i-sun" />
            <Moon className="i-moon" />
          </Button>
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

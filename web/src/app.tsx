import { useEffect, useRef, type ReactNode } from 'react';
import { Toaster } from 'sonner';
import { BottomNav } from '@/components/bottom-nav';
import { Masthead, TopBar } from '@/components/masthead';
import { nextFrom } from '@/lib/page-search';
import { match, navigate, usePath } from '@/lib/router';
import { useUser, type User } from '@/lib/session';
import { useNarrow } from '@/lib/use-narrow';
import { cn } from '@/lib/utils';
import { AccountView } from '@/views/account';
import { AdminView } from '@/views/admin';
import { CourseView } from '@/views/course';
import { CourseEditView } from '@/views/course-edit';
import { LessonView } from '@/views/lesson';
import { LibraryView } from '@/views/library';
import { LoginView } from '@/views/login';
import { MigrateView } from '@/views/migrate';
import { MineView } from '@/views/mine';
import { NotesView } from '@/views/notes';
import { NotificationsView } from '@/views/notifications';

function routeView(path: string, user: User): ReactNode {
  const mg = match('/courses/:id/lessons/:lesson/migrate', path);
  if (mg) return <MigrateView courseId={Number(mg.id)} lessonId={Number(mg.lesson)} />;
  const l = match('/courses/:id/lessons/:lesson', path);
  if (l) return <LessonView courseId={Number(l.id)} lessonId={Number(l.lesson)} />;
  const e = match('/courses/:id/edit', path);
  if (e) return <CourseEditView id={Number(e.id)} />;
  const m = match('/courses/:id', path);
  if (m) return <CourseView id={Number(m.id)} />;
  if (path.startsWith('/mine')) return <MineView />;
  if (path.startsWith('/notifications')) return <NotificationsView />;
  if (path.startsWith('/notes')) return <NotesView />;
  if (path.startsWith('/account')) return <AccountView user={user} />;
  if (path.startsWith('/admin') && user.role === 'admin') return <AdminView path={path} user={user} />;
  return <LibraryView />;
}

// The bars that frame a page on a narrow screen. Pages have the main top bar
// and the bottom nav; a page that needs its own (the course page's actions in
// place of the nav, the lesson's own top bar) gets a case here and a value
// for its bar. 'own': the page draws that bar itself; 'none': no bar.
// bottom 'course': the course page draws its own bar of course actions in
// place of the nav; the shell only keeps the page clear of it.
interface Chrome { top: 'main' | 'own'; bottom: 'nav' | 'course' | 'none' }
function chromeFor(path: string): Chrome {
  // a lesson is read full screen: its own top bar, which gives way while
  // reading, and nothing below
  if (match('/courses/:id/lessons/:lesson', path)) return { top: 'own', bottom: 'none' };
  if (match('/courses/:id', path)) return { top: 'main', bottom: 'course' };
  return { top: 'main', bottom: 'nav' };
}

export function App() {
  const user = useUser();
  const path = usePath();
  // the page notifications or the admin pages were come into from, for
  // their top bar's ← (see page-search.ts)
  const was = useRef({ path, from: null as string | null });
  if (was.current.path !== path) was.current = { path, from: nextFrom(was.current.from, was.current.path, path) };
  const narrow = useNarrow();

  useEffect(() => {
    if (user === null && path !== '/login') navigate('/login', { replace: true });
    if (user && (path === '/login' || path === '/')) navigate('/library', { replace: true });
  }, [user, path]);

  // switching lessons keeps the reader mounted, so only the text reloads;
  // switching admin tabs keeps the admin page, so only its tab changes
  const viewKey = path.replace(/^(\/courses\/[^/]+\/lessons)\/[^/]+$/, '$1').replace(/^\/admin(\/.*)?$/, '/admin');
  let view: ReactNode = null;
  if (user === null) view = <LoginView />;
  else if (user) view = routeView(path, user);

  // signed out, a phone keeps the plain masthead: there is nothing to search or navigate yet;
  // signed in, chrome carries the user for the bars
  const chrome = narrow && user ? { ...chromeFor(path), user } : null;
  return (
    <div className={cn('shell', chrome && chrome.bottom !== 'none' && 'with-bar', chrome?.top === 'own' && 'own-top')}>
      {chrome ? (chrome.top === 'main' ? <TopBar path={path} from={was.current.from} /> : null) : <Masthead user={user} path={path} />}
      <main className="main" id="view" tabIndex={-1}>{view && <div key={viewKey} className="contents">{view}</div>}</main>
      {chrome?.bottom === 'nav' ? <BottomNav user={chrome.user} path={path} /> : null}
      <Toaster position="top-center" toastOptions={{ style: { background: 'var(--text)', color: 'var(--bg)', border: 0, borderRadius: 'var(--radius)' } }} />
    </div>
  );
}

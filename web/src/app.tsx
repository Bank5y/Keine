import { useEffect, type ReactNode } from 'react';
import { Toaster } from 'sonner';
import { Masthead } from '@/components/masthead';
import { match, navigate, usePath } from '@/lib/router';
import { useUser, type User } from '@/lib/session';
import { AccountView } from '@/views/account';
import { AdminView } from '@/views/admin';
import { CourseView } from '@/views/course';
import { LessonView } from '@/views/lesson';
import { LibraryView } from '@/views/library';
import { LoginView } from '@/views/login';
import { MineView } from '@/views/mine';

function routeView(path: string, user: User): ReactNode {
  const l = match('/courses/:id/lessons/:lesson', path);
  if (l) return <LessonView courseId={Number(l.id)} lessonId={Number(l.lesson)} />;
  const m = match('/courses/:id', path);
  if (m) return <CourseView id={Number(m.id)} />;
  if (path.startsWith('/mine')) return <MineView />;
  if (path.startsWith('/account')) return <AccountView user={user} />;
  if (path.startsWith('/admin') && user.role === 'admin') return <AdminView path={path} user={user} />;
  return <LibraryView />;
}

export function App() {
  const user = useUser();
  const path = usePath();

  useEffect(() => {
    if (user === null && path !== '/login') navigate('/login', { replace: true });
    if (user && (path === '/login' || path === '/')) navigate('/library', { replace: true });
  }, [user, path]);

  let view: ReactNode = null;
  if (user === null) view = <LoginView />;
  else if (user) view = routeView(path, user);

  return (
    <>
      <Masthead user={user} path={path} />
      <main className="main" id="view" tabIndex={-1}>{view && <div key={path} className="contents">{view}</div>}</main>
      <Toaster position="top-center" toastOptions={{ style: { background: 'var(--text)', color: 'var(--bg)', border: 0, borderRadius: 'var(--radius)' } }} />
    </>
  );
}

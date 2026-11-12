import { useEffect, type ReactNode } from 'react';
import { Toaster } from 'sonner';
import { Masthead } from '@/components/masthead';
import { navigate, usePath } from '@/lib/router';
import { useUser } from '@/lib/session';
import { LibraryView } from '@/views/library';
import { LoginView } from '@/views/login';
import { MineView } from '@/views/mine';

function routeView(path: string): ReactNode {
  if (path.startsWith('/mine')) return <MineView />;
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
  else if (user) view = routeView(path);

  return (
    <>
      <Masthead user={user} path={path} />
      <main className="main" id="view" tabIndex={-1}>{view && <div key={path} className="contents">{view}</div>}</main>
      <Toaster position="top-center" toastOptions={{ style: { background: 'var(--text)', color: 'var(--bg)', border: 0, borderRadius: 'var(--radius)' } }} />
    </>
  );
}

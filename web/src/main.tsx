import { createRoot } from 'react-dom/client';
import { App } from '@/app';
import { loadUser } from '@/lib/session';
import './styles.css';

const root = createRoot(document.getElementById('root')!);
// PROTOTYPE (#137): a dev-only page with mock data, outside the signed-in app
if (import.meta.env.DEV && location.pathname.startsWith('/prototype/nodes')) {
  void import('@/views/prototype-nodes').then(({ PrototypeNodes }) => root.render(<PrototypeNodes />));
} else {
  root.render(<App />);
  void loadUser();
}

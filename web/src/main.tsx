import { createRoot } from 'react-dom/client';
import { App } from '@/app';
import { loadUser } from '@/lib/session';
import './styles.css';

const root = createRoot(document.getElementById('root')!);
// PROTOTYPE (#74): a dev-only page with mock data, outside the signed-in app
if (import.meta.env.DEV && location.pathname.startsWith('/prototype/explore')) {
  void import('@/views/prototype-explore').then(({ PrototypeExplore }) => root.render(<PrototypeExplore />));
} else {
  root.render(<App />);
  void loadUser();
}

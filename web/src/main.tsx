import { createRoot } from 'react-dom/client';
import { App } from '@/app';
import { loadUser } from '@/lib/session';
import './styles.css';

createRoot(document.getElementById('root')!).render(<App />);
void loadUser();

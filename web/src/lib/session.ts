import { useSyncExternalStore } from 'react';
import { api, ApiError, setUnauthorizedHandler } from '@/lib/api';
import type { AIProblem } from '@/lib/ai';
import type { Account } from '@/lib/credits';

export interface User {
  id: number; username: string; role: 'user' | 'admin'; disabled: boolean; createdAt: string;
  // the assistant has a model for every turn kind (only /api/me says so)
  aiAvailable?: boolean;
  // why not, when it is not
  aiProblem?: AIProblem | null;
  // the reader has read what goes to third-party services (#132), and the
  // services by name: the analysis model, and where its searches go ('' for
  // none)
  aiNoticeRead?: boolean;
  aiProvider?: string;
  aiSearch?: string;
  // the reader's credits today (#130)
  credits?: Account;
}

// undefined: still checking; null: signed out
let current: User | null | undefined;
const listeners = new Set<() => void>();
const set = (u: User | null) => { current = u; listeners.forEach((l) => l()); };

export function useUser() {
  return useSyncExternalStore((l) => { listeners.add(l); return () => listeners.delete(l); }, () => current);
}

export async function loadUser() {
  try {
    set(await api<User>('GET', '/api/me'));
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) set(null);
    else throw e;
  }
}

// readNotice notes that the reader read the notice, on every device.
export async function readNotice() {
  await api('POST', '/api/me/ai-notice');
  if (current) set({ ...current, aiNoticeRead: true });
}

export async function login(username: string, password: string) {
  set(await api<User>('POST', '/api/session', { provider: 'password', username, password }));
}

export async function logout() {
  await api('DELETE', '/api/session');
  set(null);
}

setUnauthorizedHandler(() => { if (current) set(null); });

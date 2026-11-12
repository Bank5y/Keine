import { useSyncExternalStore } from 'react';
import { api, ApiError, setUnauthorizedHandler } from '@/lib/api';

export interface User { id: number; username: string; role: 'user' | 'admin'; disabled: boolean; createdAt: string }

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

export async function login(username: string, password: string) {
  set(await api<User>('POST', '/api/session', { provider: 'password', username, password }));
}

export async function logout() {
  await api('DELETE', '/api/session');
  set(null);
}

setUnauthorizedHandler(() => { if (current) set(null); });

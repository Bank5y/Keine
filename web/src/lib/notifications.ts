import { useSyncExternalStore } from 'react';
import { api } from '@/lib/api';
import { navigate } from '@/lib/router';

// A notification is a kind plus a kind-specific payload. To add a kind, add
// its renderer to `kinds`: what it says and where clicking it goes.

export interface Notification {
  id: number;
  kind: string;
  payload: any;
  read: boolean;
  createdAt: string;
}

export interface NotificationPage {
  notifications: Notification[];
  total: number;
  unread: number;
  page: number;
  pageSize: number;
}

export interface Rendered { title: string; detail?: string; href: string }

const kinds: Record<string, (p: any) => Rendered> = {
  publish_approved: (p) => ({ title: `「${p.courseTitle}」已公开到课程库`, detail: '你的公开申请已通过。', href: `/courses/${p.courseId}` }),
  publish_rejected: (p) => ({ title: `「${p.courseTitle}」的公开申请被驳回`, detail: `理由: ${p.reason}`, href: `/courses/${p.courseId}` }),
  update_approved: (p) => ({ title: `「${p.courseTitle}」的更新申请已通过`, detail: '课程已换成你提交的新版本。', href: `/courses/${p.courseId}` }),
  update_rejected: (p) => ({ title: `「${p.courseTitle}」的更新申请被驳回`, detail: `理由: ${p.reason}`, href: `/courses/${p.courseId}` }),
};

export function render(n: Notification): Rendered {
  return kinds[n.kind]?.(n.payload ?? {}) ?? { title: '一条通知', href: '/notifications' };
}

// ---- the unread count, shared by the bell and the notifications page ----

let unread = 0;
const listeners = new Set<() => void>();
function setUnread(n: number) {
  if (n === unread) return;
  unread = n;
  listeners.forEach((l) => l());
}

export function useUnread() {
  return useSyncExternalStore((l) => { listeners.add(l); return () => listeners.delete(l); }, () => unread);
}

export async function refreshUnread() {
  try {
    setUnread((await api<{ unread: number }>('GET', '/api/notifications/unread-count')).unread);
  } catch { /* keep the last count */ }
}

export async function listNotifications(page: number, pageSize: number) {
  const p = await api<NotificationPage>('GET', `/api/notifications?page=${page}&pageSize=${pageSize}`);
  setUnread(p.unread);
  return p;
}

export async function markAllRead() {
  await api('POST', '/api/notifications/read-all');
  setUnread(0);
}

// open marks a notification read and goes where it points.
export async function openNotification(n: Notification) {
  if (!n.read) {
    try {
      await api('POST', `/api/notifications/${n.id}/read`);
      setUnread(Math.max(0, unread - 1));
    } catch { /* still go there */ }
  }
  navigate(render(n).href);
}

export function timeAgo(iso: string) {
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 60) return '刚刚';
  if (s < 3600) return `${Math.floor(s / 60)} 分钟前`;
  if (s < 86400) return `${Math.floor(s / 3600)} 小时前`;
  if (s < 86400 * 30) return `${Math.floor(s / 86400)} 天前`;
  return new Date(iso).toLocaleDateString('zh-CN');
}

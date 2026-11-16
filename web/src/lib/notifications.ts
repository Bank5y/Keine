import { useSyncExternalStore } from 'react';
import { api } from '@/lib/api';
import { migrateURL } from '@/lib/annotations';
import { navigate } from '@/lib/router';

// A notification is a kind plus a kind-specific payload. To add a kind, add
// its renderer to `kinds`: what it says and where clicking it goes.

export interface Notification {
  id: number;
  kind: string;
  payload: any;
  read: boolean;
  // sticky notifications stay in the bell's inbox, read or not, until dismissed
  sticky: boolean;
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

// A request turned down because its course was deleted has no course page to go to.
const DELETED_REASON = '课程已删除';
const requestHref = (p: any) => (p.reason === DELETED_REASON ? '/mine' : `/courses/${p.courseId}`);

// Where an annotations_pending notification leads: the migration page of
// the first lesson with annotations awaiting migration.
export const migrationHref = (p: any) => migrateURL(p.courseId, p.lessonId);

function changes(p: any) {
  const parts = [['新增', p.added], ['删除', p.removed], ['修改', p.modified], ['改名', p.renamed], ['移动', p.moved]]
    .filter(([, n]) => n > 0)
    .map(([verb, n]) => `${verb} ${n} 讲`);
  return parts.length ? `${parts.join('，')}。` : '课程内容有调整。';
}

const kinds: Record<string, (p: any) => Rendered> = {
  publish_approved: (p) => ({ title: `「${p.courseTitle}」已公开到课程库`, detail: '你的公开申请已通过。', href: `/courses/${p.courseId}` }),
  publish_rejected: (p) => ({ title: `「${p.courseTitle}」的公开申请被驳回`, detail: `理由: ${p.reason}`, href: requestHref(p) }),
  update_approved: (p) => ({ title: `「${p.courseTitle}」的更新申请已通过`, detail: '课程已换成你提交的新版本。', href: `/courses/${p.courseId}` }),
  update_rejected: (p) => ({ title: `「${p.courseTitle}」的更新申请被驳回`, detail: `理由: ${p.reason}`, href: requestHref(p) }),
  course_updated: (p) => ({ title: `「${p.courseTitle}」有更新`, detail: changes(p), href: `/courses/${p.courseId}` }),
  course_unpublished: (p) => ({ title: `「${p.courseTitle}」已从课程库下架`, detail: '学完或做过标注的讲会为订阅者保留。', href: `/courses/${p.courseId}` }),
  course_deleted: (p) => ({
    title: `「${p.courseTitle}」已被删除`,
    detail: p.retainedCount > 0 ? `你保留了其中 ${p.retainedCount} 讲，仍可阅读。` : '你没有保留这门课的任何一讲。',
    href: p.retainedCount > 0 ? `/courses/${p.courseId}` : '/library',
  }),
  publish_requested: (p) => ({ title: `${p.requester} 申请公开「${p.courseTitle}」`, detail: '等待审核。', href: '/admin/reviews' }),
  update_requested: (p) => ({ title: `${p.requester} 提交了「${p.courseTitle}」的更新申请`, detail: '等待审核。', href: '/admin/reviews' }),
  annotations_pending: (p) => ({
    title: `「${p.courseTitle}」更新后，你有标注待迁移`,
    detail: `涉及 ${p.lessons} 讲、${p.annotations} 条标注。`,
    href: migrationHref(p),
  }),
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

// listNotifications pages through every notification, or, for the bell,
// through the inbox: the unread ones and the sticky ones not yet dismissed.
export async function listNotifications(page: number, pageSize: number, inbox = false) {
  const p = await api<NotificationPage>('GET', `/api/notifications?page=${page}&pageSize=${pageSize}${inbox ? '&inbox=1' : ''}`);
  setUnread(p.unread);
  return p;
}

export async function markAllRead() {
  await api('POST', '/api/notifications/read-all');
  setUnread(0);
}

// dismissNotification takes a sticky notification out of the inbox for good.
export async function dismissNotification(n: Notification) {
  await api('POST', `/api/notifications/${n.id}/dismiss`);
  if (!n.read) setUnread(Math.max(0, unread - 1));
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

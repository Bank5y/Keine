import { openNotification, render, timeAgo, type Notification } from '@/lib/notifications';
import { cn } from '@/lib/utils';

export function NotificationItem({ n, compact }: { n: Notification; compact?: boolean }) {
  const r = render(n);
  return (
    <a
      href={r.href}
      className={cn('notif-item', !n.read && 'unread', compact && 'compact')}
      onClick={(e) => {
        if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
        e.preventDefault();
        void openNotification(n);
      }}
    >
      <span className="notif-dot" aria-label={n.read ? undefined : '未读'} />
      <span className="notif-body">
        <span className="notif-title">{r.title}</span>
        {r.detail ? <span className="notif-detail">{r.detail}</span> : null}
        <time className="notif-time" dateTime={n.createdAt} title={new Date(n.createdAt).toLocaleString('zh-CN')}>{timeAgo(n.createdAt)}</time>
      </span>
    </a>
  );
}

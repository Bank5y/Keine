import { X } from 'lucide-react';
import { openNotification, render, timeAgo, type Notification } from '@/lib/notifications';
import { cn } from '@/lib/utils';

// NotificationItem is one notification; a sticky one also offers 忽略,
// which takes it out of the inbox for good.
export function NotificationItem({ n, compact, onDismiss }: { n: Notification; compact?: boolean; onDismiss?: (n: Notification) => void }) {
  const r = render(n);
  const link = (
    <a
      href={r.href}
      className={cn('notif-item', !n.read && 'unread', compact && 'compact', n.sticky && 'sticky')}
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
  if (!n.sticky || !onDismiss) return link;
  return (
    <div className={cn('notif-row', compact && 'compact')}>
      {link}
      <button type="button" className="notif-dismiss" title="忽略这条通知" onClick={() => onDismiss(n)}><X />忽略</button>
    </div>
  );
}

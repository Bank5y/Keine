import { useEffect, useState } from 'react';
import { Bell } from 'lucide-react';
import { Popover } from 'radix-ui';
import { Button } from '@/components/ui/button';
import { Link } from '@/lib/router';
import { NotificationItem } from '@/components/notification-item';
import { useNarrow } from '@/lib/use-narrow';
import { dismissNotification, listNotifications, markAllRead, refreshUnread, useUnread, type Notification } from '@/lib/notifications';

const PREVIEW = 8;

// NotificationBell shows the unread count, refreshed on every page change
// and once a minute, and drops down the inbox: unread notifications, with
// the sticky ones (annotations awaiting migration) first until dismissed.
// On a narrow screen there is no room for the drop-down: it opens the
// notifications page instead.
export function NotificationBell({ path }: { path: string }) {
  const unread = useUnread();
  const narrow = useNarrow();
  const [open, setOpen] = useState(false);
  const [list, setList] = useState<Notification[] | null>(null);

  useEffect(() => { void refreshUnread(); }, [path]);
  useEffect(() => {
    const t = setInterval(() => void refreshUnread(), 60_000);
    return () => clearInterval(t);
  }, []);
  useEffect(() => { setOpen(false); }, [path]);
  useEffect(() => {
    if (!open) return;
    listNotifications(1, PREVIEW, true).then((p) => setList(p.notifications), () => setList([]));
  }, [open]);

  async function readAll() {
    await markAllRead();
    setList((l) => l && l.filter((n) => n.sticky).map((n) => ({ ...n, read: true })));
  }

  async function dismiss(n: Notification) {
    await dismissNotification(n);
    setList((l) => l && l.filter((x) => x.id !== n.id));
  }

  const label = unread ? `通知 (${unread} 条未读)` : '通知';
  const bellFace = (
    <>
      <Bell />
      {unread ? <span className="bell-count" data-testid="unread-count">{unread > 99 ? '99+' : unread}</span> : null}
    </>
  );
  if (narrow) {
    return (
      <Button asChild variant="round" size="round" className="bell">
        <Link href="/notifications" title={label} aria-label={label}>{bellFace}</Link>
      </Button>
    );
  }
  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
        <Button variant="round" size="round" title={label} aria-label={label} className="bell">{bellFace}</Button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content align="end" sideOffset={10} collisionPadding={12} className="notif-pop z-30 data-[state=open]:animate-in data-[state=open]:fade-in-0">
          <div className="notif-pop-head">
            <span>通知</span>
            <Button variant="text" size="bare" disabled={!unread} onClick={() => void readAll()}>全部标为已读</Button>
          </div>
          <div className="notif-pop-list">
            {list === null ? <div className="notif-none">加载中…</div>
              : list.length === 0 ? <div className="notif-none">没有未读通知</div>
              : list.map((n) => <NotificationItem key={n.id} n={n} compact onDismiss={(x) => void dismiss(x)} />)}
          </div>
          <Link className="notif-pop-foot" href="/notifications">查看全部通知</Link>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

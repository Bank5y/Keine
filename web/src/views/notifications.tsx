import { useEffect, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Empty, ErrorBox, PageHead } from '@/components/common';
import { NotificationItem } from '@/components/notification-item';
import { ApiError } from '@/lib/api';
import { listNotifications, markAllRead, useUnread, type NotificationPage } from '@/lib/notifications';

const PAGE_SIZE = 20;

export function NotificationsView() {
  const unread = useUnread();
  const [page, setPage] = useState(1);
  const [data, setData] = useState<NotificationPage | null>(null);
  const [err, setErr] = useState('');

  useEffect(() => {
    let live = true;
    listNotifications(page, PAGE_SIZE).then(
      (p) => { if (live) { setData(p); setErr(''); } },
      (e) => { if (live) setErr(e instanceof ApiError ? e.message : '加载失败'); },
    );
    return () => { live = false; };
  }, [page]);

  async function readAll() {
    await markAllRead();
    setData((d) => d && { ...d, unread: 0, notifications: d.notifications.map((n) => ({ ...n, read: true })) });
  }

  const pages = data ? Math.max(1, Math.ceil(data.total / PAGE_SIZE)) : 1;
  return (
    <div className="page enter">
      <PageHead
        kicker="Notifications"
        title="通知"
        sub={data ? (unread ? `${unread} 条未读` : '没有未读通知') : null}
        actions={<Button variant="outline" disabled={!unread} onClick={() => void readAll()}>全部标为已读</Button>}
      />
      {err ? <ErrorBox message={err} /> : null}
      {data && data.total === 0 ? <Empty glyph="讯" title="还没有通知">申请的审核结果会出现在这里。</Empty> : null}
      {data && data.notifications.length ? (
        <div className="notif-list">
          {data.notifications.map((n) => <NotificationItem key={n.id} n={n} />)}
        </div>
      ) : null}
      {pages > 1 ? (
        <nav className="pager" aria-label="分页">
          <Button variant="outline" disabled={page <= 1} onClick={() => setPage(page - 1)}>上一页</Button>
          <span className="pager-at">第 {page} / {pages} 页</span>
          <Button variant="outline" disabled={page >= pages} onClick={() => setPage(page + 1)}>下一页</Button>
        </nav>
      ) : null}
    </div>
  );
}

import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { ErrorBox, PageHead } from '@/components/common';
import { api, ApiError } from '@/lib/api';
import { Link } from '@/lib/router';
import type { User } from '@/lib/session';
import { cn } from '@/lib/utils';
import { AdminReviews } from '@/views/admin-reviews';
import { AdminUsers } from '@/views/admin-users';

const TABS: [string, string][] = [['/admin/reviews', '审核'], ['/admin/users', '用户'], ['/admin/site', '站点设置']];

export function AdminView({ path, user }: { path: string; user: User }) {
  const tab = TABS.find(([href]) => path.startsWith(href))?.[0] ?? TABS[0][0];
  return (
    <div className="page enter">
      <PageHead kicker="Admin" title="管理" sub="审核公开申请, 管理本站的账号与设置。" />
      <nav className="subnav" aria-label="管理">
        {TABS.map(([href, label]) => <Link key={href} href={href} className={cn(tab === href && 'active')}>{label}</Link>)}
      </nav>
      {tab === '/admin/site' ? <SiteSettings /> : (tab === '/admin/users' ? <AdminUsers me={user} /> : <AdminReviews />)}
    </div>
  );
}

function SiteSettings() {
  const [open, setOpen] = useState<boolean | null>(null);
  const [err, setErr] = useState('');
  useEffect(() => { api('GET', '/api/site').then((s) => setOpen(s.registrationOpen)).catch((e) => setErr(e.message)); }, []);

  async function toggle() {
    try {
      const s = await api('PUT', '/api/site', { registrationOpen: !open });
      setOpen(s.registrationOpen);
      toast.success(s.registrationOpen ? '已开放注册' : '已关闭注册');
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '保存失败');
    }
  }

  return (
    <div className="sections">
      <section className="section">
        <div className="section-head"><div className="no">01</div><h2>自助注册</h2><p>关闭后访客无法注册, 只能由管理员创建账号。</p></div>
        <div className="form-rows">
          {err ? <ErrorBox message={err} /> : null}
          {open === null ? null : (
            <div className="flex items-center gap-4">
              <span className={cn('st', open ? 'ok' : 'off')}>{open ? '开放注册中' : '注册已关闭'}</span>
              <Button size="sm" variant={open ? 'destructive' : 'default'} onClick={toggle}>{open ? '关闭注册' : '开放注册'}</Button>
            </div>
          )}
        </div>
      </section>
    </div>
  );
}

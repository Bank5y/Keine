import { createElement, useEffect, useState, type ComponentType, type ReactNode } from 'react';
import { ArrowLeft, ChartColumn, ClipboardCheck, Cpu, HardDrive, Network, Package, PanelLeftClose, PanelLeftOpen, Settings2, Users, type LucideIcon } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { ErrorBox, PageHead } from '@/components/common';
import { ThemeToggle } from '@/components/masthead';
import { NotificationBell } from '@/components/notification-bell';
import { api, ApiError } from '@/lib/api';
import { TabPane, useTabFade } from '@/components/tabs';
import { Link } from '@/lib/router';
import type { User } from '@/lib/session';
import { useNarrow } from '@/lib/use-narrow';
import { cn } from '@/lib/utils';
import { AdminReviews } from '@/views/admin-reviews';
import { AdminConcepts } from '@/views/admin-concepts';
import { AdminModels } from '@/views/admin-models';
import { AdminPlans } from '@/views/admin-plans';
import { AdminStorage } from '@/views/admin-storage';
import { AdminUsage } from '@/views/admin-usage';
import { AdminUsers } from '@/views/admin-users';

interface AdminSection { href: string; label: string; icon: LucideIcon; desc: string }

// The admin sections, in the groups of the console's side menu.
const GROUPS: [string, AdminSection[]][] = [
  ['内容', [
    { href: '/admin/reviews', label: '审核', icon: ClipboardCheck, desc: '处理公开与更新申请' },
    { href: '/admin/concepts', label: '概念索引', icon: Network, desc: '课程概念的提取与重建' },
  ]],
  ['账号', [
    { href: '/admin/users', label: '用户', icon: Users, desc: '账号、角色与额度' },
    { href: '/admin/plans', label: '套餐', icon: Package, desc: '额度套餐的定价与内容' },
    { href: '/admin/usage', label: '用量', icon: ChartColumn, desc: '站点的 AI 用量与费用' },
  ]],
  ['系统', [
    { href: '/admin/models', label: '模型', icon: Cpu, desc: '站点模型与连接测试' },
    { href: '/admin/storage', label: '存储', icon: HardDrive, desc: 'S3 对象存储、访问方式与文件迁移' },
    { href: '/admin/site', label: '站点设置', icon: Settings2, desc: '注册开关等全站设置' },
  ]],
];
const SECTIONS = GROUPS.flatMap(([, items]) => items);

const SIMPLE: Record<string, ComponentType> = { '/admin/reviews': AdminReviews, '/admin/plans': AdminPlans, '/admin/site': SiteSettings, '/admin/storage': AdminStorage };

function SectionView({ href, path, user }: { href: string; path: string; user: User }) {
  if (href === '/admin/users') return <AdminUsers me={user} />;
  if (href === '/admin/usage') return <AdminUsage path={path} />;
  if (href === '/admin/models') return <AdminModels path={path} />;
  if (href === '/admin/concepts') return <AdminConcepts path={path} />;
  return createElement(SIMPLE[href]);
}

// AdminView stays mounted while its sections change (each has its own
// address): only the section's content fades over. The desktop shows it as
// a console of its own (the app shell leaves out the masthead for it); a
// phone, reaching it from 我的, keeps the page with a row of links.
export function AdminView({ path, user }: { path: string; user: User }) {
  const narrow = useNarrow();
  const current = SECTIONS.find((s) => path.startsWith(s.href)) ?? SECTIONS[0];
  const [shown, fading] = useTabFade(current.href);
  const body = <TabPane fading={fading}><SectionView href={shown} path={path} user={user} /></TabPane>;
  if (!narrow) return <AdminConsole current={current} path={path} user={user}>{body}</AdminConsole>;
  return (
    <div className="page enter">
      <PageHead kicker="Admin" title="管理" sub="审核公开申请, 管理本站的账号、模型、套餐、存储与设置。" />
      <nav className="subnav" aria-label="管理">
        {SECTIONS.map((s) => <Link key={s.href} href={s.href} keepScroll className={cn(s === current && 'active')} aria-current={s === current ? 'page' : undefined}>{s.label}</Link>)}
      </nav>
      {body}
    </div>
  );
}

const COLLAPSED = 'keine.admin-menu-collapsed';

function AdminConsole({ current, path, user, children }: { current: AdminSection; path: string; user: User; children: ReactNode }) {
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(COLLAPSED) === '1');
  const toggle = () => { localStorage.setItem(COLLAPSED, collapsed ? '0' : '1'); setCollapsed(!collapsed); };
  const group = GROUPS.find(([, items]) => items.includes(current))![0];
  return (
    <div className={cn('console', collapsed && 'collapsed')}>
      <header className="console-top">
        <Link className="wordmark" href="/admin" aria-label="Keine 管理后台">
          <span className="seal" aria-hidden="true">慧</span>
          <span className="wm-text">Keine<span className="wm-sub">管理后台</span></span>
        </Link>
        <div className="console-crumb">管理<span>/</span>{group}<span>/</span><b>{current.label}</b></div>
        <div className="mast-tools">
          <NotificationBell path={path} />
          <ThemeToggle />
          <span className="console-user">{user.username}</span>
          <Button variant="outline" size="sm" asChild><Link href="/library"><ArrowLeft />返回学习空间</Link></Button>
        </div>
      </header>
      <nav className="console-side" aria-label="管理">
        {GROUPS.map(([g, items]) => (
          <div key={g} className="console-group">
            <div className="console-group-label">{g}</div>
            {items.map((s) => (
              <Link key={s.href} href={s.href} keepScroll title={collapsed ? s.label : undefined} className={cn('console-item', s === current && 'active')} aria-current={s === current ? 'page' : undefined}>
                <s.icon aria-hidden="true" /><span>{s.label}</span>
              </Link>
            ))}
          </div>
        ))}
        <button type="button" className="console-collapse" onClick={toggle} aria-label={collapsed ? '展开菜单' : '收起菜单'} title={collapsed ? '展开菜单' : undefined}>
          {collapsed ? <PanelLeftOpen /> : <><PanelLeftClose /><span>收起</span></>}
        </button>
      </nav>
      <div className="console-main page enter">
        <div className="console-head"><h1>{current.label}</h1><p>{current.desc}</p></div>
        {children}
      </div>
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

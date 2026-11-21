import { useEffect, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Empty, ErrorBox } from '@/components/common';
import { api, ApiError } from '@/lib/api';
import { credits, quota, type Account, type Plan } from '@/lib/credits';
import type { User } from '@/lib/session';
import { cn } from '@/lib/utils';

const fmtDate = (s: string) => new Date(s).toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' });

// a user on the admin's list, with their plan and what they used today
type Listed = User & { credits: Account };

export function AdminUsers({ me }: { me: User }) {
  const [users, setUsers] = useState<Listed[] | null>(null);
  const [plans, setPlans] = useState<Plan[]>([]);
  const [err, setErr] = useState('');
  const [creating, setCreating] = useState(false);
  const [resetting, setResetting] = useState<User | null>(null);
  const [toggling, setToggling] = useState<User | null>(null);

  const load = () => api<{ users: Listed[] }>('GET', '/api/admin/users').then((r) => setUsers(r.users)).catch((e) => setErr(e.message));
  useEffect(() => {
    void load();
    api<{ plans: Plan[] }>('GET', '/api/admin/plans').then((r) => setPlans(r.plans)).catch((e) => setErr(e.message));
  }, []);

  async function assign(u: Listed, planId: number) {
    try {
      await api('PUT', `/api/admin/users/${u.id}`, { planId });
      toast.success(`${u.username} 已改到「${plans.find((p) => p.id === planId)?.name ?? ''}」`);
      void load();
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '操作失败');
    }
  }

  async function toggle(u: User) {
    try {
      await api('PUT', `/api/admin/users/${u.id}`, { disabled: !u.disabled });
      toast.success(u.disabled ? `已启用 ${u.username}` : `已禁用 ${u.username}`);
      setToggling(null);
      void load();
    } catch (ex) {
      toast.error(ex instanceof ApiError ? ex.message : '操作失败');
    }
  }

  return (
    <>
      <div className="result-bar" style={{ marginTop: 0 }}>
        <span className="count">{users ? <><b>{users.length}</b>个账号</> : '正在加载…'}</span>
        <Button size="sm" variant="default" onClick={() => setCreating(true)}>新建账号</Button>
      </div>
      {err ? <ErrorBox message={err} /> : null}
      {users && !users.length ? <Empty title="还没有账号" /> : null}
      <div className="ledger users">
        {users?.length ? (
          <div className="ledger-row ledger-head" aria-hidden="true">
            <span /><span /><span>套餐</span><span>今日已用 / 每日额度</span><span className="meta" /><span />
          </div>
        ) : null}
        {(users || []).map((u) => (
          <div className="ledger-row" key={u.id}>
            <span className="name" title={u.username}>{u.username}{u.id === me.id ? <span className="ml-2 text-xs font-normal text-ink-3">(你)</span> : null}</span>
            <span className={cn('st', u.disabled ? 'off' : (u.role === 'admin' ? 'admin' : 'ok'))}>{u.disabled ? '已禁用' : (u.role === 'admin' ? '管理员' : '正常')}</span>
            <select className="select plan-select" aria-label={`${u.username} 的套餐`} value={u.credits.planId} onChange={(e) => void assign(u, Number(e.target.value))}>
              {plans.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </select>
            <span className="used">
              <b className={cn(u.credits.remaining !== null && u.credits.remaining < 0 && 'over')}>{credits(u.credits.used)}</b> / {quota(u.credits.daily)}
            </span>
            <span className="meta">注册于 {fmtDate(u.createdAt)}</span>
            <span className="acts">
              {/* every row has both buttons, so the columns line up; the server refuses disabling yourself */}
              <Button size="sm" variant="ghost" disabled={u.id === me.id} onClick={() => (u.disabled ? toggle(u) : setToggling(u))}>{u.disabled ? '启用' : '禁用'}</Button>
              <Button size="sm" variant="ghost" onClick={() => setResetting(u)}>重置密码</Button>
            </span>
          </div>
        ))}
      </div>
      <CreateDialog open={creating} onClose={() => setCreating(false)} onDone={() => { setCreating(false); void load(); }} />
      <ResetDialog user={resetting} onClose={() => setResetting(null)} />
      <Dialog open={!!toggling} onOpenChange={(v) => { if (!v) setToggling(null); }} title="禁用账号">
        <p>禁用后 <b>{toggling?.username}</b> 会立即退出所有设备, 且无法再登录。之后可以重新启用。</p>
        <div className="head-actions">
          <Button onClick={() => setToggling(null)}>取消</Button>
          <Button variant="default" onClick={() => toggling && toggle(toggling)}>禁用</Button>
        </div>
      </Dialog>
    </>
  );
}

function CreateDialog({ open, onClose, onDone }: { open: boolean; onClose: () => void; onDone: () => void }) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [err, setErr] = useState('');
  useEffect(() => { if (open) { setUsername(''); setPassword(''); setErr(''); } }, [open]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    try {
      await api('POST', '/api/users', { username: username.trim(), password });
      toast.success(`已创建 ${username.trim()}`);
      onDone();
    } catch (ex) {
      setErr(ex instanceof ApiError ? ex.message : '创建失败');
    }
  }

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) onClose(); }} title="新建账号">
      <form onSubmit={submit} className="mt-4">
        {err ? <ErrorBox message={err} /> : null}
        <div className="field"><label htmlFor="nu">用户名</label><Input id="nu" autoFocus value={username} onChange={(e) => setUsername(e.target.value)} /></div>
        <div className="field"><label htmlFor="np">初始密码 (至少 8 个字符)</label><Input id="np" type="text" autoComplete="off" value={password} onChange={(e) => setPassword(e.target.value)} /></div>
        <div className="head-actions">
          <Button onClick={onClose}>取消</Button>
          <Button type="submit" variant="default" disabled={!username.trim() || !password}>创建</Button>
        </div>
      </form>
    </Dialog>
  );
}

function ResetDialog({ user, onClose }: { user: User | null; onClose: () => void }) {
  const [password, setPassword] = useState('');
  const [err, setErr] = useState('');
  useEffect(() => { setPassword(''); setErr(''); }, [user]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!user) return;
    try {
      await api('PUT', `/api/admin/users/${user.id}/password`, { password });
      toast.success(`已重置 ${user.username} 的密码`);
      onClose();
    } catch (ex) {
      setErr(ex instanceof ApiError ? ex.message : '重置失败');
    }
  }

  return (
    <Dialog open={!!user} onOpenChange={(v) => { if (!v) onClose(); }} title="重置密码">
      <form onSubmit={submit} className="mt-4">
        <p>为 <b>{user?.username}</b> 设置新密码, 请把新密码告诉对方。</p>
        {err ? <ErrorBox message={err} /> : null}
        <div className="field"><label htmlFor="rp">新密码 (至少 8 个字符)</label><Input id="rp" type="text" autoComplete="off" autoFocus value={password} onChange={(e) => setPassword(e.target.value)} /></div>
        <div className="head-actions">
          <Button onClick={onClose}>取消</Button>
          <Button type="submit" variant="default" disabled={!password}>重置</Button>
        </div>
      </form>
    </Dialog>
  );
}

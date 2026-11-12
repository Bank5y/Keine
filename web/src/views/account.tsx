import { useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ErrorBox, PageHead } from '@/components/common';
import { api, ApiError } from '@/lib/api';
import type { User } from '@/lib/session';

export function AccountView({ user }: { user: User }) {
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [confirm, setConfirm] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setErr('');
    if (next !== confirm) { setErr('两次输入的新密码不一致'); return; }
    setBusy(true);
    try {
      await api('PUT', '/api/me/password', { current, new: next });
      setCurrent(''); setNext(''); setConfirm('');
      toast.success('密码已修改');
    } catch (ex) {
      setErr(ex instanceof ApiError ? ex.message : '修改失败');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="page enter">
      <PageHead kicker="Account" title="账号设置" sub={<>当前登录: <b>{user.username}</b>{user.role === 'admin' ? ' · 管理员' : ''}</>} />
      <div className="sections">
        <section className="section">
          <div className="section-head"><div className="no">01</div><h2>修改密码</h2><p>修改后, 其他设备上的登录不受影响。</p></div>
          <form className="form-rows" onSubmit={submit}>
            {err ? <ErrorBox message={err} /> : null}
            <div className="field"><label htmlFor="cur">当前密码</label><Input id="cur" type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} /></div>
            <div className="field"><label htmlFor="new">新密码 (至少 8 个字符)</label><Input id="new" type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} /></div>
            <div className="field"><label htmlFor="cfm">确认新密码</label><Input id="cfm" type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} /></div>
            <div className="form-foot"><Button type="submit" variant="default" disabled={busy || !current || !next}>保存新密码</Button></div>
          </form>
        </section>
      </div>
    </div>
  );
}

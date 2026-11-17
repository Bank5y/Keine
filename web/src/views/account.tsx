import { useState, type FormEvent } from 'react';
import { ChevronRight, LogOut, ShieldCheck } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ErrorBox, PageHead } from '@/components/common';
import { api, ApiError } from '@/lib/api';
import { Link } from '@/lib/router';
import { logout, type User } from '@/lib/session';

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

  // 我的: who is signed in, and what the masthead offers on a desktop but
  // has no room for on a phone (管理, 退出登录), with the password form.
  return (
    <div className="page enter account">
      <PageHead kicker="Account" title="我的" />
      <div className="me">
        <span className="me-avatar" aria-hidden="true">{user.username.slice(0, 1).toUpperCase()}</span>
        <div>
          <div className="me-name">{user.username}</div>
          {user.role === 'admin' ? <span className="me-role">管理员</span> : null}
        </div>
      </div>
      <div className="me-rows">
        {user.role === 'admin' ? (
          <Link className="me-row" href="/admin">
            <ShieldCheck aria-hidden="true" />
            <span><b>管理</b><small>审核申请、管理账号与站点设置</small></span>
            <ChevronRight className="me-chev" aria-hidden="true" />
          </Link>
        ) : null}
        <section className="me-section" aria-labelledby="pw-head">
          <h2 id="pw-head">修改密码</h2>
          <p>修改后, 其他设备上的登录不受影响。</p>
          <form className="form-rows" onSubmit={submit}>
            {err ? <ErrorBox message={err} /> : null}
            <div className="field"><label htmlFor="cur">当前密码</label><Input id="cur" type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} /></div>
            <div className="field"><label htmlFor="new">新密码 (至少 8 个字符)</label><Input id="new" type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} /></div>
            <div className="field"><label htmlFor="cfm">确认新密码</label><Input id="cfm" type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} /></div>
            <div className="form-foot"><Button type="submit" variant="default" disabled={busy || !current || !next}>保存新密码</Button></div>
          </form>
        </section>
        <button type="button" className="me-row me-logout" onClick={() => void logout()}>
          <LogOut aria-hidden="true" />
          <span><b>退出登录</b></span>
        </button>
      </div>
    </div>
  );
}

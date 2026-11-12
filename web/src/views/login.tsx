import { useEffect, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ErrorBox } from '@/components/common';
import { api, ApiError } from '@/lib/api';
import { login } from '@/lib/session';

export function LoginView() {
  const [mode, setMode] = useState<'login' | 'register'>('login');
  const [registrationOpen, setRegistrationOpen] = useState(false);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api<{ registrationOpen: boolean }>('GET', '/api/site').then((s) => setRegistrationOpen(s.registrationOpen)).catch(() => {});
  }, []);

  const registering = mode === 'register' && registrationOpen;

  async function submit(e: FormEvent) {
    e.preventDefault();
    setErr('');
    if (registering && password !== confirm) { setErr('两次输入的密码不一致'); return; }
    setBusy(true);
    try {
      if (registering) {
        await api('POST', '/api/users', { username: username.trim(), password });
        toast.success('注册成功');
      }
      await login(username.trim(), password);
    } catch (ex) {
      if (ex instanceof ApiError && ex.code === 'registration_closed') setRegistrationOpen(false);
      setErr(ex instanceof ApiError ? ex.message : '请求失败');
    } finally {
      setBusy(false);
    }
  }

  const switchMode = () => { setMode(registering ? 'login' : 'register'); setErr(''); setConfirm(''); };

  return (
    <div className="page enter login-wrap">
      <form className="login" onSubmit={submit}>
        <span className="seal" aria-hidden="true">慧</span>
        <div className="kicker">{registering ? 'Sign up' : 'Sign in'}</div>
        <h1>{registering ? '注册账号' : '登录 Keine'}</h1>
        <p className="sub">你的个人学习空间: 在课程库里挑课, 或上传自己的课程。</p>
        {err ? <ErrorBox message={err} /> : null}
        <div className="field">
          <label htmlFor="username">用户名</label>
          <Input id="username" autoComplete="username" autoFocus value={username} onChange={(e) => setUsername(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="password">密码{registering ? ' (至少 8 个字符)' : ''}</label>
          <Input id="password" type="password" autoComplete={registering ? 'new-password' : 'current-password'} value={password} onChange={(e) => setPassword(e.target.value)} />
        </div>
        {registering ? (
          <div className="field">
            <label htmlFor="confirm">确认密码</label>
            <Input id="confirm" type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
          </div>
        ) : null}
        <Button type="submit" variant="default" size="block" disabled={busy || !username.trim() || !password}>
          {busy ? '请稍候…' : (registering ? '注册并登录' : '登录')}
        </Button>
        {registrationOpen ? (
          <div className="login-switch">
            {registering ? '已有账号? ' : '还没有账号? '}
            <a className="link" onClick={switchMode}>{registering ? '去登录' : '注册一个'}</a>
          </div>
        ) : null}
      </form>
    </div>
  );
}

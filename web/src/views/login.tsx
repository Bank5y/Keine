import { useState, type FormEvent } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ErrorBox } from '@/components/common';
import { ApiError } from '@/lib/api';
import { login } from '@/lib/session';

export function LoginView() {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr('');
    try {
      await login(username.trim(), password);
    } catch (ex) {
      setErr(ex instanceof ApiError ? ex.message : '登录失败');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="page enter login-wrap">
      <form className="login" onSubmit={submit}>
        <span className="seal" aria-hidden="true">慧</span>
        <div className="kicker">Sign in</div>
        <h1>登录 Keine</h1>
        <p className="sub">你的个人学习空间: 在课程库里挑课, 或上传自己的课程。</p>
        {err ? <ErrorBox message={err} /> : null}
        <div className="field">
          <label htmlFor="username">用户名</label>
          <Input id="username" autoComplete="username" autoFocus value={username} onChange={(e) => setUsername(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="password">密码</label>
          <Input id="password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </div>
        <Button type="submit" variant="default" size="block" disabled={busy || !username.trim() || !password}>{busy ? '登录中…' : '登录'}</Button>
      </form>
    </div>
  );
}
